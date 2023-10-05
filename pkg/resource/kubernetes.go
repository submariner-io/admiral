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

//nolint:wrapcheck // These functions are pass-through wrappers for the k8s APIs.
package resource

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	controllerClient "sigs.k8s.io/controller-runtime/pkg/client"
)

type KubernetesInterface[T runtime.Object, L runtime.Object] interface {
	Get(ctx context.Context, name string, options metav1.GetOptions) (T, error)
	Create(ctx context.Context, obj T, options metav1.CreateOptions) (T, error)
	Update(ctx context.Context, obj T, options metav1.UpdateOptions) (T, error)
	Delete(ctx context.Context, name string, options metav1.DeleteOptions) error
	List(ctx context.Context, options metav1.ListOptions) (L, error)
}

type KubernetesStatusInterface[T runtime.Object, L runtime.Object] interface {
	KubernetesInterface[T, L]
	UpdateStatus(ctx context.Context, obj T, options metav1.UpdateOptions) (T, error)
}

func buildListItemExtractor[T runtime.Object, L runtime.Object](k8sInterface KubernetesInterface[T, L],
) func(context.Context, metav1.ListOptions) ([]T, error) {
	return func(ctx context.Context, options metav1.ListOptions) ([]T, error) {
		l, err := k8sInterface.List(ctx, options)
		return MustExtractList[T](l), err
	}
}

func kubernetesInterfaceAdapter[T runtime.Object, L runtime.Object](k8sInterface KubernetesInterface[T, L]) Interface[T] {
	return &InterfaceFuncs[T]{
		GetFunc:    k8sInterface.Get,
		CreateFunc: k8sInterface.Create,
		UpdateFunc: k8sInterface.Update,
		DeleteFunc: k8sInterface.Delete,
		ListFunc:   buildListItemExtractor[T, L](k8sInterface),
	}
}

func kubernetesStatusInterfaceAdapter[T runtime.Object, L runtime.Object](k8sInterface KubernetesStatusInterface[T, L]) Interface[T] {
	return &InterfaceFuncs[T]{
		GetFunc:          k8sInterface.Get,
		CreateFunc:       k8sInterface.Create,
		UpdateFunc:       k8sInterface.Update,
		UpdateStatusFunc: k8sInterface.UpdateStatus,
		DeleteFunc:       k8sInterface.Delete,
		ListFunc:         buildListItemExtractor[T, L](k8sInterface),
	}
}

// Entries are sorted alphabetically by group and resource

// Apps

func ForDaemonSet(client kubernetes.Interface, namespace string) Interface[*appsv1.DaemonSet] {
	return kubernetesStatusInterfaceAdapter[*appsv1.DaemonSet, *appsv1.DaemonSetList](client.AppsV1().DaemonSets(namespace))
}

func ForDeployment(client kubernetes.Interface, namespace string) Interface[*appsv1.Deployment] {
	return kubernetesStatusInterfaceAdapter[*appsv1.Deployment, *appsv1.DeploymentList](client.AppsV1().Deployments(namespace))
}

// Core

func ForConfigMap(client kubernetes.Interface, namespace string) Interface[*corev1.ConfigMap] {
	return kubernetesInterfaceAdapter[*corev1.ConfigMap, *corev1.ConfigMapList](client.CoreV1().ConfigMaps(namespace))
}

func ForNamespace(client kubernetes.Interface) Interface[*corev1.Namespace] {
	return kubernetesStatusInterfaceAdapter[*corev1.Namespace, *corev1.NamespaceList](client.CoreV1().Namespaces())
}

func ForPod(client kubernetes.Interface, namespace string) Interface[*corev1.Pod] {
	return kubernetesStatusInterfaceAdapter[*corev1.Pod, *corev1.PodList](client.CoreV1().Pods(namespace))
}

func ForService(client kubernetes.Interface, namespace string) Interface[*corev1.Service] {
	return kubernetesStatusInterfaceAdapter[*corev1.Service, *corev1.ServiceList](client.CoreV1().Services(namespace))
}

func ForServiceAccount(client kubernetes.Interface, namespace string) Interface[*corev1.ServiceAccount] {
	return kubernetesInterfaceAdapter[*corev1.ServiceAccount, *corev1.ServiceAccountList](client.CoreV1().ServiceAccounts(namespace))
}

// RBAC

func ForClusterRole(client kubernetes.Interface) Interface[*rbacv1.ClusterRole] {
	return kubernetesInterfaceAdapter[*rbacv1.ClusterRole, *rbacv1.ClusterRoleList](client.RbacV1().ClusterRoles())
}

func ForClusterRoleBinding(client kubernetes.Interface) Interface[*rbacv1.ClusterRoleBinding] {
	return kubernetesInterfaceAdapter[*rbacv1.ClusterRoleBinding, *rbacv1.ClusterRoleBindingList](client.RbacV1().ClusterRoleBindings())
}

func ForRole(client kubernetes.Interface, namespace string) Interface[*rbacv1.Role] {
	return kubernetesInterfaceAdapter[*rbacv1.Role, *rbacv1.RoleList](client.RbacV1().Roles(namespace))
}

func ForRoleBinding(client kubernetes.Interface, namespace string) Interface[*rbacv1.RoleBinding] {
	return kubernetesInterfaceAdapter[*rbacv1.RoleBinding, *rbacv1.RoleBindingList](client.RbacV1().RoleBindings(namespace))
}

// Controller client wrappers

func ForControllerClient[T controllerClient.Object](client controllerClient.Client, namespace string, objType T) *InterfaceFuncs[T] {
	return &InterfaceFuncs[T]{
		GetFunc: func(ctx context.Context, name string, options metav1.GetOptions) (T, error) {
			obj := objType.DeepCopyObject().(T)
			err := client.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, obj)
			return obj, err
		},
		CreateFunc: func(ctx context.Context, obj T, options metav1.CreateOptions) (T, error) {
			obj = obj.DeepCopyObject().(T)
			err := client.Create(ctx, obj)
			return obj, err
		},
		UpdateFunc: func(ctx context.Context, obj T, options metav1.UpdateOptions) (T, error) {
			obj = obj.DeepCopyObject().(T)
			err := client.Update(ctx, obj)
			return obj, err
		},
		UpdateStatusFunc: func(ctx context.Context, obj T, options metav1.UpdateOptions) (T, error) {
			obj = obj.DeepCopyObject().(T)
			err := client.Status().Update(ctx, obj)
			return obj, err
		},
		DeleteFunc: func(ctx context.Context, name string, options metav1.DeleteOptions) error {
			obj := objType.DeepCopyObject().(controllerClient.Object)
			obj.SetName(name)
			obj.SetNamespace(namespace)

			return client.Delete(ctx, obj)
		},
	}
}

func ForListableControllerClient[T controllerClient.Object](client controllerClient.Client, namespace string, objType T,
	listType controllerClient.ObjectList,
) *InterfaceFuncs[T] {
	i := ForControllerClient[T](client, namespace, objType)
	i.ListFunc = func(ctx context.Context, options metav1.ListOptions) ([]T, error) {
		opts := []controllerClient.ListOption{controllerClient.InNamespace(namespace)}

		if options.LabelSelector != "" {
			s, err := labels.Parse(options.LabelSelector)
			utilruntime.Must(err)

			opts = append(opts, controllerClient.MatchingLabelsSelector{Selector: s})
		}

		list := listType.DeepCopyObject().(controllerClient.ObjectList)

		err := client.List(ctx, list, opts...)

		return MustExtractList[T](list), err
	}

	return i
}
