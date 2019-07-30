package kubefed

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	ctlutil "sigs.k8s.io/kubefed/pkg/controller/util"
	kubefedopt "sigs.k8s.io/kubefed/pkg/kubefedctl/options"
)

var _ = Describe("Federator", func() {
	Describe("function Distribute", testDistribute)
	Describe("helper function createFederatedResource", testCreateFederatedResource)
})

func testCreateFederatedResource() {
	var (
		resourceTyped      *corev1.Pod
		expectedTyped      *unstructured.Unstructured
		resource, expected runtime.Object
		f                  *federator
		err                error
		initObjs           []runtime.Object
		scheme             *runtime.Scheme
	)

	BeforeEach(func() {
		resourceTyped = newPod("test-pod")
		resource = resourceTyped
		scheme = runtime.NewScheme()
	})

	JustBeforeEach(func() {
		f = newFederatorWithScheme(scheme, initObjs...)
		expectedTyped, err = f.createFederatedResource(resource)
		expected = expectedTyped
	})

	When("the resource type scheme has NOT been added to the type registry", func() {
		When("failing to convert to Unstructured", func() {
			It("should throw an error", func() {
				Expect(err).To(MatchError(HavePrefix(errorUnstructuredConversion, "")))
			})

			It("should return a null expected", func() {
				Expect(expectedTyped).To(BeNil())
			})
		})
	})

	When("the resource type scheme has been added to the type registry", func() {
		BeforeEach(func() {
			corev1.AddToScheme(scheme)
		})

		It("should not modify the input resource but copy it", func() {
			Expect(resource).ToNot(BeIdenticalTo(expected))
		})

		It("should return no error on success", func() {
			Expect(err).ToNot(HaveOccurred())
		})

		It("the returned resource's Kind should be of Federated<kind>", func() {
			expectedKind := federatedKindPrefix + resourceTyped.GroupVersionKind().Kind
			Expect(expectedTyped.GroupVersionKind().Kind).
				To(Equal(expectedKind))
		})

		It("the returned resource's Group should be the default KubeFed FederatedGroup", func() {
			Expect(expectedTyped.GroupVersionKind().Group).
				To(Equal(kubefedopt.DefaultFederatedGroup))
		})

		It("the returned resource's Version should be the default KubeFed FederatedVersion", func() {
			Expect(expectedTyped.GroupVersionKind().Version).
				To(Equal(kubefedopt.DefaultFederatedVersion))
		})

		It("the returned resource's name should be the same as the input's object", func() {
			Expect(expectedTyped.GetName()).
				To(Equal(resourceTyped.GetName()))
		})

		It("the returned resource's template should be the input's unstructured", func() {
			// This test implicitly tests that the nested hierarchy of fields exists

			mapObj, found, err := getTemplateField(expectedTyped)
			targetResource := &unstructured.Unstructured{}
			// No need to check error code because it's covered by another test
			_ = f.scheme.Convert(resourceTyped, targetResource, nil)
			removeUnwantedFields(targetResource)

			Expect(err).ToNot(HaveOccurred())
			Expect(found).To(BeTrue(), "Hierarchy of fields missing!")
			Expect(mapObj["spec"]).To(BeEquivalentTo(targetResource.Object["spec"]))
			Expect(mapObj["metadata"]).To(BeEquivalentTo(targetResource.Object["metadata"]))
		})

		It("the returned resource's placement should be an empty matcher", func() {
			// This test implicitly tests that the nested hierarchy of fields exists
			mapObj, found, err := getMatchLabelsField(expectedTyped)
			Expect(err).ToNot(HaveOccurred())
			Expect(found).To(BeTrue(), "Hierarchy of fields missing!")
			Expect(mapObj).To(BeEmpty())
		})

		It("the returned resource's namespace should be the input's namespace", func() {
			Expect(expectedTyped.GetNamespace()).
				To(Equal(resourceTyped.GetNamespace()))
		})
	})
}

