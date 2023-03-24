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

package syncer

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/syncer"
	"github.com/submariner-io/admiral/pkg/syncer/broker"
	testV1 "github.com/submariner-io/admiral/test/apis/admiral.submariner.io/v1"
	"github.com/submariner-io/admiral/test/e2e/util"
	"github.com/submariner-io/shipyard/test/e2e/framework"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metaapi "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
)

var _ = Describe("[syncer] Broker bi-directional syncer tests", func() {
	Context("with a specific source namespace", testWithSpecificSourceNamespace)
	Context("with source namespace all", testWithSourceNamespaceAll)
	Context("with local-to-broker transform", testWithTransformLocalToBroker)
	Context("with a label selector", testWithLabelSelector)
	Context("with a field selector", testWithFieldSelector)
})

func testWithSpecificSourceNamespace() {
	t := newTestDriver()

	BeforeEach(func() {
		t.localSourceNamespace = t.framework.Namespace
	})

	t.bidirectionalSyncTests()
}

func testWithSourceNamespaceAll() {
	newTestDriver().bidirectionalSyncTests()
}

func testWithTransformLocalToBroker() {
	t := newTestDriver()

	BeforeEach(func() {
		t.brokerResourceType = &testV1.ExportedToaster{}
		t.transformLocalToBroker = func(from runtime.Object, numRequeues int, op syncer.Operation) (runtime.Object, bool) {
			toaster, ok := from.(*testV1.Toaster)
			Expect(ok).To(BeTrue())

			return &testV1.ExportedToaster{
				ObjectMeta: metav1.ObjectMeta{
					Name: toaster.GetName(),
				},
				Spec: toaster.Spec,
			}, false
		}
	})

	When("Toaster resources are created and deleted in one cluster", func() {
		It("should correctly sync the ExportedToaster resources to both clusters", func() {
			toaster := t.createToaster(framework.ClusterA)
			t.awaitExportedToaster(framework.ClusterA, toaster)
			t.awaitExportedToaster(framework.ClusterB, toaster)

			t.deleteToaster(framework.ClusterA, toaster)
			t.awaitNoExportedToaster(framework.ClusterA, toaster)
			t.awaitNoExportedToaster(framework.ClusterB, toaster)
		})
	})
}

func testWithLabelSelector() {
	t := newTestDriver()

	BeforeEach(func() {
		req, err := labels.NewRequirement("foo", selection.Equals, []string{"bar"})
		Expect(err).To(Succeed())
		t.labelSelector = labels.NewSelector().Add(*req).String()
	})

	When("a label selector is specified", func() {
		When("Toaster resources are created with and without the label selector criteria", func() {
			It("should correctly sync or exclude the Toaster resource", func() {
				By("Creating a Toaster with the label selector criteria")
				toaster := t.createToasterResource(framework.ClusterA, util.AddLabel(t.newToaster("sync"), "foo", "bar"))
				t.awaitToaster(framework.ClusterB, toaster)

				By("Creating a Toaster without the label selector criteria")
				toaster = t.createToasterResource(framework.ClusterA, t.newToaster("no-sync"))
				time.Sleep(1000 * time.Millisecond)
				t.awaitNoToaster(framework.ClusterB, toaster)
			})
		})
	})
}

func testWithFieldSelector() {
	t := newTestDriver()

	BeforeEach(func() {
		t.fieldSelector = fields.OneTermEqualSelector("metadata.name", "sync").String()
	})

	When("a field selector is specified", func() {
		When("Toaster resources are created with and without the field selector criteria", func() {
			It("should correctly sync or exclude the Toaster resource", func() {
				By("Creating a Toaster with the field selector criteria")
				toaster := t.createToasterResource(framework.ClusterA, t.newToaster("sync"))
				t.awaitToaster(framework.ClusterB, toaster)

				By("Creating a Toaster without the field selector criteria")
				toaster = t.createToasterResource(framework.ClusterA, t.newToaster("no-sync"))
				time.Sleep(1000 * time.Millisecond)
				t.awaitNoToaster(framework.ClusterB, toaster)
			})
		})
	})
}

type testDriver struct {
	framework              *framework.Framework
	localSourceNamespace   string
	transformLocalToBroker func(from runtime.Object, numRequeues int, op syncer.Operation) (runtime.Object, bool)
	brokerResourceType     runtime.Object
	labelSelector          string
	fieldSelector          string
	clusterClients         []dynamic.Interface
	stopCh                 chan struct{}
}

