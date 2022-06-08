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

package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/imdario/mergo"
	cronjobTriggerApi "github.com/kubeless/cronjob-trigger/pkg/apis/kubeless/v1beta1"
	kubelessApi "github.com/kubeless/kubeless/pkg/apis/kubeless/v1beta1"
	batchv1 "k8s.io/api/batch/v1"
	batchv1beta1 "k8s.io/api/batch/v1beta1"
	v1 "k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// EnsureCronJob creates/updates a function cron job
func EnsureCronJob(client kubernetes.Interface, funcObj *kubelessApi.Function, cronjobTriggerObj *cronjobTriggerApi.CronJobTrigger, reqImage string, or []metav1.OwnerReference, reqImagePullSecret []v1.LocalObjectReference) error {
	var maxSucccessfulHist, maxFailedHist int32
	maxSucccessfulHist = 3
	maxFailedHist = 1
	var timeout int
	if funcObj.Spec.Timeout != "" {
		var err error
		timeout, err = strconv.Atoi(funcObj.Spec.Timeout)
		if err != nil {
			return fmt.Errorf("Unable convert %s to a valid timeout", funcObj.Spec.Timeout)
		}
	} else {
		timeout, _ = strconv.Atoi(defaultTimeout)
	}

	schedule := cronjobTriggerObj.Spec.Schedule
	rawPayload, err := json.Marshal(cronjobTriggerObj.Spec.Payload)
	payload := string(rawPayload)
	payloadContentType := "application/json"

	if err != nil {
		return fmt.Errorf("Found an error during JSON parsing on your payload: %s", err)
	}

	activeDeadlineSeconds := int64(timeout)
	jobName := fmt.Sprintf("trigger-%s", funcObj.ObjectMeta.Name)
	functionPort := "8080"
	if len(funcObj.Spec.ServiceSpec.Ports) != 0 {
		functionPort = strconv.Itoa(int(funcObj.Spec.ServiceSpec.Ports[0].Port))
	}

	functionEndpoint := fmt.Sprintf("http://%s.%s.svc.cluster.local:%s", funcObj.ObjectMeta.Name, funcObj.ObjectMeta.Namespace, functionPort)

	headersTemplate := "-H %s -H %s -H %s -H %s -H %s"
	eventId := "\"Event-Id: $(POD_UID)\""
	eventTime := "\"Event-Time: $(date --rfc-3339=seconds --utc)\""
	eventNamespace := "\"Event-Namespace: cronjobtrigger.kubeless.io\""
	eventType := fmt.Sprintf("\"Event-Type: %s\"", payloadContentType)
	contentType := fmt.Sprintf("\"Content-Type: %s\"", payloadContentType)
	headers := fmt.Sprintf(headersTemplate, eventId, eventTime, eventNamespace, eventType, contentType)

	commandTemplate := "curl -Lv %s %s"
	command := fmt.Sprintf(commandTemplate, headers, functionEndpoint)

	mergedLabels := mergeMaps(cronjobTriggerObj.ObjectMeta.Labels, funcObj.ObjectMeta.Labels)
	mergedAnnotations := mergeMaps(cronjobTriggerObj.ObjectMeta.Annotations, funcObj.ObjectMeta.Annotations)

	if payload != "null" {
		command += fmt.Sprintf(" -d '%s'", payload)
	}

	job := &batchv1beta1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:            jobName,
			Namespace:       funcObj.ObjectMeta.Namespace,
			Labels:          addDefaultLabel(mergedLabels),
			Annotations:     mergedAnnotations,
			OwnerReferences: or,
		},
		Spec: batchv1beta1.CronJobSpec{
			Schedule:                   schedule,
			SuccessfulJobsHistoryLimit: &maxSucccessfulHist,
			FailedJobsHistoryLimit:     &maxFailedHist,
			JobTemplate: batchv1beta1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					ActiveDeadlineSeconds: &activeDeadlineSeconds,
					Template: v1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels:      addDefaultLabel(mergedLabels),
							Annotations: mergedAnnotations,
						},
						Spec: v1.PodSpec{
							ImagePullSecrets: reqImagePullSecret,
							Containers: []v1.Container{
								{
									Image: reqImage,
									Name:  "trigger",
									Env: []v1.EnvVar{
										{
											Name: "POD_UID",
											ValueFrom: &v1.EnvVarSource{
												FieldRef: &v1.ObjectFieldSelector{
													FieldPath: "metadata.uid",
												},
											},
										},
									},
									Command: []string{
										"/bin/sh",
										"-c",
									},
									Args: []string{
										command,
									},
									Resources: v1.ResourceRequirements{
										Limits: v1.ResourceList{
											v1.ResourceMemory: resource.MustParse("64Mi"),
											v1.ResourceCPU:    resource.MustParse("100m"),
										},
										Requests: v1.ResourceList{
											v1.ResourceMemory: resource.MustParse("16Mi"),
											v1.ResourceCPU:    resource.MustParse("10m"),
										},
									},
								},
							},
							RestartPolicy: v1.RestartPolicyNever,
						},
					},
				},
			},
		},
	}

	_, err = client.BatchV1beta1().CronJobs(funcObj.ObjectMeta.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
	if err != nil && k8sErrors.IsAlreadyExists(err) {
		newCronJob := &batchv1beta1.CronJob{}
		newCronJob, err = client.BatchV1beta1().CronJobs(funcObj.ObjectMeta.Namespace).Get(context.TODO(), jobName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if !hasDefaultLabel(newCronJob.ObjectMeta.Labels) {
			return fmt.Errorf("Found a conflicting cronjob object %s/%s. Aborting", funcObj.ObjectMeta.Namespace, funcObj.ObjectMeta.Name)
		}
		newCronJob.ObjectMeta.Labels = funcObj.ObjectMeta.Labels
		newCronJob.ObjectMeta.OwnerReferences = or
		newCronJob.Spec = job.Spec
		_, err = client.BatchV1beta1().CronJobs(funcObj.ObjectMeta.Namespace).Update(context.TODO(), newCronJob, metav1.UpdateOptions{})
	}
	return err
}

func addDefaultLabel(labels map[string]string) map[string]string {
	if labels == nil {
		labels = make(map[string]string)
	}
	labels["created-by"] = "kubeless"
	return labels
}

func hasDefaultLabel(labels map[string]string) bool {
	if labels == nil || labels["created-by"] != "kubeless" {
		return false
	}
	return true
}

func mergeMaps(m1 map[string]string, m2 map[string]string) map[string]string {
	dest := make(map[string]string)

	mergo.Merge(&dest, m2, mergo.WithOverride)
	mergo.Merge(&dest, m1, mergo.WithOverride)

	return dest
}
