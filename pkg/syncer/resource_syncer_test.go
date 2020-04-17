package syncer_test

import (
	"errors"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/federate/fake"
	. "github.com/submariner-io/admiral/pkg/gomega"
	"github.com/submariner-io/admiral/pkg/syncer"
	"github.com/submariner-io/admiral/pkg/syncer/test"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	fakeClient "k8s.io/client-go/dynamic/fake"
)

var _ = Describe("Resource Syncer", func() {
	Describe("Local -> Remote", testLocalToRemote)

	Describe("Remote -> Local", func() {
		Context("with a local cluster ID", testRemoteToLocalWithLocalClusterID)
		Context("without a local cluster ID", testRemoteToLocalWithoutLocalClusterID)
	})

	Describe("With Transform Function", testTransformFunction)
	Describe("Sync Errors", testSyncErrors)
	Describe("Update Suppression", testUpdateSuppression)
})

func testLocalToRemote() {
	d := newTestDiver(test.LocalNamespace, "", syncer.LocalToRemote)

	When("a resource without a cluster ID label is created in the local datastore", func() {
		d.verifyDistributeOnCreateTest("")
	})

	When("a resource without a cluster ID label is updated in the local datastore", func() {
		d.verifyDistributeOnUpdateTest("")
	})

	When("a resource without a cluster ID label is deleted from the local datastore", func() {
		d.verifyDistributeOnDeleteTest("")
	})

	When("a resource with a cluster ID label is created in the local datastore", func() {
		d.verifyNoDistributeOnCreateTest("remote")
	})

	When("a resource with a cluster ID label is updated in the local datastore", func() {
		d.verifyNoDistributeOnUpdateTest("remote")
	})

	When("a resource with a cluster ID label is deleted from the local datastore", func() {
		d.verifyNoDistributeOnDeleteTest("remote")
	})
}

func testRemoteToLocalWithLocalClusterID() {
	d := newTestDiver(test.RemoteNamespace, "local", syncer.RemoteToLocal)

	When("a resource with a non-local cluster ID label is created in the remote datastore", func() {
		d.verifyDistributeOnCreateTest("remote")
	})

	When("a resource with a non-local cluster ID label is updated in the remote datastore", func() {
		d.verifyDistributeOnUpdateTest("remote")
	})

	When("a resource with a non-local cluster ID label is deleted from the remote datastore", func() {
		d.verifyDistributeOnDeleteTest("remote")
	})

	When("a resource with a local cluster ID label is created in the remote datastore", func() {
		d.verifyNoDistributeOnCreateTest(d.config.LocalClusterID)
	})

	When("a resource with a local cluster ID label is updated in the remote datastore", func() {
		d.verifyNoDistributeOnUpdateTest(d.config.LocalClusterID)
	})

	When("a resource with a local cluster ID label is deleted from the remote datastore", func() {
		d.verifyNoDistributeOnDeleteTest(d.config.LocalClusterID)
	})

	When("a resource without a cluster ID label is created in the remote datastore", func() {
		d.verifyNoDistributeOnCreateTest("")
	})

	When("a resource without a cluster ID label is updated in the remote datastore", func() {
		d.verifyNoDistributeOnUpdateTest("")
	})

	When("a resource without a local cluster ID label is deleted from the remote datastore", func() {
		d.verifyNoDistributeOnDeleteTest("")
	})
}

func testRemoteToLocalWithoutLocalClusterID() {
	d := newTestDiver(test.RemoteNamespace, "", syncer.RemoteToLocal)

	When("a resource with a cluster ID label is created in the remote datastore", func() {
		d.verifyDistributeOnCreateTest("remote")
	})

	When("a resource with a cluster ID label is updated in the remote datastore", func() {
		d.verifyDistributeOnUpdateTest("remote")
	})

	When("a resource with a cluster ID label is deleted from the remote datastore", func() {
		d.verifyDistributeOnCreateTest("remote")
	})

	When("a resource without a cluster ID label is created in the remote datastore", func() {
		d.verifyDistributeOnCreateTest("")
	})

	When("a resource without a cluster ID label is updated in the local datastore", func() {
		d.verifyDistributeOnUpdateTest("")
	})

	When("a resource without a cluster ID label is deleted from the local datastore", func() {
		d.verifyDistributeOnCreateTest("")
	})
}