func newTestDriver() *testDriver {
	f := framework.NewFramework("broker-syncer")

	t := &testDriver{framework: f}

	BeforeEach(func() {
		t.stopCh = make(chan struct{})
		t.brokerResourceType = &testV1.Toaster{}
		t.localSourceNamespace = metav1.NamespaceAll
		t.clusterClients = nil
	})

	JustBeforeEach(func() {
		clusterASyncer := t.newSyncer(framework.ClusterA)
		clusterBSyncer := t.newSyncer(framework.ClusterB)

		c, err := dynamic.NewForConfig(framework.RestConfigs[framework.ClusterA])
		Expect(err).To(Succeed())
		t.clusterClients = append(t.clusterClients, c)

		c, err = dynamic.NewForConfig(framework.RestConfigs[framework.ClusterB])
		Expect(err).To(Succeed())
		t.clusterClients = append(t.clusterClients, c)

		Expect(clusterASyncer.Start(t.stopCh)).To(Succeed())
		Expect(clusterBSyncer.Start(t.stopCh)).To(Succeed())
	})

	JustAfterEach(func() {
		close(t.stopCh)

		brokerNS, found := os.LookupEnv("BROKER_K8S_REMOTENAMESPACE")
		Expect(found).To(BeTrue())

		t.deleteAllToasters(framework.ClusterA, f.Namespace)
		t.deleteAllExportedToasters(framework.ClusterA, f.Namespace)
		t.deleteAllToasters(framework.ClusterB, f.Namespace)
		t.deleteAllExportedToasters(framework.ClusterB, f.Namespace)
		t.deleteAllToasters(framework.ClusterB, brokerNS)
		t.deleteAllExportedToasters(framework.ClusterB, brokerNS)
	})

	return t
}

func (t *testDriver) newSyncer(cluster framework.ClusterIndex) *broker.Syncer {
	localResourceType := &testV1.Toaster{}

	localClusterID := ""
	if reflect.TypeOf(localResourceType) == reflect.TypeOf(t.brokerResourceType) {
		localClusterID = framework.TestContext.ClusterIDs[cluster]
	}

	syncerObj, err := broker.NewSyncer(broker.SyncerConfig{
		LocalRestConfig: framework.RestConfigs[cluster],
		LocalNamespace:  t.framework.Namespace,
		LocalClusterID:  localClusterID,
		ResourceConfigs: []broker.ResourceConfig{
			{
				LocalResourceType:        localResourceType,
				LocalSourceNamespace:     t.localSourceNamespace,
				LocalSourceLabelSelector: t.labelSelector,
				LocalSourceFieldSelector: t.fieldSelector,
				TransformLocalToBroker:   t.transformLocalToBroker,
				BrokerResourceType:       t.brokerResourceType,
			},
		},
	})

	Expect(err).To(Succeed())

	return syncerObj
}

func (t *testDriver) bidirectionalSyncTests() {
	When("Toaster resources are created and deleted in one cluster", func() {
		It("should correctly sync the Toaster resources to the other cluster", func() {
			toaster := t.createToaster(framework.ClusterA)
			t.awaitToaster(framework.ClusterB, toaster)

			t.deleteToaster(framework.ClusterA, toaster)
			t.awaitNoToaster(framework.ClusterB, toaster)

			toaster = t.createToaster(framework.ClusterB)
			t.awaitToaster(framework.ClusterA, toaster)

			t.deleteToaster(framework.ClusterB, toaster)
			t.awaitNoToaster(framework.ClusterA, toaster)
		})
	})
}

func (t *testDriver) newToaster(name string) *testV1.Toaster {
	return util.NewToaster(name, t.framework.Namespace)
}

func (t *testDriver) createToaster(cluster framework.ClusterIndex) *testV1.Toaster {
	return t.createToasterResource(cluster, t.newToaster(framework.TestContext.ClusterIDs[cluster]+"-toaster"))
}

func (t *testDriver) createToasterResource(cluster framework.ClusterIndex, toaster *testV1.Toaster) *testV1.Toaster {
	return util.CreateToaster(t.clusterClients[cluster], toaster, framework.TestContext.ClusterIDs[cluster])
}

func (t *testDriver) deleteToaster(cluster framework.ClusterIndex, toDelete *testV1.Toaster) {
	util.DeleteToaster(t.clusterClients[cluster], toDelete, framework.TestContext.ClusterIDs[cluster])
}

func (t *testDriver) awaitToaster(cluster framework.ClusterIndex, expected *testV1.Toaster) {
	By(fmt.Sprintf("Waiting for Toaster %q to be synced to %q", expected.Name, framework.TestContext.ClusterIDs[cluster]))
	actual, ok := t.awaitResource(cluster, util.ToasterGVR(), expected).(*testV1.Toaster)
	Expect(ok).To(BeTrue())
	Expect(actual.Spec).To(Equal(expected.Spec))
}

