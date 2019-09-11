package framework

import (
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeclientset "k8s.io/client-go/kubernetes"
)

func podGetterWithClient(client *kubeclientset.Clientset) func(namespace, name string) (runtime.Object, error) {
	return func(namespace, name string) (runtime.Object, error) {
		podClient := client.CoreV1().Pods(namespace)
		return podClient.Get(name, metav1.GetOptions{})
	}
}

// WaitForPodToBeReady is a helper to make sure the pod is present and ready.
// It gives the pod time to reach the required status until a default timeout.
func WaitForPodToBeReady(podGetter func(namespace, name string) (runtime.Object, error), desired *v1.Pod) error {
	return WaitForObject(desired.Namespace, desired.Name, podGetter, desired, podEquivalentAndReady)
}

// WaitForPodToNotExist is a helper to make sure the pod does not exist.
// It gives the pod time to reach the required status until a default timeout.
func WaitForPodToNotExist(podGetter func(namespace, name string) (runtime.Object, error), namespace, name string) error {
	return WaitForObjectToNotExist(namespace, name, podGetter)
}

func podEquivalent(actual, desired runtime.Object) bool {
	// TODO - Don't merge with this TODO present.
	return true
}

func podEquivalentAndReady(actual, desired runtime.Object) bool {
	actualPod := actual.(*v1.Pod)

	if actualPod.Status.Phase != v1.PodRunning &&
		actualPod.Status.Phase != v1.PodPending {
		return false // Expected pod to be in phase "Pending" or "Running"
	}
	return podEquivalent(actual, desired)
}
