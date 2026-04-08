/*
SPDX-License-Identifier: Apache-2.0

Copyright Contributors to the Submariner project.

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

package syncer_test

import (
	"context"
	"errors"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
	. "github.com/submariner-io/admiral/pkg/gomega"
	resourceutils "github.com/submariner-io/admiral/pkg/resource"
	"github.com/submariner-io/admiral/pkg/syncer"
	"github.com/submariner-io/admiral/pkg/syncer/test"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var _ = Describe("Resource Syncer", func() {
	Describe("Local -> Remote", testLocalToRemote)

	Describe("Remote -> Local", func() {
		Context("with a local cluster ID", testRemoteToLocalWithLocalClusterID)
		Context("without a local cluster ID", testRemoteToLocalWithoutLocalClusterID)
	})

	Describe("With Transform Function", testTransformFunction)
	Describe("With OnSuccessfulSync Function", testOnSuccessfulSyncFunction)
	Describe("With ShouldProcess Function", testShouldProcessFunction)
	Describe("Sync Errors", testSyncErrors)
	Describe("Update Suppression", testUpdateSuppression)
	Describe("GetResource", testGetResource)
	Describe("ListResources", testListResources)
	Describe("ListResourcesBySelector", testListResourcesBySelector)
	Describe("RequeueResource", testRequeueResource)
	Describe("Reconcile", func() {
		Context(fmt.Sprintf("Direction: %s", syncer.LocalToRemote), testReconcileLocalToRemote)
		Context(fmt.Sprintf("Direction: %s", syncer.RemoteToLocal), testReconcileRemoteToLocal)
		Context(fmt.Sprintf("Direction: %s", syncer.None), testReconcileNoDirection)
	})
	Describe("Trim Resource Fields", testTrimResourceFields)
	Describe("With SharedInformer", testWithSharedInformer)
	Describe("With missing namespace", testWithMissingNamespace)
	Describe("Event Ordering", testEventOrdering)
	Describe("Priority Ordering", testPriorityOrdering)
	Describe("AwaitStopped", testAwaitStopped)
})

func testLocalToRemote() {
	d := newTestDriver(test.LocalNamespace, "", syncer.LocalToRemote)

	BeforeEach(func() {
		d.config.Metrics = syncer.MetricsConfig{
			SyncCounterOpts: &prometheus.GaugeOpts{
				Name: "sync-counter",
			},
			FederationDurationMsOpts: &prometheus.HistogramOpts{
				Name: "federation",
			},
			QueueWaitDurationMsOpts: &prometheus.HistogramOpts{
				Name: "queue-wait",
			},
			QueueLengthOpts: &prometheus.HistogramOpts{
				Name: "queue-length",
			},
		}
	})

	When("a resource without a cluster ID label is created in the local datastore", func() {
		BeforeEach(func() {
			d.config.MaxLogVerbosity = 2
		})

		d.verifyDistributeOnCreateTest("")
	})

	When("a resource without a cluster ID label is updated in the local datastore", func() {
		BeforeEach(func() {
			d.config.MaxLogVerbosity = 2
		})

		d.verifyDistributeOnUpdateTest("")
	})

	When("a resource without a cluster ID label is deleted from the local datastore", func() {
		BeforeEach(func() {
			d.config.MaxLogVerbosity = 2
		})

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
	d := newTestDriver(test.RemoteNamespace, "local", syncer.RemoteToLocal)

	BeforeEach(func() {
		d.config.Metrics.SyncCounter = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{},
			[]string{
				syncer.DirectionLabel,
				syncer.OperationLabel,
				syncer.SyncerNameLabel,
			},
		)
	})

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
	d := newTestDriver(test.RemoteNamespace, "", syncer.RemoteToLocal)

	When("a resource with a cluster ID label is created in the remote datastore", func() {
		d.verifyDistributeOnCreateTest("remote")
	})

	When("a resource with a cluster ID label is updated in the remote datastore", func() {
		d.verifyDistributeOnUpdateTest("remote")
	})

	When("a resource with a cluster ID label is deleted from the remote datastore", func() {
		d.verifyDistributeOnDeleteTest("remote")
	})

	When("a resource without a cluster ID label is created in the remote datastore", func() {
		d.verifyDistributeOnCreateTest("")
	})

	When("a resource without a cluster ID label is updated in the local datastore", func() {
		d.verifyDistributeOnUpdateTest("")
	})

	When("a resource without a cluster ID label is deleted from the local datastore", func() {
		d.verifyDistributeOnDeleteTest("")
	})
}

func testSyncErrors() {
	d := newTestDriver(test.LocalNamespace, "", syncer.LocalToRemote)

	var expectedErr error

	BeforeEach(func() {
		expectedErr = errors.New("fake error")
	})

	When("distribute initially fails", func() {
		BeforeEach(func() {
			d.federator.FailOnDistribute(expectedErr)
		})

		It("should log the error and retry until it succeeds", func(ctx context.Context) {
			d.federator.VerifyDistribute(test.CreateResource(ctx, d.sourceClient, d.resource))
			Eventually(d.handledError).WithTimeout(time.Second * 5).Should(Receive(ContainErrorSubstring(expectedErr)))
		})
	})

	When("delete initially fails", func() {
		BeforeEach(func() {
			d.federator.FailOnDelete(expectedErr)
			d.addInitialResource(d.resource)
		})

		It("should log the error and retry until it succeeds", func(ctx SpecContext) {
			expected := test.GetResource(ctx, d.sourceClient, d.resource)
			d.federator.VerifyDistribute(expected)

			Expect(d.sourceClient.Delete(ctx, d.resource.GetName(), metav1.DeleteOptions{})).To(Succeed())
			Eventually(d.handledError).WithTimeout(time.Second * 5).Should(Receive(ContainErrorSubstring(expectedErr)))
			d.federator.VerifyDelete(expected)
		})
	})

	When("delete fails with not found", func() {
		BeforeEach(func() {
			d.federator.FailOnDelete(apierrors.NewNotFound(schema.GroupResource{}, "not found"))
			d.addInitialResource(d.resource)
		})

		It("should not log the error nor retry", func(ctx SpecContext) {
			expected := test.GetResource(ctx, d.sourceClient, d.resource)
			d.federator.VerifyDistribute(expected)

			Expect(d.sourceClient.Delete(ctx, d.resource.GetName(), metav1.DeleteOptions{})).To(Succeed())
			Consistently(d.handledError).WithTimeout(300*time.Millisecond).ShouldNot(Receive(), "Error was unexpectedly logged")
		})
	})
}

func testRequeueResource() {
	d := newTestDriver(test.LocalNamespace, "", syncer.LocalToRemote)

	var transformed *corev1.Pod

	BeforeEach(func() {
		transformed = test.NewPodWithImage(d.config.SourceNamespace, "transformed")

		d.config.Transform = func(_ runtime.Object, _ int, _ syncer.Operation) (runtime.Object, bool) {
			return transformed, false
		}
	})

	When("the requested resource exists", func() {
		JustBeforeEach(func(ctx context.Context) {
			test.CreateResource(ctx, d.sourceClient, d.resource)
		})

		It("should requeue it", func() {
			d.federator.VerifyDistribute(transformed)

			d.syncer.RequeueResource(d.resource.Name, d.resource.Namespace)

			d.federator.VerifyDistribute(transformed)
		})
	})

	When("the requested resource does not exist", func() {
		It("should not requeue it", func() {
			d.syncer.RequeueResource(d.resource.Name, d.resource.Namespace)
			d.federator.VerifyNoDistribute()
		})
	})
}

func testTrimResourceFields() {
	d := newTestDriver(test.LocalNamespace, "", syncer.LocalToRemote)

	BeforeEach(func() {
		d.config.ResyncPeriod = time.Millisecond * 100

		d.resource.SetManagedFields([]metav1.ManagedFieldsEntry{
			{
				Manager:    "kubectl",
				Operation:  metav1.ManagedFieldsOperationApply,
				APIVersion: "v1",
				Time:       new(metav1.Now()),
				FieldsType: "FieldsV1",
				FieldsV1:   new(metav1.FieldsV1{}),
			},
		})

		d.addInitialResource(d.resource)
	})

	It("should remove ManagedFields from created resources", func() {
		obj, exists, err := d.syncer.GetResource(d.resource.Name, d.resource.Namespace)
		Expect(err).To(Succeed())
		Expect(exists).To(BeTrue())
		Expect(resourceutils.MustToMeta(obj).GetManagedFields()).Should(BeNil())

		// Sleep a little so a re-sync occurs and doesn't cause a data race.
		time.Sleep(200 * time.Millisecond)
	})
}

func testWithSharedInformer() {
	d := newTestDriver(test.LocalNamespace, "", syncer.LocalToRemote)

	BeforeEach(func() {
		d.useSharedInformer = true
	})

	When("a resource is created in the local datastore", func() {
		d.verifyDistributeOnCreateTest("")
	})
}