func testTransformFunction() {
	d := newTestDiver(test.LocalNamespace, "", syncer.LocalToRemote)

	var transformed *corev1.Pod

	BeforeEach(func() {
		transformed = test.NewPodWithImage(d.config.SourceNamespace, "transformed")
		d.config.Transform = func(from runtime.Object) runtime.Object {
			pod, ok := from.(*corev1.Pod)
			Expect(ok).To(BeTrue())
			Expect(equality.Semantic.DeepDerivative(d.resource.Spec, pod.Spec)).To(BeTrue())
			return transformed
		}
	})

	When("a resource is created in the datastore", func() {
		It("should distribute the transformed resource", func() {
			test.CreateResource(d.sourceClient, d.resource)
			d.federator.VerifyDistribute(test.ToUnstructured(transformed))
		})
	})

	When("a resource is updated in the datastore", func() {
		BeforeEach(func() {
			d.addInitialResource(d.resource)
		})

		It("should distribute the transformed resource", func() {
			d.federator.VerifyDistribute(test.ToUnstructured(transformed))

			d.resource = test.NewPodWithImage(d.config.SourceNamespace, "updated")
			test.UpdateResource(d.sourceClient, test.NewPodWithImage(d.config.SourceNamespace, "updated"))
			d.federator.VerifyDistribute(test.ToUnstructured(transformed))
		})
	})

	When("a resource is deleted in the datastore", func() {
		BeforeEach(func() {
			d.addInitialResource(d.resource)
		})

		It("should delete the transformed resource", func() {
			d.federator.VerifyDistribute(test.ToUnstructured(transformed))

			Expect(d.sourceClient.Delete(d.resource.GetName(), nil)).To(Succeed())
			d.federator.VerifyDelete(test.ToUnstructured(transformed))
		})
	})

	When("the transform function returns nil", func() {
		BeforeEach(func() {
			d.config.Transform = func(from runtime.Object) runtime.Object {
				return nil
			}
		})

		When("a resource is created in the datastore", func() {
			It("should not distribute the resource", func() {
				test.CreateResource(d.sourceClient, d.resource)
				d.federator.VerifyNoDistribute()
			})
		})

		When("a resource is deleted in the datastore", func() {
			BeforeEach(func() {
				d.addInitialResource(d.resource)
			})

			It("should not delete the resource", func() {
				d.federator.VerifyNoDistribute()

				Expect(d.sourceClient.Delete(d.resource.GetName(), nil)).To(Succeed())
				d.federator.VerifyNoDelete()
			})
		})
	})
}

func testSyncErrors() {
	d := newTestDiver(test.LocalNamespace, "", syncer.LocalToRemote)

	var expectedErr error

	BeforeEach(func() {
		expectedErr = errors.New("fake error")
	})

	When("distribute initially fails", func() {
		JustBeforeEach(func() {
			d.federator.FailOnDistribute = expectedErr
		})

		It("should log the error and retry until it succeeds", func() {
			d.federator.VerifyDistribute(test.CreateResource(d.sourceClient, d.resource))
			Eventually(d.handledError, 5).Should(Receive(ContainErrorSubstring(expectedErr)))
		})
	})

	When("delete initially fails", func() {
		BeforeEach(func() {
			d.federator.FailOnDelete = expectedErr
			d.addInitialResource(d.resource)
		})

		It("should log the error and retry until it succeeds", func() {
			expected := test.GetResource(d.sourceClient, d.resource)
			d.federator.VerifyDistribute(expected)

			Expect(d.sourceClient.Delete(d.resource.GetName(), nil)).To(Succeed())
			d.federator.VerifyDelete(expected)
			Eventually(d.handledError, 5).Should(Receive(ContainErrorSubstring(expectedErr)))
		})
	})

	When("delete fails with not found", func() {
		BeforeEach(func() {
			d.federator.FailOnDelete = apierrors.NewNotFound(schema.GroupResource{}, "not found")
			d.addInitialResource(d.resource)
		})

		It("should not log the error nor retry", func() {
			expected := test.GetResource(d.sourceClient, d.resource)
			d.federator.VerifyDistribute(expected)

			Expect(d.sourceClient.Delete(d.resource.GetName(), nil)).To(Succeed())
			d.federator.VerifyDelete(expected)
			Consistently(d.handledError, 300*time.Millisecond).ShouldNot(Receive(), "Error was unexpectedly logged")
		})
	})
}

func testUpdateSuppression() {
	d := newTestDiver(test.LocalNamespace, "", syncer.LocalToRemote)

	BeforeEach(func() {
		d.addInitialResource(d.resource)
	})

	JustBeforeEach(func() {
		d.federator.VerifyDistribute(test.GetResource(d.sourceClient, d.resource))
		test.UpdateResource(d.sourceClient, d.resource)
	})

	When("the resource's Status is updated in the datastore", func() {
		BeforeEach(func() {
			d.resource.Status.Phase = corev1.PodRunning
		})

		It("should not distribute it", func() {
			d.federator.VerifyNoDistribute()
		})
	})

	When("the resource's ObjectMeta is updated in the datastore", func() {
		BeforeEach(func() {
			t := metav1.Now()
			d.resource.ObjectMeta.DeletionTimestamp = &t
		})

		It("should not distribute it", func() {
			d.federator.VerifyNoDistribute()
		})
	})

	When("the resource's Labels are updated in the datastore", func() {
		BeforeEach(func() {
			d.resource.SetLabels(map[string]string{"new-label": "value"})
		})

		It("should distribute it", func() {
			d.federator.VerifyDistribute(test.ToUnstructured(d.resource))
		})
	})

	When("the resource's Annotations are updated in the datastore", func() {
		BeforeEach(func() {
			d.resource.SetAnnotations(map[string]string{"new-annotations": "value"})
		})

		It("should distribute it", func() {
			d.federator.VerifyDistribute(test.ToUnstructured(d.resource))
		})
	})
}

