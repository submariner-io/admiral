/*
© 2021 Red Hat, Inc.

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
package resource

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
)

//nolint:dupl //false positive - lines are similar but not duplicated
func ForDeployment(client kubernetes.Interface, namespace string) Interface {
	return &InterfaceFuncs{
		GetFunc: func(name string, options metav1.GetOptions) (runtime.Object, error) {
			return client.AppsV1().Deployments(namespace).Get(name, options)
		},
		CreateFunc: func(obj runtime.Object) (runtime.Object, error) {
			return client.AppsV1().Deployments(namespace).Create(obj.(*appsv1.Deployment))
		},
		UpdateFunc: func(obj runtime.Object) (runtime.Object, error) {
			return client.AppsV1().Deployments(namespace).Update(obj.(*appsv1.Deployment))
		},
		DeleteFunc: func(name string, options *metav1.DeleteOptions) error {
			return client.AppsV1().Deployments(namespace).Delete(name, options)
		},
	}
}

//nolint:dupl //false positive - lines are similar but not duplicated
func ForDaemonSet(client kubernetes.Interface, namespace string) Interface {
	return &InterfaceFuncs{
		GetFunc: func(name string, options metav1.GetOptions) (runtime.Object, error) {
			return client.AppsV1().DaemonSets(namespace).Get(name, options)
		},
		CreateFunc: func(obj runtime.Object) (runtime.Object, error) {
			return client.AppsV1().DaemonSets(namespace).Create(obj.(*appsv1.DaemonSet))
		},
		UpdateFunc: func(obj runtime.Object) (runtime.Object, error) {
			return client.AppsV1().DaemonSets(namespace).Update(obj.(*appsv1.DaemonSet))
		},
		DeleteFunc: func(name string, options *metav1.DeleteOptions) error {
			return client.AppsV1().DaemonSets(namespace).Delete(name, options)
		},
	}
}

//nolint:dupl //false positive - lines are similar but not duplicated
func ForPod(client kubernetes.Interface, namespace string) Interface {
	return &InterfaceFuncs{
		GetFunc: func(name string, options metav1.GetOptions) (runtime.Object, error) {
			return client.CoreV1().Pods(namespace).Get(name, options)
		},
		CreateFunc: func(obj runtime.Object) (runtime.Object, error) {
			return client.CoreV1().Pods(namespace).Create(obj.(*corev1.Pod))
		},
		UpdateFunc: func(obj runtime.Object) (runtime.Object, error) {
			return client.CoreV1().Pods(namespace).Update(obj.(*corev1.Pod))
		},
		DeleteFunc: func(name string, options *metav1.DeleteOptions) error {
			return client.CoreV1().Pods(namespace).Delete(name, options)
		},
	}
}

//nolint:dupl //false positive - lines are similar but not duplicated
func ForRole(client kubernetes.Interface, namespace string) Interface {
	return &InterfaceFuncs{
		GetFunc: func(name string, options metav1.GetOptions) (runtime.Object, error) {
			return client.RbacV1().Roles(namespace).Get(name, options)
		},
		CreateFunc: func(obj runtime.Object) (runtime.Object, error) {
			return client.RbacV1().Roles(namespace).Create(obj.(*rbacv1.Role))
		},
		UpdateFunc: func(obj runtime.Object) (runtime.Object, error) {
			return client.RbacV1().Roles(namespace).Update(obj.(*rbacv1.Role))
		},
		DeleteFunc: func(name string, options *metav1.DeleteOptions) error {
			return client.RbacV1().Roles(namespace).Delete(name, options)
		},
	}
}

//nolint:dupl //false positive - lines are similar but not duplicated
func ForRoleBinding(client kubernetes.Interface, namespace string) Interface {
	return &InterfaceFuncs{
		GetFunc: func(name string, options metav1.GetOptions) (runtime.Object, error) {
			return client.RbacV1().RoleBindings(namespace).Get(name, options)
		},
		CreateFunc: func(obj runtime.Object) (runtime.Object, error) {
			return client.RbacV1().RoleBindings(namespace).Create(obj.(*rbacv1.RoleBinding))
		},
		UpdateFunc: func(obj runtime.Object) (runtime.Object, error) {
			return client.RbacV1().RoleBindings(namespace).Update(obj.(*rbacv1.RoleBinding))
		},
		DeleteFunc: func(name string, options *metav1.DeleteOptions) error {
			return client.RbacV1().RoleBindings(namespace).Delete(name, options)
		},
	}
}

//nolint:dupl //false positive - lines are similar but not duplicated
func ForClusterRole(client kubernetes.Interface) Interface {
	return &InterfaceFuncs{
		GetFunc: func(name string, options metav1.GetOptions) (runtime.Object, error) {
			return client.RbacV1().ClusterRoles().Get(name, options)
		},
		CreateFunc: func(obj runtime.Object) (runtime.Object, error) {
			return client.RbacV1().ClusterRoles().Create(obj.(*rbacv1.ClusterRole))
		},
		UpdateFunc: func(obj runtime.Object) (runtime.Object, error) {
			return client.RbacV1().ClusterRoles().Update(obj.(*rbacv1.ClusterRole))
		},
		DeleteFunc: func(name string, options *metav1.DeleteOptions) error {
			return client.RbacV1().ClusterRoles().Delete(name, options)
		},
	}
}

//nolint:dupl //false positive - lines are similar but not duplicated
func ForClusterRoleBinding(client kubernetes.Interface) Interface {
	return &InterfaceFuncs{
		GetFunc: func(name string, options metav1.GetOptions) (runtime.Object, error) {
			return client.RbacV1().ClusterRoleBindings().Get(name, options)
		},
		CreateFunc: func(obj runtime.Object) (runtime.Object, error) {
			return client.RbacV1().ClusterRoleBindings().Create(obj.(*rbacv1.ClusterRoleBinding))
		},
		UpdateFunc: func(obj runtime.Object) (runtime.Object, error) {
			return client.RbacV1().ClusterRoleBindings().Update(obj.(*rbacv1.ClusterRoleBinding))
		},
		DeleteFunc: func(name string, options *metav1.DeleteOptions) error {
			return client.RbacV1().ClusterRoleBindings().Delete(name, options)
		},
	}
}

//nolint:dupl //false positive - lines are similar but not duplicated
func ForServiceAccount(client kubernetes.Interface, namespace string) Interface {
	return &InterfaceFuncs{
		GetFunc: func(name string, options metav1.GetOptions) (runtime.Object, error) {
			return client.CoreV1().ServiceAccounts(namespace).Get(name, options)
		},
		CreateFunc: func(obj runtime.Object) (runtime.Object, error) {
			return client.CoreV1().ServiceAccounts(namespace).Create(obj.(*corev1.ServiceAccount))
		},
		UpdateFunc: func(obj runtime.Object) (runtime.Object, error) {
			return client.CoreV1().ServiceAccounts(namespace).Update(obj.(*corev1.ServiceAccount))
		},
		DeleteFunc: func(name string, options *metav1.DeleteOptions) error {
			return client.CoreV1().ServiceAccounts(namespace).Delete(name, options)
		},
	}
}
