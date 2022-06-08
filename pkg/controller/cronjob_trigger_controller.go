/*
Copyright (c) 2016-2017 Bitnami

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"time"

	cronjobTriggerAPi "github.com/kubeless/cronjob-trigger/pkg/apis/kubeless/v1beta1"
	"github.com/kubeless/cronjob-trigger/pkg/client/clientset/versioned"
	cronjobInformers "github.com/kubeless/cronjob-trigger/pkg/client/informers/externalversions/kubeless/v1beta1"
	cronjobutils "github.com/kubeless/cronjob-trigger/pkg/utils"
	kubelessApi "github.com/kubeless/kubeless/pkg/apis/kubeless/v1beta1"
	kubelessversioned "github.com/kubeless/kubeless/pkg/client/clientset/versioned"
	kubelessInformers "github.com/kubeless/kubeless/pkg/client/informers/externalversions/kubeless/v1beta1"
	kubelessutils "github.com/kubeless/kubeless/pkg/utils"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

const (
	cronJobTriggerMaxRetries = 11
	cronJobObjKind           = "Trigger"
	cronJobAPIVersion        = "kubeless.io/v1beta1"
	cronJobTriggerFinalizer  = "kubeless.io/cronjobtrigger"
)

// CronJobTriggerController object
type CronJobTriggerController struct {
	logger           *logrus.Entry
	clientset        kubernetes.Interface
	config           *corev1.ConfigMap
	cronjobclient    versioned.Interface
	kubelessclient   kubelessversioned.Interface
	queue            workqueue.RateLimitingInterface
	cronJobInformer  cache.SharedIndexInformer
	functionInformer cache.SharedIndexInformer
	imagePullSecrets []corev1.LocalObjectReference
}

// CronJobTriggerConfig contains config for CronJobTriggerController
type CronJobTriggerConfig struct {
	KubeCli        kubernetes.Interface
	TriggerClient  versioned.Interface
	KubelessClient kubelessversioned.Interface
}

// NewCronJobTriggerController initializes a controller object
func NewCronJobTriggerController(cfg CronJobTriggerConfig) *CronJobTriggerController {
	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	config, err := kubelessutils.GetKubelessConfig(cfg.KubeCli, kubelessutils.GetAPIExtensionsClientInCluster())
	if err != nil {
		logrus.Fatalf("Unable to read the configmap: %s", err)
	}

	cronJobInformer := cronjobInformers.NewCronJobTriggerInformer(cfg.TriggerClient, config.Data["functions-namespace"], 0, cache.Indexers{})

	functionInformer := kubelessInformers.NewFunctionInformer(cfg.KubelessClient, config.Data["functions-namespace"], 0, cache.Indexers{})

	cronJobInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(obj)
			if err == nil {
				queue.Add(key)
			}
		},
		UpdateFunc: func(old, new interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(new)
			if err == nil {
				newObj := new.(*cronjobTriggerAPi.CronJobTrigger)
				oldObj := old.(*cronjobTriggerAPi.CronJobTrigger)
				if cronJobTriggerObjChanged(oldObj, newObj) {
					queue.Add(key)
				}
			}
		},
		DeleteFunc: func(obj interface{}) {
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			if err == nil {
				queue.Add(key)
			}
		},
	})
	controller := CronJobTriggerController{
		logger:           logrus.WithField("controller", "cronjob-trigger-controller"),
		clientset:        cfg.KubeCli,
		kubelessclient:   cfg.KubelessClient,
		cronjobclient:    cfg.TriggerClient,
		config:           config,
		cronJobInformer:  cronJobInformer,
		functionInformer: functionInformer,
		queue:            queue,
		imagePullSecrets: cronjobutils.GetSecretsAsLocalObjectReference(config.Data["provision-image-secret"], config.Data["builder-image-secret"]),
	}

	functionInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			controller.functionAddedDeletedUpdated(obj, false)
		},
		DeleteFunc: func(obj interface{}) {
			controller.functionAddedDeletedUpdated(obj, true)
		},
		UpdateFunc: func(old, new interface{}) {
			controller.functionAddedDeletedUpdated(new, false)
		},
	})

	return &controller
}

// Run starts the Trigger controller
func (c *CronJobTriggerController) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	c.logger.Info("Starting Cron Job Trigger controller")

	go c.cronJobInformer.Run(stopCh)
	go c.functionInformer.Run(stopCh)

	if !c.WaitForCacheSync(stopCh) {
		return
	}

	c.logger.Info("Cron Job Trigger controller synced and ready")

	wait.Until(c.runWorker, time.Second, stopCh)
}

// WaitForCacheSync is required for caches to be synced
func (c *CronJobTriggerController) WaitForCacheSync(stopCh <-chan struct{}) bool {
	if !cache.WaitForCacheSync(stopCh, c.cronJobInformer.HasSynced, c.functionInformer.HasSynced) {
		utilruntime.HandleError(fmt.Errorf("Timed out waiting for caches required for Cronjob triggers controller to sync;"))
		return false
	}
	c.logger.Info("Cronjob Trigger controller caches are synced and ready")
	return true
}

func (c *CronJobTriggerController) runWorker() {
	for c.processNextItem() {
		// continue looping
	}
}

func (c *CronJobTriggerController) processNextItem() bool {
	key, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(key)

	err := c.syncCronJobTrigger(key.(string))
	if err == nil {
		// No error, reset the ratelimit counters
		c.queue.Forget(key)
	} else if c.queue.NumRequeues(key) < cronJobTriggerMaxRetries {
		c.logger.Errorf("Error processing %s (will retry): %v", key, err)
		c.queue.AddRateLimited(key)
	} else {
		// err != nil and too many retries
		c.logger.Errorf("Error processing %s (giving up): %v", key, err)
		c.queue.Forget(key)
		utilruntime.HandleError(err)
	}

	return true
}

func (c *CronJobTriggerController) syncCronJobTrigger(key string) error {
	c.logger.Infof("Processing update to CronJob Trigger: %s", key)

	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}

	obj, exists, err := c.cronJobInformer.GetIndexer().GetByKey(key)
	if err != nil {
		return fmt.Errorf("Error fetching object with key %s from store: %v", key, err)
	}

	// this is an update when CronJob trigger API object is actually deleted, we dont need to process anything here
	if !exists {
		c.logger.Infof("Cronjob Trigger %s not found, ignoring", key)
		return nil
	}

	cronJobtriggerObj := obj.(*cronjobTriggerAPi.CronJobTrigger)

	// CronJob trigger API object is marked for deletion (DeletionTimestamp != nil), so lets process the delete update
	if cronJobtriggerObj.ObjectMeta.DeletionTimestamp != nil {

		// If finalizer is removed, then we already processed the delete update, so just return
		if !c.cronJobTriggerObjHasFinalizer(cronJobtriggerObj) {
			return nil
		}

		// CronJob Trigger object should be deleted, so remove associated cronjob and remove the finalizer
		err = c.clientset.BatchV1beta1().CronJobs(ns).Delete(context.TODO(), name, metav1.DeleteOptions{})
		if err != nil && !k8sErrors.IsNotFound(err) {
			c.logger.Errorf("Failed to remove CronJob created for CronJobTrigger Obj: %s due to: %v: ", key, err)
			return err
		}

		// remove finalizer from the cronjob trigger object, so that we dont have to process any further and object can be deleted
		err = c.cronJobTriggerObjRemoveFinalizer(cronJobtriggerObj)
		if err != nil {
			c.logger.Errorf("Failed to remove CronJob trigger controller as finalizer to CronJob Obj: %s due to: %v: ", key, err)
			return err
		}

		c.logger.Infof("Cronjob trigger object %s has been successfully processed and marked for deletion", key)
		return nil
	}

	// If CronJob trigger API in not marked with self as finalizer, then add the finalizer
	if !c.cronJobTriggerObjHasFinalizer(cronJobtriggerObj) {
		err = c.cronJobTriggerObjAddFinalizer(cronJobtriggerObj)
		if err != nil {
			c.logger.Errorf("Error adding CronJob trigger controller as finalizer to  CronJobTrigger Obj: %s CRD object due to: %v: ", key, err)
			return err
		}
	}

	or, err := kubelessutils.GetOwnerReference(cronJobObjKind, cronJobAPIVersion, cronJobtriggerObj.Name, cronJobtriggerObj.UID)
	if err != nil {
		return err
	}

	functionObj, err := c.kubelessclient.KubelessV1beta1().Functions(ns).Get(cronJobtriggerObj.Spec.FunctionName, metav1.GetOptions{})
	if err != nil {
		c.logger.Errorf("Unable to find the function %s in the namespace %s. Received %s: ", cronJobtriggerObj.Spec.FunctionName, ns, err)
		return err
	}
	err = cronjobutils.EnsureCronJob(c.clientset, functionObj, cronJobtriggerObj, c.config.Data["provision-image"], or, c.imagePullSecrets)
	if err != nil {
		return err
	}

	c.logger.Infof("Processed update to CronJobrigger: %s", key)
	return nil
}

func (c *CronJobTriggerController) functionAddedDeletedUpdated(obj interface{}, deleted bool) error {
	functionObj, ok := obj.(*kubelessApi.Function)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			err := fmt.Errorf("Couldn't get object from tombstone %#v", obj)
			c.logger.Errorf(err.Error())
			return err
		}
		functionObj, ok = tombstone.Obj.(*kubelessApi.Function)
		if !ok {
			err := fmt.Errorf("Tombstone contained object that is not a Pod %#v", obj)
			c.logger.Errorf(err.Error())
			return err
		}
	}

	c.logger.Infof("Processing update to function object %s Namespace: %s", functionObj.Name, functionObj.Namespace)
	if deleted {
		c.logger.Infof("Function %s deleted. Removing associated cronjob trigger", functionObj.Name)
		cjtList, err := c.cronjobclient.KubelessV1beta1().CronJobTriggers(functionObj.Namespace).List(metav1.ListOptions{})
		if err != nil {
			return err
		}
		for _, cjt := range cjtList.Items {
			if cjt.Spec.FunctionName == functionObj.Name {
				err = c.cronjobclient.KubelessV1beta1().CronJobTriggers(functionObj.Namespace).Delete(cjt.Name, &metav1.DeleteOptions{})
				if err != nil && !k8sErrors.IsNotFound(err) {
					c.logger.Errorf("Failed to delete cronjobtrigger created for the function %s in namespace %s, Error: %s", functionObj.ObjectMeta.Name, functionObj.ObjectMeta.Namespace, err)
					return err
				}
			}
		}
	}
	return nil
}

func (c *CronJobTriggerController) cronJobTriggerObjHasFinalizer(triggerObj *cronjobTriggerAPi.CronJobTrigger) bool {
	currentFinalizers := triggerObj.ObjectMeta.Finalizers
	for _, f := range currentFinalizers {
		if f == cronJobTriggerFinalizer {
			return true
		}
	}
	return false
}

func (c *CronJobTriggerController) cronJobTriggerObjAddFinalizer(triggercObj *cronjobTriggerAPi.CronJobTrigger) error {
	triggercObjClone := triggercObj.DeepCopy()
	triggercObjClone.ObjectMeta.Finalizers = append(triggercObjClone.ObjectMeta.Finalizers, cronJobTriggerFinalizer)
	return cronjobutils.UpdateCronJobCustomResource(c.cronjobclient, triggercObjClone)
}

func (c *CronJobTriggerController) cronJobTriggerObjRemoveFinalizer(triggercObj *cronjobTriggerAPi.CronJobTrigger) error {
	triggerObjClone := triggercObj.DeepCopy()
	newSlice := make([]string, 0)
	for _, item := range triggerObjClone.ObjectMeta.Finalizers {
		if item == cronJobTriggerFinalizer {
			continue
		}
		newSlice = append(newSlice, item)
	}
	if len(newSlice) == 0 {
		newSlice = nil
	}
	triggerObjClone.ObjectMeta.Finalizers = newSlice
	err := cronjobutils.UpdateCronJobCustomResource(c.cronjobclient, triggerObjClone)
	if err != nil {
		return err
	}
	return nil
}

func cronJobTriggerObjChanged(oldObj, newObj *cronjobTriggerAPi.CronJobTrigger) bool {
	// If the CronJob trigger object's deletion timestamp is set, then process
	if oldObj.DeletionTimestamp != newObj.DeletionTimestamp {
		return true
	}
	// If the new and old CronJob trigger object's resource version is same
	if oldObj.ResourceVersion != newObj.ResourceVersion {
		return true
	}
	newSpec := newObj.Spec
	oldSpec := oldObj.Spec
	if newSpec.Schedule != oldSpec.Schedule {
		return true
	}

	return false
}
