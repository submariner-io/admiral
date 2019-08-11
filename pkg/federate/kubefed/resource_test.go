package kubefed

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kuberrors "k8s.io/apimachinery/pkg/api/errors"
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
		actual   *unstructured.Unstructured
		err      error
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
		BeforeEach(func() {
			actual, err = createFederatedResource(scheme, resource)
		})

		It("should throw an error, failing to convert to Unstructured", func() {
			var expectedType *UnstructuredConversionError
			Expect(err).To(HaveOccurred())
			Expect(err).To(BeAssignableToTypeOf(expectedType))
		})

		It("should return a nil value", func() {
			Expect(actual).To(BeNil())
		})
	})

	When("the resource type scheme has been added to the type registry", func() {
		BeforeEach(func() {
			Expect(corev1.AddToScheme(scheme)).To(Succeed())
		})

		JustBeforeEach(func() {
			actual, err = createFederatedResource(scheme, resource)
			Expect(err).ToNot(HaveOccurred())
		})

		It("the returned resource's Kind should be of Federated<kind>", func() {
			expectedKind := federatedKindPrefix + resource.GroupVersionKind().Kind
			Expect(actual.GroupVersionKind().Kind).To(Equal(expectedKind))
		})

		It("the returned resource's Group should be the default KubeFed FederatedGroup", func() {
			Expect(actual.GroupVersionKind().Group).
				To(Equal(kubefedopt.DefaultFederatedGroup))
		})

		It("the returned resource's Version should be the default KubeFed FederatedVersion", func() {
			Expect(actual.GroupVersionKind().Version).
				To(Equal(kubefedopt.DefaultFederatedVersion))
		})

		It("the returned resource's name should be the same as the input's object", func() {
			Expect(actual.GetName()).To(Equal(resource.GetName()))
		})

		It("the returned resource's namespace should be the input's namespace", func() {
			Expect(actual.GetNamespace()).To(Equal(resource.GetNamespace()))
		})

		It("the returned resource's template should be the input's unstructured", func() {
			// This test implicitly tests that the nested hierarchy of fields exists
			targetResource := &unstructured.Unstructured{}
			Expect(scheme.Convert(resource, targetResource, nil)).To(Succeed())

			mapObj := getTemplateField(actual)
			Expect(mapObj["spec"]).To(BeEquivalentTo(targetResource.Object["spec"]))
			Expect(mapObj["metadata"]).To(BeEmpty())
		})

		It("the returned resource's placement should be an empty matcher", func() {
			// This test implicitly tests that the nested hierarchy of fields exists
			mapObj, found := getMatchLabelsField(actual)
			Expect(found).To(BeTrue(), "Hierarchy of the MatchLabels field missing!")
			Expect(mapObj).To(BeEmpty())
		})
	})
}

