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
	"context"

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
		GetFunc: func(ctx context.Context, name string, options metav1.GetOptions) (runtime.Object, error) {
			return client.AppsV1().Deployments(namespace).Get(ctx, name, options)
		},
		CreateFunc: func(ctx context.Context, obj runtime.Object, options metav1.CreateOptions) (runtime.Object, error) {
			return client.AppsV1().Deployments(namespace).Create(ctx, obj.(*appsv1.Deployment), options)
		},
		UpdateFunc: func(ctx context.Context, obj runtime.Object, options metav1.UpdateOptions) (runtime.Object, error) {
			return client.AppsV1().Deployments(namespace).Update(ctx, obj.(*appsv1.Deployment), options)
		},
		DeleteFunc: func(ctx context.Context, name string, options metav1.DeleteOptions) error {
			return client.AppsV1().Deployments(namespace).Delete(ctx, name, options)
		},
	}
}

//nolint:dupl //false positive - lines are similar but not duplicated
func ForDaemonSet(client kubernetes.Interface, namespace string) Interface {
	return &InterfaceFuncs{
		GetFunc: func(ctx context.Context, name string, options metav1.GetOptions) (runtime.Object, error) {
			return client.AppsV1().DaemonSets(namespace).Get(ctx, name, options)
		},
		CreateFunc: func(ctx context.Context, obj runtime.Object, options metav1.CreateOptions) (runtime.Object, error) {
			return client.AppsV1().DaemonSets(namespace).Create(ctx, obj.(*appsv1.DaemonSet), options)
		},
		UpdateFunc: func(ctx context.Context, obj runtime.Object, options metav1.UpdateOptions) (runtime.Object, error) {
			return client.AppsV1().DaemonSets(namespace).Update(ctx, obj.(*appsv1.DaemonSet), options)
		},
		DeleteFunc: func(ctx context.Context, name string, options metav1.DeleteOptions) error {
			return client.AppsV1().DaemonSets(namespace).Delete(ctx, name, options)
		},
	}
}

//nolint:dupl //false positive - lines are similar but not duplicated
func ForPod(client kubernetes.Interface, namespace string) Interface {
	return &InterfaceFuncs{
		GetFunc: func(ctx context.Context, name string, options metav1.GetOptions) (runtime.Object, error) {
			return client.CoreV1().Pods(namespace).Get(ctx, name, options)
		},
		CreateFunc: func(ctx context.Context, obj runtime.Object, options metav1.CreateOptions) (runtime.Object, error) {
			return client.CoreV1().Pods(namespace).Create(ctx, obj.(*corev1.Pod), options)
		},
		UpdateFunc: func(ctx context.Context, obj runtime.Object, options metav1.UpdateOptions) (runtime.Object, error) {
			return client.CoreV1().Pods(namespace).Update(ctx, obj.(*corev1.Pod), options)
		},
		DeleteFunc: func(ctx context.Context, name string, options metav1.DeleteOptions) error {
			return client.CoreV1().Pods(namespace).Delete(ctx, name, options)
		},
	}
}

//nolint:dupl //false positive - lines are similar but not duplicated
func ForRole(client kubernetes.Interface, namespace string) Interface {
	return &InterfaceFuncs{
		GetFunc: func(ctx context.Context, name string, options metav1.GetOptions) (runtime.Object, error) {
			return client.RbacV1().Roles(namespace).Get(ctx, name, options)
		},
		CreateFunc: func(ctx context.Context, obj runtime.Object, options metav1.CreateOptions) (runtime.Object, error) {
			return client.RbacV1().Roles(namespace).Create(ctx, obj.(*rbacv1.Role), options)
		},
		UpdateFunc: func(ctx context.Context, obj runtime.Object, options metav1.UpdateOptions) (runtime.Object, error) {
			return client.RbacV1().Roles(namespace).Update(ctx, obj.(*rbacv1.Role), options)
		},
		DeleteFunc: func(ctx context.Context, name string, options metav1.DeleteOptions) error {
			return client.RbacV1().Roles(namespace).Delete(ctx, name, options)
		},
	}
}

