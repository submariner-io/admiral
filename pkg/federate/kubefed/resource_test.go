package kubefed

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	ctlutil "sigs.k8s.io/kubefed/pkg/controller/util"
	kubefedopt "sigs.k8s.io/kubefed/pkg/kubefedctl/options"
)

type fakeClientWithUpdateError struct {
	client.Client
	msg string
}

var _ client.Client = &fakeClientWithUpdateError{}

func (fake *fakeClientWithUpdateError) Update(ctx context.Context, obj runtime.Object) error {
	return errors.New(fake.msg)
}

var _ = Describe("Federator", func() {
	Describe("function Distribute", testDistribute)
	Describe("helper function createFederatedResource", testCreateFederatedResource)
})

func testCreateFederatedResource() {
	var (
		resource *corev1.Pod
		scheme   *runtime.Scheme
	)

	BeforeEach(func() {
		resource = newPod("test-pod", "nginx")
		scheme = runtime.NewScheme()
	})

	AfterEach(func() {
		resource = nil
		scheme = nil
	})

	When("the resource type scheme has NOT been added to the type registry", func() {
		When("failing to convert to Unstructured", func() {
			It("should throw an error", func() {
				var expectedType *UnstructuredConversionError
				_, err := createFederatedResource(scheme, resource)
				Expect(err).To(HaveOccurred())
				Expect(err).To(BeAssignableToTypeOf(expectedType))
			})

			It("should return a null expected", func() {
				expected, _ := createFederatedResource(scheme, resource)
				Expect(expected).To(BeNil())
			})
		})
	})

	When("the resource type scheme has been added to the type registry", func() {
		BeforeEach(func() {
			err := corev1.AddToScheme(scheme)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should not modify the input resource but copy it", func() {
			expected, _ := createFederatedResource(scheme, resource)
			Expect(expected).ToNot(BeIdenticalTo(resource))
		})

		It("the returned resource's Kind should be of Federated<kind>", func() {
			expected, err := createFederatedResource(scheme, resource)
			Expect(err).ToNot(HaveOccurred())
			expectedKind := federatedKindPrefix + resource.GroupVersionKind().Kind
			Expect(expected.GroupVersionKind().Kind).
				To(Equal(expectedKind))
		})

		It("the returned resource's Group should be the default KubeFed FederatedGroup", func() {
			expected, err := createFederatedResource(scheme, resource)
			Expect(err).ToNot(HaveOccurred())
			Expect(expected.GroupVersionKind().Group).
				To(Equal(kubefedopt.DefaultFederatedGroup))
		})

		It("the returned resource's Version should be the default KubeFed FederatedVersion", func() {
			expected, err := createFederatedResource(scheme, resource)
			Expect(err).ToNot(HaveOccurred())
			Expect(expected.GroupVersionKind().Version).
				To(Equal(kubefedopt.DefaultFederatedVersion))
		})

		It("the returned resource's name should be the same as the input's object", func() {
			expected, err := createFederatedResource(scheme, resource)
			Expect(err).ToNot(HaveOccurred())
			Expect(expected.GetName()).
				To(Equal(resource.GetName()))
		})

		It("the returned resource's template should be the input's unstructured", func() {
			// This test implicitly tests that the nested hierarchy of fields exists
			expected, err := createFederatedResource(scheme, resource)
			Expect(err).ToNot(HaveOccurred())

			targetResource := &unstructured.Unstructured{}

			err = scheme.Convert(resource, targetResource, nil)
			Expect(err).ToNot(HaveOccurred())

			mapObj, found, err := getTemplateField(expected)
			Expect(err).ToNot(HaveOccurred())
			Expect(found).To(BeTrue(), "Hierarchy of the Template field missing!")
			Expect(mapObj["spec"]).To(BeEquivalentTo(targetResource.Object["spec"]))
			Expect(mapObj["metadata"]).To(BeEmpty())
		})

		It("the returned resource's placement should be an empty matcher", func() {
			// This test implicitly tests that the nested hierarchy of fields exists
			expected, err := createFederatedResource(scheme, resource)
			Expect(err).ToNot(HaveOccurred())

			mapObj, found, err := getMatchLabelsField(expected)
			Expect(err).ToNot(HaveOccurred())
			Expect(found).To(BeTrue(), "Hierarchy of the MatchLabels field missing!")
			Expect(mapObj).To(BeEmpty())
		})

		It("the returned resource's namespace should be the input's namespace", func() {
			expected, err := createFederatedResource(scheme, resource)
			Expect(err).ToNot(HaveOccurred())
			Expect(expected.GetNamespace()).
				To(Equal(resource.GetNamespace()))
		})
	})
}

