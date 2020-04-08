package test

import (
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/federate"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
)

func GetResource(resourceInterface dynamic.ResourceInterface, obj runtime.Object) (*unstructured.Unstructured, error) {
	meta, err := meta.Accessor(obj)
	Expect(err).To(Succeed())
	return resourceInterface.Get(meta.GetName(), metav1.GetOptions{})
}

func VerifyResource(resourceInterface dynamic.ResourceInterface, expected *corev1.Pod, expNamespace, clusterID string) {
	raw, err := GetResource(resourceInterface, expected)
	Expect(err).To(Succeed())

	actual := &corev1.Pod{}
	err = scheme.Scheme.Convert(raw, actual, nil)
	Expect(err).To(Succeed())

	Expect(actual.GetName()).To(Equal(expected.GetName()))
	Expect(actual.GetNamespace()).To(Equal(expNamespace))
	Expect(actual.GetAnnotations()).To(Equal(expected.GetAnnotations()))
	Expect(actual.Spec).To(Equal(expected.Spec))

	Expect(actual.GetUID()).NotTo(Equal(expected.GetUID()))
	Expect(actual.GetResourceVersion()).NotTo(Equal(expected.GetResourceVersion()))

	copy := make(map[string]string)
	for k, v := range expected.GetLabels() {
		copy[k] = v
	}

	if clusterID != "" {
		copy[federate.ClusterIDLabelKey] = clusterID
	}

	Expect(actual.GetLabels()).To(Equal(copy))
}

func NewPod(namespace string) *corev1.Pod {
	return NewPodWithImage(namespace, "nginx")
}

func NewPodWithImage(namespace, imageName string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test-pod",
			Namespace:       namespace,
			UID:             uuid.NewUUID(),
			ResourceVersion: "10",
			Labels:          map[string]string{"app": "test"},
			Annotations:     map[string]string{"foo": "bar"},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				corev1.Container{
					Image: imageName,
					Name:  "httpd",
				},
			},
		},
	}
}
