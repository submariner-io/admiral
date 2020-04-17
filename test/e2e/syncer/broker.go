package syncer

import (
	"fmt"
	"os"
	"reflect"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/syncer/broker"
	"github.com/submariner-io/admiral/pkg/syncer/test"
	testV1 "github.com/submariner-io/admiral/test/apis/admiral.submariner.io/v1"
	"github.com/submariner-io/shipyard/test/e2e/framework"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
)

var _ = Describe("[syncer] Broker bi-directional syncer tests", func() {
	Context("with a specific source namespace", testWithSpecificSourceNamespace)
	Context("with source namespace all", testWithSourceNamespaceAll)
	Context("with local transform", testWithLocalTransform)
})

func testWithSpecificSourceNamespace() {
	t := newTestDiver()

	BeforeEach(func() {
		t.localSourceNamespace = t.framework.Namespace
	})

	t.bidirectionalSyncTests()
}

func testWithSourceNamespaceAll() {
	newTestDiver().bidirectionalSyncTests()
}

func testWithLocalTransform() {
	t := newTestDiver()

	BeforeEach(func() {
		t.brokerResourceType = &testV1.ExportedToaster{}
		t.localTransform = func(from runtime.Object) runtime.Object {
			toaster := from.(*testV1.Toaster)
			return &testV1.ExportedToaster{
				ObjectMeta: metav1.ObjectMeta{
					Name: toaster.GetName(),
				},
				Spec: toaster.Spec,
			}
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

type testDriver struct {
	framework            *framework.Framework
	localSourceNamespace string
	localTransform       func(from runtime.Object) runtime.Object
	brokerResourceType   runtime.Object
	clusterClients       []dynamic.Interface
	stopCh               chan struct{}
}

func newTestDiver() *testDriver {
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

	syncer, err := broker.NewSyncer(broker.SyncerConfig{
		LocalRestConfig: framework.RestConfigs[cluster],
		LocalNamespace:  t.framework.Namespace,
		LocalClusterID:  localClusterID,
		ResourceConfigs: []broker.ResourceConfig{
			{
				LocalResourceType:    localResourceType,
				LocalSourceNamespace: t.localSourceNamespace,
				LocalTransform:       t.localTransform,
				BrokerResourceType:   t.brokerResourceType,
			},
		},
	})

	Expect(err).To(Succeed())
	return syncer
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

func (t *testDriver) createToaster(cluster framework.ClusterIndex) *testV1.Toaster {
	namespace := t.framework.Namespace
	toaster := &testV1.Toaster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      framework.TestContext.ClusterIDs[cluster] + "-toaster",
			Namespace: namespace,
		},
		Spec: testV1.ToasterSpec{
			Manufacturer: "Cuisinart",
			ModelNumber:  "1234T",
		},
	}

	By(fmt.Sprintf("Creating Toaster %q in namespace %q in %q", toaster.Name, namespace, framework.TestContext.ClusterIDs[cluster]))

	_, err := t.clusterClients[cluster].Resource(*toasterGVR()).Namespace(namespace).Create(test.ToUnstructured(toaster), metav1.CreateOptions{})
	Expect(err).To(Succeed())

	return toaster
}

func (t *testDriver) deleteToaster(cluster framework.ClusterIndex, toDelete *testV1.Toaster) {
	By(fmt.Sprintf("Deleting Toaster %q in namespace %q from %q", toDelete.Name, toDelete.Namespace, framework.TestContext.ClusterIDs[cluster]))
	t.deleteResource(cluster, toasterGVR(), toDelete)
}

func (t *testDriver) deleteResource(cluster framework.ClusterIndex, gvr *schema.GroupVersionResource, toDelete runtime.Object) {
	clusterName := framework.TestContext.ClusterIDs[cluster]

	meta, err := meta.Accessor(toDelete)
	Expect(err).To(Succeed())

	msg := fmt.Sprintf("delete %s %q in namespace %q from %q", gvr.Resource, meta.GetName(), meta.GetNamespace(), clusterName)
	framework.AwaitUntil(msg, func() (i interface{}, e error) {
		return nil, t.clusterClients[cluster].Resource(*gvr).Namespace(meta.GetNamespace()).Delete(meta.GetName(), nil)
	}, framework.NoopCheckResult)
}

func (t *testDriver) awaitToaster(cluster framework.ClusterIndex, expected *testV1.Toaster) {
	By(fmt.Sprintf("Waiting for Toaster %q to be synced to %q", expected.Name, framework.TestContext.ClusterIDs[cluster]))
	actual := t.awaitResource(cluster, toasterGVR(), expected).(*testV1.Toaster)
	Expect(actual.Spec).To(Equal(expected.Spec))
}

func (t *testDriver) awaitNoToaster(cluster framework.ClusterIndex, lookup *testV1.Toaster) {
	By(fmt.Sprintf("Waiting for Toaster %q to be removed from %q", lookup.Name, framework.TestContext.ClusterIDs[cluster]))
	t.awaitNoResource(cluster, toasterGVR(), lookup)
}

func (t *testDriver) awaitNoExportedToaster(cluster framework.ClusterIndex, lookup *testV1.Toaster) {
	By(fmt.Sprintf("Waiting for ExportedToaster %q to be removed from %q", lookup.Name, framework.TestContext.ClusterIDs[cluster]))
	t.awaitNoResource(cluster, exportedToasterGVR(), lookup)
}

func (t *testDriver) awaitExportedToaster(cluster framework.ClusterIndex, expected *testV1.Toaster) {
	By(fmt.Sprintf("Waiting for ExportedToaster %q to be synced to %q", expected.Name, framework.TestContext.ClusterIDs[cluster]))
	actual := t.awaitResource(cluster, exportedToasterGVR(), &testV1.ExportedToaster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      expected.Name,
			Namespace: expected.Namespace,
		},
	}).(*testV1.ExportedToaster)

	Expect(actual.Spec).To(Equal(expected.Spec))
}

func (t *testDriver) awaitResource(cluster framework.ClusterIndex, gvr *schema.GroupVersionResource, resource runtime.Object) runtime.Object {
	clusterName := framework.TestContext.ClusterIDs[cluster]

	meta, err := meta.Accessor(resource)
	Expect(err).To(Succeed())

	msg := fmt.Sprintf("get %s %q in namespace %q from %q", gvr.Resource, meta.GetName(), meta.GetNamespace(), clusterName)
	raw := framework.AwaitUntil(msg, func() (i interface{}, e error) {
		obj, err := t.clusterClients[cluster].Resource(*gvr).Namespace(meta.GetNamespace()).Get(meta.GetName(), metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return obj, err
	}, func(result interface{}) (bool, string, error) {
		if result == nil {
			return false, fmt.Sprintf("%s %q not found", gvr.Resource, meta.GetName()), nil
		}

		return true, "", nil
	}).(*unstructured.Unstructured)

	result := resource.DeepCopyObject()
	err = scheme.Scheme.Convert(raw, result, nil)
	Expect(err).To(Succeed())

	return result
}

func (t *testDriver) awaitNoResource(cluster framework.ClusterIndex, gvr *schema.GroupVersionResource, resource runtime.Object) {
	clusterName := framework.TestContext.ClusterIDs[cluster]

	meta, err := meta.Accessor(resource)
	Expect(err).To(Succeed())

	msg := fmt.Sprintf("get %s %q in namespace %q from %q", gvr.Resource, meta.GetName(), meta.GetNamespace(), clusterName)
	framework.AwaitUntil(msg, func() (i interface{}, e error) {
		obj, err := t.clusterClients[cluster].Resource(*gvr).Namespace(meta.GetNamespace()).Get(meta.GetName(), metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
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
	t.deleteAllOf(cluster, toasterGVR(), namespace)
}

func (t *testDriver) deleteAllExportedToasters(cluster framework.ClusterIndex, namespace string) {
	t.deleteAllOf(cluster, exportedToasterGVR(), namespace)
}

func (t *testDriver) deleteAllOf(cluster framework.ClusterIndex, gvr *schema.GroupVersionResource, namespace string) {
	clusterName := framework.TestContext.ClusterIDs[cluster]
	By(fmt.Sprintf("Deleting all %s in namespace %q from %q", gvr.Resource, namespace, clusterName))

	resource := t.clusterClients[cluster].Resource(*gvr).Namespace(namespace)
	Expect(resource.DeleteCollection(nil, metav1.ListOptions{})).To(Succeed())

	framework.AwaitUntil(fmt.Sprintf("list %s in namespace %q from %q", gvr.Resource, namespace, clusterName), func() (i interface{}, e error) {
		return resource.List(metav1.ListOptions{})
	}, func(result interface{}) (bool, string, error) {
		list := result.(*unstructured.UnstructuredList)
		if len(list.Items) != 0 {
			return false, fmt.Sprintf("%d Toasters still remain", len(list.Items)), nil
		}

		return true, "", nil
	})
}

func toasterGVR() *schema.GroupVersionResource {
	return &schema.GroupVersionResource{
		Group:    testV1.SchemeGroupVersion.Group,
		Version:  testV1.SchemeGroupVersion.Version,
		Resource: "toasters",
	}
}

func exportedToasterGVR() *schema.GroupVersionResource {
	return &schema.GroupVersionResource{
		Group:    testV1.SchemeGroupVersion.Group,
		Version:  testV1.SchemeGroupVersion.Version,
		Resource: "exportedtoasters",
	}
}
