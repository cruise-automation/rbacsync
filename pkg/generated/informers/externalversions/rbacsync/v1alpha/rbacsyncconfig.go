/*
Copyright The Kubernetes Authors.

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
// Code generated by informer-gen. DO NOT EDIT.

package v1alpha

import (
	"context"
	time "time"

	rbacsyncv1alpha "github.com/cruise-automation/rbacsync/pkg/apis/rbacsync/v1alpha"
	versioned "github.com/cruise-automation/rbacsync/pkg/generated/clientset/versioned"
	internalinterfaces "github.com/cruise-automation/rbacsync/pkg/generated/informers/externalversions/internalinterfaces"
	v1alpha "github.com/cruise-automation/rbacsync/pkg/generated/listers/rbacsync/v1alpha"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	cache "k8s.io/client-go/tools/cache"
)

// RBACSyncConfigInformer provides access to a shared informer and lister for
// RBACSyncConfigs.
type RBACSyncConfigInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1alpha.RBACSyncConfigLister
}

type rBACSyncConfigInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
	namespace        string
}

// NewRBACSyncConfigInformer constructs a new informer for RBACSyncConfig type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewRBACSyncConfigInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredRBACSyncConfigInformer(client, namespace, resyncPeriod, indexers, nil)
}

// NewFilteredRBACSyncConfigInformer constructs a new informer for RBACSyncConfig type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredRBACSyncConfigInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options v1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.RBACSyncV1alpha().RBACSyncConfigs(namespace).List(context.TODO(), options)
			},
			WatchFunc: func(options v1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.RBACSyncV1alpha().RBACSyncConfigs(namespace).Watch(context.TODO(), options)
			},
		},
		&rbacsyncv1alpha.RBACSyncConfig{},
		resyncPeriod,
		indexers,
	)
}

func (f *rBACSyncConfigInformer) defaultInformer(client versioned.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredRBACSyncConfigInformer(client, f.namespace, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *rBACSyncConfigInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&rbacsyncv1alpha.RBACSyncConfig{}, f.defaultInformer)
}

func (f *rBACSyncConfigInformer) Lister() v1alpha.RBACSyncConfigLister {
	return v1alpha.NewRBACSyncConfigLister(f.Informer().GetIndexer())
}
