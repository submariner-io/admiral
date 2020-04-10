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
	Describe("Remote -> Local ", testRemoteToLocal)
	Describe("With Transform Function", testTransformFunction)
	Describe("Sync Errors", testSyncErrors)
	Describe("Update Suppression", testUpdateSuppression)
})

func testLocalToRemote() {
	var (
		d *testDriver
	)

	BeforeEach(func() {
		d = newTestDiver(test.LocalNamespace, "", syncer.LocalToRemote)
	})

	JustBeforeEach(func() {
		d.run()
	})

	AfterEach(func() {
		d.stop()
	})

	When("a resource without a cluster ID label is created in the local datastore", func() {
		It("should distribute it remotely", func() {
			d.federator.VerifyDistribute(test.CreateResource(d.sourceClient, d.resource))
		})
	})

	When("a resource without a cluster ID label is updated in the local datastore", func() {
		BeforeEach(func() {
			d.addInitialResource(d.resource)
		})

		It("should distribute it remotely", func() {
			d.federator.VerifyDistribute(test.GetResource(d.sourceClient, d.resource))
			d.federator.VerifyDistribute(test.UpdateResource(d.sourceClient, test.NewPodWithImage(d.config.SourceNamespace, "apache")))
		})
	})

	When("a resource without a cluster ID label is deleted from the local datastore", func() {
		BeforeEach(func() {
			d.addInitialResource(d.resource)
		})

		It("should delete it remotely", func() {
			expected := test.GetResource(d.sourceClient, d.resource)
			d.federator.VerifyDistribute(expected)

			Expect(d.sourceClient.Delete(d.resource.GetName(), nil)).To(Succeed())
			d.federator.VerifyDelete(expected)
		})
	})

	When("a resource with a cluster ID label is created in the local datastore", func() {
		It("should not distribute it remotely", func() {
			test.CreateResource(d.sourceClient, test.SetClusterIDLabel(d.resource, "remote"))
			d.federator.VerifyNoDistribute()
		})
	})

	When("a resource with a cluster ID label is updated in the local datastore", func() {
		BeforeEach(func() {
			d.addInitialResource(test.SetClusterIDLabel(d.resource, "remote"))
		})

		It("should not distribute it remotely", func() {
			d.federator.VerifyNoDistribute()

			test.UpdateResource(d.sourceClient, test.SetClusterIDLabel(
				test.NewPodWithImage(d.config.SourceNamespace, "apache"), "remote"))
			d.federator.VerifyNoDistribute()
		})
	})

	When("a resource with a local cluster ID label is deleted from the local datastore", func() {
		BeforeEach(func() {
			d.addInitialResource(test.SetClusterIDLabel(d.resource, "remote"))
		})

		It("should not delete it remotely", func() {
			d.federator.VerifyNoDistribute()

			Expect(d.sourceClient.Delete(d.resource.GetName(), nil)).To(Succeed())
			d.federator.VerifyNoDelete()
		})
	})
}

func testRemoteToLocal() {
	var (
		d *testDriver
	)

	BeforeEach(func() {
		d = newTestDiver(test.RemoteNamespace, "local", syncer.RemoteToLocal)
	})

	JustBeforeEach(func() {
		d.run()
	})

	AfterEach(func() {
		d.stop()
	})

	When("a resource with a non-local cluster ID label is created in the remote datastore", func() {
		It("should distribute it locally", func() {
			d.federator.VerifyDistribute(test.CreateResource(d.sourceClient, test.SetClusterIDLabel(d.resource, "remote")))
		})
	})

	When("a resource with a non-local cluster ID label is updated in the remote datastore", func() {
		BeforeEach(func() {
			d.addInitialResource(test.SetClusterIDLabel(d.resource, "remote"))
		})

		It("should distribute it locally", func() {
			d.federator.VerifyDistribute(test.GetResource(d.sourceClient, d.resource))
			d.federator.VerifyDistribute(test.UpdateResource(d.sourceClient,
				test.SetClusterIDLabel(test.NewPodWithImage(d.config.SourceNamespace, "apache"), "remote")))
		})
	})

	When("a resource with a non-local cluster ID label is deleted from the remote datastore", func() {
		BeforeEach(func() {
			d.addInitialResource(test.SetClusterIDLabel(d.resource, "remote"))
		})

		It("should delete it locally", func() {
			expected := test.GetResource(d.sourceClient, d.resource)
			d.federator.VerifyDistribute(expected)

			Expect(d.sourceClient.Delete(d.resource.GetName(), nil)).To(Succeed())
			d.federator.VerifyDelete(expected)
		})
	})

	When("a resource with a local cluster ID label is created in the remote datastore", func() {
		It("should not distribute it locally", func() {
			test.CreateResource(d.sourceClient, test.SetClusterIDLabel(d.resource, d.config.LocalClusterID))
			d.federator.VerifyNoDistribute()
		})
	})

	When("a resource with a local cluster ID label is updated in the remote datastore", func() {
		BeforeEach(func() {
			d.addInitialResource(test.SetClusterIDLabel(d.resource, d.config.LocalClusterID))
		})

		It("should not distribute it locally", func() {
			d.federator.VerifyNoDistribute()

			test.UpdateResource(d.sourceClient, test.NewPodWithImage(d.config.SourceNamespace, "apache"))
			d.federator.VerifyNoDistribute()
		})
	})

	When("a resource with a local cluster ID label is deleted from the remote datastore", func() {
		BeforeEach(func() {
			d.addInitialResource(test.SetClusterIDLabel(d.resource, d.config.LocalClusterID))
		})

		It("should not delete it locally", func() {
			d.federator.VerifyNoDistribute()

			Expect(d.sourceClient.Delete(d.resource.GetName(), nil)).To(Succeed())
			d.federator.VerifyNoDelete()
		})
	})

	When("a resource without a cluster ID label is created in the remote datastore", func() {
		It("should not distribute it locally", func() {
			test.CreateResource(d.sourceClient, d.resource)
			d.federator.VerifyNoDistribute()
		})
	})

	When("a resource without a cluster ID label is updated in the remote datastore", func() {
		BeforeEach(func() {
			d.addInitialResource(d.resource)
		})

		It("should not distribute it locally", func() {
			d.federator.VerifyNoDistribute()

			test.UpdateResource(d.sourceClient, test.NewPodWithImage(d.config.SourceNamespace, "apache"))
			d.federator.VerifyNoDistribute()
		})
	})

	When("a resource without a local cluster ID label is deleted from the remote datastore", func() {
		BeforeEach(func() {
			d.addInitialResource(d.resource)
		})

		It("should not delete it locally", func() {
			d.federator.VerifyNoDistribute()

			Expect(d.sourceClient.Delete(d.resource.GetName(), nil)).To(Succeed())
			d.federator.VerifyNoDelete()
		})
	})
}

