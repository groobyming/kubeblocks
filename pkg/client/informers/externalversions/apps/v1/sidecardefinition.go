/*
Copyright (C) 2022-2025 ApeCloud Co., Ltd

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

package v1

import (
	"context"
	time "time"

	appsv1 "github.com/apecloud/kubeblocks/apis/apps/v1"
	versioned "github.com/apecloud/kubeblocks/pkg/client/clientset/versioned"
	internalinterfaces "github.com/apecloud/kubeblocks/pkg/client/informers/externalversions/internalinterfaces"
	v1 "github.com/apecloud/kubeblocks/pkg/client/listers/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	cache "k8s.io/client-go/tools/cache"
)

// SidecarDefinitionInformer provides access to a shared informer and lister for
// SidecarDefinitions.
type SidecarDefinitionInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1.SidecarDefinitionLister
}

type sidecarDefinitionInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
}

// NewSidecarDefinitionInformer constructs a new informer for SidecarDefinition type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewSidecarDefinitionInformer(client versioned.Interface, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredSidecarDefinitionInformer(client, resyncPeriod, indexers, nil)
}

// NewFilteredSidecarDefinitionInformer constructs a new informer for SidecarDefinition type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredSidecarDefinitionInformer(client versioned.Interface, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.AppsV1().SidecarDefinitions().List(context.TODO(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.AppsV1().SidecarDefinitions().Watch(context.TODO(), options)
			},
		},
		&appsv1.SidecarDefinition{},
		resyncPeriod,
		indexers,
	)
}

func (f *sidecarDefinitionInformer) defaultInformer(client versioned.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredSidecarDefinitionInformer(client, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *sidecarDefinitionInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&appsv1.SidecarDefinition{}, f.defaultInformer)
}

func (f *sidecarDefinitionInformer) Lister() v1.SidecarDefinitionLister {
	return v1.NewSidecarDefinitionLister(f.Informer().GetIndexer())
}