func testDistribute() {
	var (
		resource      *corev1.Pod
		fedResource   *unstructured.Unstructured
		f             *federator
		initObjs      []runtime.Object
		clusterNames  []string
		scheme        *runtime.Scheme
		clientPatcher client.Client
	)

	BeforeEach(func() {
		resource = newPod("test-pod", "nginx")
		scheme = runtime.NewScheme()
	})

	AfterEach(func() {
		resource = nil
		fedResource = nil
		f = nil
		initObjs = []runtime.Object{}
		clusterNames = []string{}
		scheme = nil
		clientPatcher = nil
	})

	JustBeforeEach(func() {
		f = newFederatorWithScheme(scheme, clientPatcher, initObjs...)
	})

	When("the resource type scheme has NOT been added to the type registry", func() {
		It("should throw an error", func() {
			err := f.Distribute(resource, clusterNames...)
			Expect(err).To(HaveOccurred())
		})
	})

	When("the resource type scheme has been added to the type registry", func() {
		BeforeEach(func() {
			err := corev1.AddToScheme(scheme)
			Expect(err).ToNot(HaveOccurred())
			fedResource, _ = createFederatedResource(scheme, resource)
		})

		It("should return no error on success", func() {
			err := f.Distribute(resource, clusterNames...)
			Expect(err).ToNot(HaveOccurred())
		})

		When("the resource is already in the Kube API", func() {
			expectedImage := "apache"

			BeforeEach(func() {
				initObjs = append(initObjs, fedResource)
				resource = newPod("test-pod", expectedImage)
			})

			It("should update the resource", func() {
				_ = f.Distribute(resource, clusterNames...)
				fedPod, err := getResourceFromAPI(f.kubeFedClient, fedResource)
				Expect(err).ToNot(HaveOccurred())
				fedPodObj, found, err := getTemplateField(fedPod)
				Expect(err).ToNot(HaveOccurred())
				Expect(found).To(BeTrue(), "Hierarchy of the Template field missing!")
				expectObj, found, err := getTemplateField(fedResource)
				Expect(err).ToNot(HaveOccurred())
				Expect(found).To(BeTrue(), "Hierarchy of the Template field missing!")

				Expect(fedPodObj["spec"]).ToNot(BeEquivalentTo(expectObj["spec"]))

				mapObj, _, _ := unstructured.NestedSlice(
					fedPod.Object,
					ctlutil.SpecField, ctlutil.TemplateField, "spec", "containers",
				)
				image := mapObj[0].(map[string]interface{})["image"]
				Expect(image).To(BeEquivalentTo(expectedImage))
			})
		})

		When("on update the Kube API returns an error that is not a NotFound", func() {
			BeforeEach(func() {
				clientPatcher = &fakeClientWithUpdateError{fake.NewFakeClient(), "test error"}
			})

			It("should return an error and not create the resource", func() {
				err := f.Distribute(resource, clusterNames...)
				Expect(err).To(MatchError(HavePrefix("test error")))
			})
		})

		When("the resource is not in the Kube API", func() {
			It("should create the resource", func() {
				// Sanity check
				_, err := getResourceFromAPI(f.kubeFedClient, fedResource)
				Expect(err).To(HaveOccurred())

				_ = f.Distribute(resource, clusterNames...)
				fedPod, err := getResourceFromAPI(f.kubeFedClient, fedResource)
				Expect(err).ToNot(HaveOccurred())
				Expect(fedPod).ToNot(BeNil())
				Expect(fedPod).To(BeEquivalentTo(fedResource))
			})
		})

		When("the resource needs to be distributed to specific clusters", func() {
			BeforeEach(func() {
				clusterNames = []string{"cluster1", "cluster2"}
			})

			It("should set the placement to be an explicit list of clusters", func() {
				// This test implicitly tests that the nested hierarchy of fields exists
				_ = f.Distribute(resource, clusterNames...)
				fedPod, err := getResourceFromAPI(f.kubeFedClient, fedResource)
				Expect(err).ToNot(HaveOccurred())

				mapObj, found, err := getClustersField(fedPod)
				Expect(err).ToNot(HaveOccurred())
				Expect(found).To(BeTrue(), "Hierarchy of the Cluster field missing!")
				Expect(unpackClusterNames(mapObj)).To(BeEquivalentTo(clusterNames))
			})

			It("the returned resource's placement should not contain an empty matcher", func() {
				// This test implicitly tests that the nested hierarchy of fields exists
				_ = f.Distribute(resource, clusterNames...)
				fedPod, err := getResourceFromAPI(f.kubeFedClient, fedResource)
				Expect(err).ToNot(HaveOccurred())

				mapObj, found, err := getMatchLabelsField(fedPod)
				Expect(err).ToNot(HaveOccurred())
				Expect(found).ToNot(BeTrue(), "Hierarchy of the MatchLabels field is present!")
				Expect(mapObj).To(BeEmpty())
			})
		})

		When("the resource needs to be distributed to specific clusters", func() {
			It("the returned resource's placement should be an empty matcher", func() {
				// This test implicitly tests that the nested hierarchy of fields exists
				_ = f.Distribute(resource)
				fedPod, err := getResourceFromAPI(f.kubeFedClient, fedResource)
				Expect(err).ToNot(HaveOccurred())

				mapObj, found, err := getMatchLabelsField(fedPod)
				Expect(err).ToNot(HaveOccurred())
				Expect(found).To(BeTrue(), "Hierarchy of the MatchLabels field missing!")
				Expect(mapObj).To(BeEmpty())
			})
		})
	})
}

func newPod(name string, imageName string) *corev1.Pod {
	return &corev1.Pod{
		TypeMeta: metav1.TypeMeta{Kind: "Pod"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "test-namespace",
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

func newFederatorWithScheme(scheme *runtime.Scheme, client client.Client, initObjs ...runtime.Object) *federator {
	if client == nil {
		client = fake.NewFakeClientWithScheme(
			scheme,
			initObjs...,
		)
	}

	return &federator{
		scheme:        scheme,
		kubeFedClient: client,
	}
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

func getResourceFromAPI(kubeFedClient client.Client, resource *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	fedResource := &unstructured.Unstructured{}
	fedResource.SetGroupVersionKind(resource.GroupVersionKind())
	err := kubeFedClient.Get(
		context.TODO(),
		client.ObjectKey{
			Namespace: resource.GetNamespace(),
			Name:      resource.GetName(),
		},
		fedResource,
	)
	return fedResource, err
}