func testTransformFunction() {
	var (
		d           *testDriver
		transformed *corev1.Pod
	)

	BeforeEach(func() {
		d = newTestDiver(test.LocalNamespace, "", syncer.LocalToRemote)
		transformed = test.NewPodWithImage(d.config.SourceNamespace, "transformed")
		d.config.Transform = func(from runtime.Object) runtime.Object {
			pod, ok := from.(*corev1.Pod)
			Expect(ok).To(BeTrue())
			Expect(equality.Semantic.DeepDerivative(d.resource.Spec, pod.Spec)).To(BeTrue())
			return transformed
		}
	})

	JustBeforeEach(func() {
		d.run()
	})

	AfterEach(func() {
		d.stop()
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
	var (
		d           *testDriver
		expectedErr error
	)

	BeforeEach(func() {
		d = newTestDiver(test.LocalNamespace, "", syncer.LocalToRemote)
		expectedErr = errors.New("fake error")
	})

	JustBeforeEach(func() {
		d.run()
	})

	AfterEach(func() {
		d.stop()
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
	var (
		d *testDriver
	)

	BeforeEach(func() {
		d = newTestDiver(test.LocalNamespace, "", syncer.LocalToRemote)
		d.addInitialResource(d.resource)
	})

	JustBeforeEach(func() {
		d.run()

		d.federator.VerifyDistribute(test.GetResource(d.sourceClient, d.resource))
		test.UpdateResource(d.sourceClient, d.resource)
	})

	AfterEach(func() {
		d.stop()
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
	federator := fake.New()
	resourceType := &corev1.Pod{}
	return &testDriver{
		config: syncer.ResourceSyncerConfig{
			Name:            "test",
			SourceNamespace: sourceNamespace,
			LocalClusterID:  localClusterID,
			Federator:       federator,
			ResourceType:    resourceType,
			Direction:       syncDirection,
			Scheme:          runtime.NewScheme(),
		},
		federator:          federator,
		stopCh:             make(chan struct{}),
		resource:           test.NewPod(sourceNamespace),
		savedErrorHandlers: utilruntime.ErrorHandlers,
		handledError:       make(chan error, 1000),
	}
}

func (t *testDriver) run() {
	err := corev1.AddToScheme(t.config.Scheme)
	Expect(err).To(Succeed())

	initObjs := test.PrepInitialClientObjs(t.config.SourceNamespace, "", t.initialResources...)

	restMapper, gvr := test.GetRESTMapperAndGroupVersionResourceFor(t.config.ResourceType)

	t.config.RestMapper = restMapper
	t.config.SourceClient = fakeClient.NewSimpleDynamicClient(t.config.Scheme, initObjs...)

	t.sourceClient = t.config.SourceClient.Resource(*gvr).Namespace(t.config.SourceNamespace)

	t.syncer, err = syncer.NewResourceSyncer(&t.config)
	Expect(err).To(Succeed())

	utilruntime.ErrorHandlers = append(utilruntime.ErrorHandlers, func(err error) {
		t.handledError <- err
	})

	Expect(t.syncer.Start(t.stopCh)).To(Succeed())
}

func (t *testDriver) stop() {
	close(t.stopCh)
	utilruntime.ErrorHandlers = t.savedErrorHandlers
}

func (t *testDriver) addInitialResource(obj runtime.Object) {
	t.initialResources = append(t.initialResources, test.ToUnstructured(obj))
}