type testDriver struct {
	config             syncer.ResourceSyncerConfig
	syncer             syncer.Interface
	sourceClient       dynamic.ResourceInterface
	federator          *fake.Federator
	initialResources   []runtime.Object
	stopCh             chan struct{}
	resource           *corev1.Pod
	savedErrorHandlers []func(error)
	handledError       chan error
}

func newTestDiver(sourceNamespace, localClusterID string, syncDirection syncer.SyncDirection) *testDriver {
	resourceType := &corev1.Pod{}
	d := &testDriver{
		config: syncer.ResourceSyncerConfig{
			Name:            "test",
			SourceNamespace: sourceNamespace,
			LocalClusterID:  localClusterID,
			ResourceType:    resourceType,
			Direction:       syncDirection,
			Scheme:          runtime.NewScheme(),
		},
	}

	err := corev1.AddToScheme(d.config.Scheme)
	if err != nil {
		panic(err)
	}

	BeforeEach(func() {
		d.federator = fake.New()
		d.config.Federator = d.federator
		d.initialResources = nil
		d.resource = test.NewPod(sourceNamespace)
		d.stopCh = make(chan struct{})
		d.savedErrorHandlers = utilruntime.ErrorHandlers
		d.handledError = make(chan error, 1000)
	})

	JustBeforeEach(func() {
		initObjs := test.PrepInitialClientObjs(d.config.SourceNamespace, "", d.initialResources...)

		restMapper, gvr := test.GetRESTMapperAndGroupVersionResourceFor(d.config.ResourceType)

		d.config.RestMapper = restMapper
		d.config.SourceClient = fakeClient.NewSimpleDynamicClient(d.config.Scheme, initObjs...)

		d.sourceClient = d.config.SourceClient.Resource(*gvr).Namespace(d.config.SourceNamespace)

		d.syncer, err = syncer.NewResourceSyncer(&d.config)
		Expect(err).To(Succeed())

		utilruntime.ErrorHandlers = append(utilruntime.ErrorHandlers, func(err error) {
			d.handledError <- err
		})

		Expect(d.syncer.Start(d.stopCh)).To(Succeed())
	})

	AfterEach(func() {
		close(d.stopCh)
		utilruntime.ErrorHandlers = d.savedErrorHandlers
	})

	return d
}

func (t *testDriver) addInitialResource(obj runtime.Object) {
	t.initialResources = append(t.initialResources, test.ToUnstructured(obj))
}

func (t *testDriver) verifyDistributeOnCreateTest(clusterID string) {
	It("should distribute it", func() {
		t.federator.VerifyDistribute(test.CreateResource(t.sourceClient, test.SetClusterIDLabel(t.resource, clusterID)))
	})
}

func (t *testDriver) verifyNoDistributeOnCreateTest(clusterID string) {
	It("should not distribute it", func() {
		test.CreateResource(t.sourceClient, test.SetClusterIDLabel(t.resource, clusterID))
		t.federator.VerifyNoDistribute()
	})
}

func (t *testDriver) verifyDistributeOnUpdateTest(clusterID string) {
	BeforeEach(func() {
		t.addInitialResource(test.SetClusterIDLabel(t.resource, clusterID))
	})

	It("should distribute it", func() {
		t.federator.VerifyDistribute(test.GetResource(t.sourceClient, t.resource))
		t.federator.VerifyDistribute(test.UpdateResource(t.sourceClient, test.SetClusterIDLabel(
			test.NewPodWithImage(t.config.SourceNamespace, "apache"), clusterID)))
	})
}

func (t *testDriver) verifyNoDistributeOnUpdateTest(clusterID string) {
	BeforeEach(func() {
		t.addInitialResource(test.SetClusterIDLabel(t.resource, clusterID))
	})

	It("should not distribute it", func() {
		t.federator.VerifyNoDistribute()

		test.UpdateResource(t.sourceClient, test.SetClusterIDLabel(
			test.NewPodWithImage(t.config.SourceNamespace, "apache"), clusterID))
		t.federator.VerifyNoDistribute()
	})
}

func (t *testDriver) verifyDistributeOnDeleteTest(clusterID string) {
	BeforeEach(func() {
		t.addInitialResource(test.SetClusterIDLabel(t.resource, clusterID))
	})

	It("should delete it", func() {
		expected := test.GetResource(t.sourceClient, t.resource)
		t.federator.VerifyDistribute(expected)

		Expect(t.sourceClient.Delete(t.resource.GetName(), nil)).To(Succeed())
		t.federator.VerifyDelete(expected)
	})
}

func (t *testDriver) verifyNoDistributeOnDeleteTest(clusterID string) {
	BeforeEach(func() {
		t.addInitialResource(test.SetClusterIDLabel(t.resource, clusterID))
	})

	It("should not delete it", func() {
		t.federator.VerifyNoDistribute()

		Expect(t.sourceClient.Delete(t.resource.GetName(), nil)).To(Succeed())
		t.federator.VerifyNoDelete()
	})
}
