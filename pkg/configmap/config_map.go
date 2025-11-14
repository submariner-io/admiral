/*
SPDX-License-Identifier: Apache-2.0

Copyright Contributors to the Submariner project.

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

package configmap

import (
	"context"
	"os"
	"slices"
	"syscall"

	"github.com/pkg/errors"
	"github.com/submariner-io/admiral/pkg/log"
	"github.com/submariner-io/admiral/pkg/resource"
	"github.com/submariner-io/admiral/pkg/syncer"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// Global defines the name of the ConfigMap shared by all Submariner components.
const Global = "submariner-global"

var logger = log.Logger{Logger: logf.Log.WithName("ConfigMap")}

// Get retrieves the ConfigMap for the given name or, if not found, returns an empty ConfigMap.
func Get(ctx context.Context, client resource.Interface[*corev1.ConfigMap], name string) (*corev1.ConfigMap, error) {
	cm, err := client.Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		return cm, nil
	}

	if resource.IsNotFoundErr(err) {
		return &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Data: map[string]string{},
		}, nil
	}

	return nil, errors.Wrapf(err, "error retrieving ConfigMap %q", name)
}

func WatchAndSignalOnChange(ctx context.Context, k8sClient kubernetes.Interface, namespace string, signal syscall.Signal,
	configMapNames ...string,
) {
	cmClient := k8sClient.CoreV1().ConfigMaps(namespace)

	process := func(obj any, op syncer.Operation) {
		name := resource.MustToMeta(obj).GetName()
		if !slices.Contains(configMapNames, name) {
			return
		}

		logger.Infof("Received %s event for ConfigMap %q - sending signal to self", op, name)

		pid := os.Getpid()

		err := syscall.Kill(pid, signal)
		if err != nil {
			logger.Error(err, "Error sending signal")
		}
	}

	_, informer := cache.NewInformerWithOptions(cache.InformerOptions{
		ListerWatcher: &cache.ListWatch{
			ListWithContextFunc: func(ctx context.Context, options metav1.ListOptions) (runtime.Object, error) {
				return cmClient.List(ctx, options)
			},
			WatchFuncWithContext: func(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
				return cmClient.Watch(ctx, options)
			},
		},
		ObjectType: &corev1.ConfigMap{},
		Handler: cache.ResourceEventHandlerDetailedFuncs{
			AddFunc: func(obj any, isInInitialList bool) {
				if !isInInitialList {
					process(obj, syncer.Create)
				}
			},
			UpdateFunc: func(_, newObj any) {
				process(newObj, syncer.Update)
			},
			DeleteFunc: func(obj any) {
				process(obj, syncer.Delete)
			},
		},
		Transform: resource.TrimManagedFields,
	})

	go informer.RunWithContext(ctx)

	logger.Infof("Started watcher for ConfigMaps %v", configMapNames)
}