func (t *testDriver) awaitNoToaster(cluster framework.ClusterIndex, lookup *testV1.Toaster) {
	By(fmt.Sprintf("Waiting for Toaster %q to be removed from %q", lookup.Name, framework.TestContext.ClusterIDs[cluster]))
	t.awaitNoResource(cluster, util.ToasterGVR(), lookup)
}

func (t *testDriver) awaitNoExportedToaster(cluster framework.ClusterIndex, lookup *testV1.Toaster) {
	By(fmt.Sprintf("Waiting for ExportedToaster %q to be removed from %q", lookup.Name, framework.TestContext.ClusterIDs[cluster]))
	t.awaitNoResource(cluster, exportedToasterGVR(), lookup)
}

func (t *testDriver) awaitExportedToaster(cluster framework.ClusterIndex, expected *testV1.Toaster) {
	By(fmt.Sprintf("Waiting for ExportedToaster %q to be synced to %q", expected.Name, framework.TestContext.ClusterIDs[cluster]))
	actual, ok := t.awaitResource(cluster, exportedToasterGVR(), &testV1.ExportedToaster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      expected.Name,
			Namespace: expected.Namespace,
		},
	}).(*testV1.ExportedToaster)
	Expect(ok).To(BeTrue())

	Expect(actual.Spec).To(Equal(expected.Spec))
}

func (t *testDriver) awaitResource(cluster framework.ClusterIndex, gvr *schema.GroupVersionResource,
	resource runtime.Object,
) runtime.Object {
	clusterName := framework.TestContext.ClusterIDs[cluster]

	meta, err := metaapi.Accessor(resource)
	Expect(err).To(Succeed())

	msg := fmt.Sprintf("get %s %q in namespace %q from %q", gvr.Resource, meta.GetName(), meta.GetNamespace(), clusterName)
	raw, ok := framework.AwaitUntil(msg, func() (i interface{}, e error) {
		obj, err := t.clusterClients[cluster].Resource(*gvr).Namespace(meta.GetNamespace()).Get(
			context.TODO(), meta.GetName(), metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil //nolint:nilnil // Returning nil value is intentional
		}
		return obj, err
	}, func(result interface{}) (bool, string, error) {
		if result == nil {
			return false, fmt.Sprintf("%s %q not found", gvr.Resource, meta.GetName()), nil
		}

		return true, "", nil
	}).(*unstructured.Unstructured)
	Expect(ok).To(BeTrue())

	result := resource.DeepCopyObject()
	err = scheme.Scheme.Convert(raw, result, nil)
	Expect(err).To(Succeed())

	return result
}

func (t *testDriver) awaitNoResource(cluster framework.ClusterIndex, gvr *schema.GroupVersionResource, resource runtime.Object) {
	clusterName := framework.TestContext.ClusterIDs[cluster]

	meta, err := metaapi.Accessor(resource)
	Expect(err).To(Succeed())

	msg := fmt.Sprintf("get %s %q in namespace %q from %q", gvr.Resource, meta.GetName(), meta.GetNamespace(), clusterName)
	framework.AwaitUntil(msg, func() (i interface{}, e error) {
		obj, err := t.clusterClients[cluster].Resource(*gvr).Namespace(meta.GetNamespace()).Get(
			context.TODO(), meta.GetName(), metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil //nolint:nilnil // Returning nil value is intentional
		}
		return obj, err
	}, func(result interface{}) (bool, string, error) {
		if result != nil {
			return false, fmt.Sprintf("%#v still exists", result), nil
		}

		return true, "", nil
	})
}

func (t *testDriver) deleteAllToasters(cluster framework.ClusterIndex, namespace string) {
	util.DeleteAllToasters(t.clusterClients[cluster], namespace, framework.TestContext.ClusterIDs[cluster])
}

func (t *testDriver) deleteAllExportedToasters(cluster framework.ClusterIndex, namespace string) {
	util.DeleteAllOf(t.clusterClients[cluster], exportedToasterGVR(), namespace, framework.TestContext.ClusterIDs[cluster])
}

func exportedToasterGVR() *schema.GroupVersionResource {
	return &schema.GroupVersionResource{
		Group:    testV1.SchemeGroupVersion.Group,
		Version:  testV1.SchemeGroupVersion.Version,
		Resource: "exportedtoasters",
	}
}