//nolint:dupl //false positive - lines are similar but not duplicated
func ForRoleBinding(ctx context.Context, client kubernetes.Interface, namespace string) Interface {
	return &InterfaceFuncs{
		GetFunc: func(ctx context.Context, name string, options metav1.GetOptions) (runtime.Object, error) {
			return client.RbacV1().RoleBindings(namespace).Get(ctx, name, options)
		},
		CreateFunc: func(ctx context.Context, obj runtime.Object, options metav1.CreateOptions) (runtime.Object, error) {
			return client.RbacV1().RoleBindings(namespace).Create(ctx, obj.(*rbacv1.RoleBinding), options)
		},
		UpdateFunc: func(ctx context.Context, obj runtime.Object, options metav1.UpdateOptions) (runtime.Object, error) {
			return client.RbacV1().RoleBindings(namespace).Update(ctx, obj.(*rbacv1.RoleBinding), options)
		},
		DeleteFunc: func(ctx context.Context, name string, options metav1.DeleteOptions) error {
			return client.RbacV1().RoleBindings(namespace).Delete(ctx, name, options)
		},
	}
}

//nolint:dupl //false positive - lines are similar but not duplicated
func ForClusterRole(client kubernetes.Interface) Interface {
	return &InterfaceFuncs{
		GetFunc: func(ctx context.Context, name string, options metav1.GetOptions) (runtime.Object, error) {
			return client.RbacV1().ClusterRoles().Get(ctx, name, options)
		},
		CreateFunc: func(ctx context.Context, obj runtime.Object, options metav1.CreateOptions) (runtime.Object, error) {
			return client.RbacV1().ClusterRoles().Create(ctx, obj.(*rbacv1.ClusterRole), options)
		},
		UpdateFunc: func(ctx context.Context, obj runtime.Object, options metav1.UpdateOptions) (runtime.Object, error) {
			return client.RbacV1().ClusterRoles().Update(ctx, obj.(*rbacv1.ClusterRole), options)
		},
		DeleteFunc: func(ctx context.Context, name string, options metav1.DeleteOptions) error {
			return client.RbacV1().ClusterRoles().Delete(ctx, name, options)
		},
	}
}

//nolint:dupl //false positive - lines are similar but not duplicated
func ForClusterRoleBinding(client kubernetes.Interface) Interface {
	return &InterfaceFuncs{
		GetFunc: func(ctx context.Context, name string, options metav1.GetOptions) (runtime.Object, error) {
			return client.RbacV1().ClusterRoleBindings().Get(ctx, name, options)
		},
		CreateFunc: func(ctx context.Context, obj runtime.Object, options metav1.CreateOptions) (runtime.Object, error) {
			return client.RbacV1().ClusterRoleBindings().Create(ctx, obj.(*rbacv1.ClusterRoleBinding), options)
		},
		UpdateFunc: func(ctx context.Context, obj runtime.Object, options metav1.UpdateOptions) (runtime.Object, error) {
			return client.RbacV1().ClusterRoleBindings().Update(ctx, obj.(*rbacv1.ClusterRoleBinding), options)
		},
		DeleteFunc: func(ctx context.Context, name string, options metav1.DeleteOptions) error {
			return client.RbacV1().ClusterRoleBindings().Delete(ctx, name, options)
		},
	}
}

//nolint:dupl //false positive - lines are similar but not duplicated
func ForServiceAccount(ctx context.Context, client kubernetes.Interface, namespace string) Interface {
	return &InterfaceFuncs{
		GetFunc: func(ctx context.Context, name string, options metav1.GetOptions) (runtime.Object, error) {
			return client.CoreV1().ServiceAccounts(namespace).Get(ctx, name, options)
		},
		CreateFunc: func(ctx context.Context, obj runtime.Object, options metav1.CreateOptions) (runtime.Object, error) {
			return client.CoreV1().ServiceAccounts(namespace).Create(ctx, obj.(*corev1.ServiceAccount), options)
		},
		UpdateFunc: func(ctx context.Context, obj runtime.Object, options metav1.UpdateOptions) (runtime.Object, error) {
			return client.CoreV1().ServiceAccounts(namespace).Update(ctx, obj.(*corev1.ServiceAccount), options)
		},
		DeleteFunc: func(ctx context.Context, name string, options metav1.DeleteOptions) error {
			return client.CoreV1().ServiceAccounts(namespace).Delete(ctx, name, options)
		},
	}
}
