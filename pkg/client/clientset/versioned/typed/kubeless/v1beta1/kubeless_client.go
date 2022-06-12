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
package v1beta1

import (
	v1beta1 "github.com/kubeless/cronjob-trigger/pkg/apis/kubeless/v1beta1"
	"github.com/kubeless/cronjob-trigger/pkg/client/clientset/versioned/scheme"
	serializer "k8s.io/apimachinery/pkg/runtime/serializer"
	rest "k8s.io/client-go/rest"
)

type KubelessV1beta1Interface interface {
	RESTClient() rest.Interface
	CronJobTriggersGetter
}

// KubelessV1beta1Client is used to interact with features provided by the kubeless.io group.
type KubelessV1beta1Client struct {
	restClient rest.Interface
}

func (c *KubelessV1beta1Client) CronJobTriggers(namespace string) CronJobTriggerInterface {
	return newCronJobTriggers(c, namespace)
}

// NewForConfig creates a new KubelessV1beta1Client for the given config.
func NewForConfig(c *rest.Config) (*KubelessV1beta1Client, error) {
	config := *c
	if err := setConfigDefaults(&config); err != nil {
		return nil, err
	}
	client, err := rest.RESTClientFor(&config)
	if err != nil {
		return nil, err
	}
	return &KubelessV1beta1Client{client}, nil
}

// NewForConfigOrDie creates a new KubelessV1beta1Client for the given config and
// panics if there is an error in the config.
func NewForConfigOrDie(c *rest.Config) *KubelessV1beta1Client {
	client, err := NewForConfig(c)
	if err != nil {
		panic(err)
	}
	return client
}

// New creates a new KubelessV1beta1Client for the given RESTClient.
func New(c rest.Interface) *KubelessV1beta1Client {
	return &KubelessV1beta1Client{c}
}

func setConfigDefaults(config *rest.Config) error {
	gv := v1beta1.SchemeGroupVersion
	config.GroupVersion = &gv
	config.APIPath = "/apis"

	v1beta1.AddToScheme(scheme.Scheme)
	config.NegotiatedSerializer = serializer.NewCodecFactory(scheme.Scheme)

	if config.UserAgent == "" {
		config.UserAgent = rest.DefaultKubernetesUserAgent()
	}

	return nil
}

// RESTClient returns a RESTClient that is used to communicate
// with API server by this client implementation.
func (c *KubelessV1beta1Client) RESTClient() rest.Interface {
	if c == nil {
		return nil
	}
	return c.restClient
}