func testDistribute() {
	var (
		resource      *corev1.Pod
		fedResource   *unstructured.Unstructured
		f             *federator
		initObjs      []runtime.Object
		scheme        *runtime.Scheme
		clientPatcher client.Client
		err           error
	)

	BeforeEach(func() {
		resource = newPod("test-pod", "nginx")
		scheme = runtime.NewScheme()
		clientPatcher = newFakeClient(scheme, initObjs...)
	})

	AfterEach(func() {
		resource = nil
		fedResource = nil
		f = nil
		initObjs = []runtime.Object{}
		scheme = nil
		clientPatcher = nil
	})

	JustBeforeEach(func() {
		f = newFederatorWithScheme(scheme, clientPatcher)
	})

	When("the resource type scheme has NOT been added to the type registry", func() {
		It("should throw an error", func() {
			err = f.Distribute(resource)
			Expect(err).To(HaveOccurred())
		})
	})

	When("the resource type scheme has been added to the type registry", func() {
		BeforeEach(func() {
			Expect(corev1.AddToScheme(scheme)).To(Succeed())
			fedResource, err = createFederatedResource(scheme, resource)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should return no error on success", func() {
			Expect(f.Distribute(resource)).To(Succeed())
		})

		When("the resource is already in the Kube API", func() {
			expectedImage := "apache"

			BeforeEach(func() {
				initObjs = append(initObjs, fedResource)
				resource = newPod("test-pod", expectedImage)
			})

			It("should update the resource", func() {
				Expect(f.Distribute(resource)).To(Succeed())

				fedPod, err := getResourceFromAPI(f.kubeFedClient, fedResource)
				Expect(err).ToNot(HaveOccurred())
				fedPodObj := getTemplateField(fedPod)
				expectObj := getTemplateField(fedResource)
				Expect(fedPodObj).ToNot(BeEquivalentTo(expectObj))

				mapObj, found, err := unstructured.NestedSlice(fedPodObj, "spec", "containers")
				Expect(err).ToNot(HaveOccurred())
				Expect(found).To(BeTrue(), "Hierarchy of the spec/containers missing!")
				Expect(mapObj[0]).To(HaveKeyWithValue("image", expectedImage))
			})
		})

		When("on update the Kube API returns an error that is not a NotFound", func() {
			BeforeEach(func() {
				clientPatcher = &fakeClientWithUpdateError{fake.NewFakeClient(), "test error"}
			})

			It("should return an error and not create the resource", func() {
				err = f.Distribute(resource)
				Expect(err).To(MatchError(HavePrefix("test error")))
			})
		})

		When("the resource is not in the Kube API", func() {
			It("should create the resource", func() {
				// Sanity check
				_, err = getResourceFromAPI(f.kubeFedClient, fedResource)
				Expect(err).To(HaveOccurred())
				Expect(kuberrors.IsNotFound(err)).To(BeTrue())

				Expect(f.Distribute(resource)).To(Succeed())

				fedPod, err := getResourceFromAPI(f.kubeFedClient, fedResource)
				Expect(err).ToNot(HaveOccurred())
				Expect(fedPod).ToNot(BeNil())
				Expect(fedPod).To(BeEquivalentTo(fedResource))
			})
		})

		When("the resource doesn't need to be distributed to specific clusters", func() {
			It("the returned resource's placement should be an empty matcher", func() {
				// This test implicitly tests that the nested hierarchy of fields exists
				Expect(f.Distribute(resource)).To(Succeed())

				fedPod, err := getResourceFromAPI(f.kubeFedClient, fedResource)
				Expect(err).ToNot(HaveOccurred())

				mapObj, found := getMatchLabelsField(fedPod)
				Expect(found).To(BeTrue(), "Hierarchy of the MatchLabels field missing!")
				Expect(mapObj).To(BeEmpty())
			})
		})

		When("the resource needs to be distributed to specific clusters", func() {
			clusterNames := []string{"cluster1", "cluster2"}

			It("should set the placement to be an explicit list of clusters", func() {
				// This test implicitly tests that the nested hierarchy of fields exists
				Expect(f.Distribute(resource, clusterNames...)).To(Succeed())

				fedPod, err := getResourceFromAPI(f.kubeFedClient, fedResource)
				Expect(err).ToNot(HaveOccurred())

				mapObj, found, err := getClustersField(fedPod)
				Expect(err).ToNot(HaveOccurred())
				Expect(found).To(BeTrue(), "Hierarchy of the Cluster field missing!")
				Expect(unpackClusterNames(mapObj)).To(BeEquivalentTo(clusterNames))
			})

			It("the returned resource's placement should not contain an empty matcher", func() {
				// This test implicitly tests that the nested hierarchy of fields exists
				Expect(f.Distribute(resource, clusterNames...)).To(Succeed())

				fedPod, err := getResourceFromAPI(f.kubeFedClient, fedResource)
				Expect(err).ToNot(HaveOccurred())

				mapObj, found := getMatchLabelsField(fedPod)
				Expect(found).ToNot(BeTrue(), "Hierarchy of the MatchLabels field is present!")
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

func newFederatorWithScheme(scheme *runtime.Scheme, client client.Client) *federator {
	return &federator{
		scheme:        scheme,
		kubeFedClient: client,
	}
}

func newFakeClient(scheme *runtime.Scheme, initObjs ...runtime.Object) client.Client {
	return fake.NewFakeClientWithScheme(
		scheme,
		initObjs...,
	)
}

func getTemplateField(resource *unstructured.Unstructured) map[string]interface{} {
	field, found, err := unstructured.NestedMap(
		resource.Object,
		ctlutil.SpecField, ctlutil.TemplateField,
	)
	Expect(err).ToNot(HaveOccurred())
	Expect(found).To(BeTrue(), "Hierarchy of the Template field missing!")
	return field
}

func getMatchLabelsField(resource *unstructured.Unstructured) (map[string]string, bool) {
	field, found, err := unstructured.NestedStringMap(
		resource.Object,
		ctlutil.SpecField, ctlutil.PlacementField,
		ctlutil.ClusterSelectorField, ctlutil.MatchLabelsField,
	)
	Expect(err).ToNot(HaveOccurred())
	return field, found
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
