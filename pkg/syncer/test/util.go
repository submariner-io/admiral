package test

import (
	"time"

	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/federate"
	"github.com/submariner-io/admiral/pkg/util"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
)

const RemoteNamespace = "remote-ns"
const LocalNamespace = "local-ns"

func GetResourceAndError(resourceInterface dynamic.ResourceInterface, obj runtime.Object) (*unstructured.Unstructured, error) {
	meta, err := meta.Accessor(obj)
	Expect(err).To(Succeed())
	return resourceInterface.Get(meta.GetName(), metav1.GetOptions{})
}

func GetResource(resourceInterface dynamic.ResourceInterface, obj runtime.Object) *unstructured.Unstructured {
	resource, err := GetResourceAndError(resourceInterface, obj)
	Expect(err).To(Succeed())
	return resource
}

func CreateResource(resourceInterface dynamic.ResourceInterface, resource runtime.Object) *unstructured.Unstructured {
	obj, err := resourceInterface.Create(ToUnstructured(resource), metav1.CreateOptions{})
	Expect(err).To(Succeed())
	return obj
}

func UpdateResource(resourceInterface dynamic.ResourceInterface, resource runtime.Object) *unstructured.Unstructured {
	obj, err := resourceInterface.Update(ToUnstructured(resource), metav1.UpdateOptions{})
	Expect(err).To(Succeed())
	return obj
}

func VerifyResource(resourceInterface dynamic.ResourceInterface, expected *corev1.Pod, expNamespace, clusterID string) {
	raw := GetResource(resourceInterface, expected)

	actual := &corev1.Pod{}
	err := scheme.Scheme.Convert(raw, actual, nil)
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

func GetRESTMapperAndGroupVersionResourceFor(obj runtime.Object) (meta.RESTMapper, *schema.GroupVersionResource) {
	gvks, _, err := scheme.Scheme.ObjectKinds(obj)
	Expect(err).To(Succeed())
	Expect(gvks).ToNot(HaveLen(0))
	gvk := gvks[0]

	restMapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{gvk.GroupVersion()})
	restMapper.Add(gvk, meta.RESTScopeNamespace)

	mapping, err := restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	Expect(err).To(Succeed())
	return restMapper, &mapping.Resource
}

func PrepInitialClientObjs(namespace, clusterID string, initObjs ...runtime.Object) []runtime.Object {
	var newObjs []runtime.Object
	for _, obj := range initObjs {
		raw := ToUnstructured(obj)
		raw.SetNamespace(namespace)
		raw.SetUID(uuid.NewUUID())
		raw.SetResourceVersion("1")

		if clusterID != "" {
			labels := raw.GetLabels()
			if labels == nil {
				labels = map[string]string{}
			}
			labels[federate.ClusterIDLabelKey] = clusterID
			raw.SetLabels(labels)
		}

		newObjs = append(newObjs, raw)
	}

	return newObjs
}

func ToUnstructured(obj runtime.Object) *unstructured.Unstructured {
	raw, err := util.ToUnstructured(obj)
	Expect(err).To(Succeed())
	return raw
}

func SetClusterIDLabel(obj runtime.Object, clusterID string) runtime.Object {
	meta, err := meta.Accessor(obj)
	Expect(err).To(Succeed())

	labels := meta.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}

	labels[federate.ClusterIDLabelKey] = clusterID
	return obj
}

func WaitForResource(client dynamic.ResourceInterface, name string) *unstructured.Unstructured {
	var found *unstructured.Unstructured
	err := wait.PollImmediate(50*time.Millisecond, 5*time.Second, func() (bool, error) {
		obj, err := client.Get(name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}

		found = obj
		return true, nil
	})

	Expect(err).To(Succeed())
	return found
}