// func assertNestedFields(accessor interface{}, expectedObj types.GomegaMatcher, obj map[string]interface{}, fields ...string) { }
func testDistribute() {
	var (
		resourceTyped       *corev1.Pod
		fedResource, fedPod *unstructured.Unstructured
		f                   *federator
		err                 error
		initObjs            []runtime.Object
		clusterNames        []string
		scheme              *runtime.Scheme
	)

	BeforeEach(func() {
		resourceTyped = newPod("test-pod")
		scheme = runtime.NewScheme()
	})

	JustBeforeEach(func() {
		f = newFederatorWithScheme(scheme, initObjs...)
		err = f.Distribute(resourceTyped, clusterNames...)
	})

	When("the resource type scheme has NOT been added to the type registry", func() {
		It("should throw an error", func() {
			Expect(err).To(HaveOccurred())
		})
	})

	When("the resource type scheme has been added to the type registry", func() {
		BeforeEach(func() {
			corev1.AddToScheme(scheme)
			tmpFederator := newFederatorWithScheme(scheme)
			fedResource, _ = tmpFederator.createFederatedResource(resourceTyped)
		})

		JustBeforeEach(func() {
			fedPod = getResourceFromAPI(f.kubeFedClient, fedResource)
		})

		It("should return no error on success", func() {
			Expect(err).ToNot(HaveOccurred())
		})

		When("the resource is already in the Kube API", func() {
			expectedImage := "apache"

			BeforeEach(func() {
				initObjs = append(initObjs, fedResource)

				resourceTyped.Spec = corev1.PodSpec{
					Containers: []corev1.Container{
						corev1.Container{
							Image: expectedImage,
							Name:  "httpd",
						},
					},
				}
			})

			It("should update the resource", func() {
				fedPodObj, _, _ := getTemplateField(fedPod)
				expectObj, _, _ := getTemplateField(fedResource)

				Expect(fedPodObj["spec"]).ToNot(BeEquivalentTo(expectObj["spec"]))

				mapObj, _, _ := unstructured.NestedSlice(
					fedPod.Object,
					ctlutil.SpecField, ctlutil.TemplateField, "spec", "containers",
				)
				image := mapObj[0].(map[string]interface{})["image"]
				Expect(image).To(BeEquivalentTo(expectedImage))
			})
		})

		When("the resource is not in the Kube API", func() {
			It("should create the resource", func() {
				Expect(fedPod).ToNot(BeNil())
				Expect(fedPod).To(BeEquivalentTo(fedResource))
			})
		})

		When("the resource needs to be distributed to specific clusters", func() {
			BeforeEach(func() {
				clusterNames = []string{"cluster1", "cluster2"}
			})

			It("should set the placement to be an explicit list of clusters", func() {
				mapObj, found, err := getClustersField(fedPod)
				fedClusterNames := unpackClusterNames(mapObj)
				Expect(err).ToNot(HaveOccurred())
				Expect(found).To(BeTrue(), "Hierarchy of fields missing!")
				Expect(fedClusterNames).To(BeEquivalentTo(clusterNames))
			})

			It("the returned resource's placement should be an empty matcher", func() {
				// This test implicitly tests that the nested hierarchy of fields exists
				mapObj, found, err := getMatchLabelsField(fedPod)
				Expect(err).ToNot(HaveOccurred())
				Expect(found).ToNot(BeTrue(), "Hierarchy of fields is present!")
				Expect(mapObj).To(BeEmpty())
			})
		})
	})
}

func newPod(name string) *corev1.Pod {
	resourceTyped := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{Kind: "Pod"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "test-namespace",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				corev1.Container{
					Image: "nginx",
					Name:  "httpd",
				},
			},
		},
	}
	return resourceTyped
}

func newFederatorWithScheme(scheme *runtime.Scheme, initObjs ...runtime.Object) *federator {
	f := &federator{
		scheme: scheme,
		kubeFedClient: fake.NewFakeClientWithScheme(
			scheme,
			initObjs...,
		),
	}
	return f
}

func getTemplateField(resource *unstructured.Unstructured) (map[string]interface{}, bool, error) {
	return unstructured.NestedMap(
		resource.Object,
		ctlutil.SpecField, ctlutil.TemplateField,
	)
}

func getMatchLabelsField(resource *unstructured.Unstructured) (map[string]string, bool, error) {
	return unstructured.NestedStringMap(
		resource.Object,
		ctlutil.SpecField, ctlutil.PlacementField,
		ctlutil.ClusterSelectorField, ctlutil.MatchLabelsField,
	)
}

func getClustersField(resource *unstructured.Unstructured) ([]interface{}, bool, error) {
	return unstructured.NestedSlice(
		resource.Object,
		ctlutil.SpecField, ctlutil.PlacementField,
		ctlutil.ClustersField,
	)
}

func unpackClusterNames(object []interface{}) []string {
	clusterNames := []string{}
	for _, item := range object {
		clusterDict := item.(map[string]interface{})
		clusterName := clusterDict[ctlutil.NameField].(string)
		clusterNames = append(clusterNames, clusterName)
	}
	return clusterNames
}

func getResourceFromAPI(kubeFedClient client.Client, resource *unstructured.Unstructured) *unstructured.Unstructured {
	fedPod := &unstructured.Unstructured{}
	fedPod.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   resource.GroupVersionKind().Group,
		Kind:    resource.GroupVersionKind().Kind,
		Version: resource.GroupVersionKind().Version,
	})
	_ = kubeFedClient.Get(
		context.TODO(),
		client.ObjectKey{
			Namespace: resource.GetNamespace(),
			Name:      resource.GetName(),
		},
		fedPod,
	)
	return fedPod
}
