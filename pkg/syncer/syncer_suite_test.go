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
	"flag"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
	fakereactor "github.com/submariner-io/admiral/pkg/fake"
	"github.com/submariner-io/admiral/pkg/federate/fake"
	"github.com/submariner-io/admiral/pkg/log/kzerolog"
	resourceutils "github.com/submariner-io/admiral/pkg/resource"
	"github.com/submariner-io/admiral/pkg/syncer"
	"github.com/submariner-io/admiral/pkg/syncer/test"
	corev1 "k8s.io/api/core/v1"
	metaapi "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	fakeClient "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/tools/cache"
)

type EmptyRegisterer struct{}

func (e EmptyRegisterer) Register(_ prometheus.Collector) error {
	return nil
}

func (e EmptyRegisterer) MustRegister(_ ...prometheus.Collector) {
}

func (e EmptyRegisterer) Unregister(_ prometheus.Collector) bool {
	return true
}

func init() {
	flags := flag.NewFlagSet("kzerolog", flag.ExitOnError)
	kzerolog.AddFlags(flags)
	_ = flags.Parse([]string{"-v=1"})

	kzerolog.AddFlags(nil)

	prometheus.DefaultRegisterer = &EmptyRegisterer{}
}

var _ = Describe("", func() {
	kzerolog.InitK8sLogging()
})

func TestSyncer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Syncer Suite")
}

type testDriver struct {
	config             syncer.ResourceSyncerConfig
	useSharedInformer  bool
	syncer             syncer.Interface
	sourceClient       dynamic.ResourceInterface
	federator          *fake.Federator
	initialResources   []runtime.Object
	stopCh             chan struct{}
	resource           *corev1.Pod
	savedErrorHandlers []utilruntime.ErrorHandler
	handledError       chan error
}

func newTestDriver(sourceNamespace, localClusterID string, syncDirection syncer.SyncDirection) *testDriver {
	resourceType := &corev1.Pod{}
	d := &testDriver{
		config: syncer.ResourceSyncerConfig{
			Name:            "test",
			SourceNamespace: sourceNamespace,
			LocalClusterID:  localClusterID,
			ResourceType:    resourceType,
			Direction:       syncDirection,
		},
	}

	BeforeEach(func() {
		d.federator = fake.New()
		d.config.Federator = d.federator
		d.initialResources = nil
		d.resource = test.NewPod(sourceNamespace)
		d.stopCh = make(chan struct{})
		d.savedErrorHandlers = utilruntime.ErrorHandlers
		d.handledError = make(chan error, 1000)
		d.config.Scheme = runtime.NewScheme()
		d.config.Transform = nil
		d.config.OnSuccessfulSync = nil
		d.config.ResourcesEquivalent = nil
		d.config.ResyncPeriod = 0
		d.useSharedInformer = false

		err := corev1.AddToScheme(d.config.Scheme)
		Expect(err).To(Succeed())
	})

	JustBeforeEach(func() {
		initObjs := test.PrepInitialClientObjs(d.config.SourceNamespace, "", d.initialResources...)

		restMapper, gvr := test.GetRESTMapperAndGroupVersionResourceFor(d.config.ResourceType)

		d.config.RestMapper = restMapper

		dynClient := fakeClient.NewSimpleDynamicClient(d.config.Scheme, initObjs...)
		fakereactor.AddBasicReactors(&dynClient.Fake)

		d.config.SourceClient = dynClient

		d.sourceClient = d.config.SourceClient.Resource(*gvr).Namespace(d.config.SourceNamespace)

		var err error

		if d.useSharedInformer {
			var sharedInformer cache.SharedInformer

			sharedInformer, err = syncer.NewSharedInformer(&d.config)
			Expect(err).To(Succeed())

			d.syncer, err = syncer.NewResourceSyncerWithSharedInformer(&d.config, sharedInformer)

			go func() {
				sharedInformer.Run(d.stopCh)
			}()
		} else {
			d.syncer, err = syncer.NewResourceSyncer(&d.config)
		}

		Expect(err).To(Succeed())

		if d.config.NamespaceInformer != nil {
			go func() {
				d.config.NamespaceInformer.Run(d.stopCh)
			}()
		}

		utilruntime.ErrorHandlers = append(utilruntime.ErrorHandlers,
			func(_ context.Context, err error, _ string, _ ...any) {
				d.handledError <- err
			})

		Expect(d.syncer.Start(d.stopCh)).To(Succeed())
	})

	JustAfterEach(func(ctx SpecContext) {
		if d.stopCh != nil {
			close(d.stopCh)
		}

		awaitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		Expect(d.syncer.AwaitStopped(awaitCtx)).To(Succeed())

		utilruntime.ErrorHandlers = d.savedErrorHandlers
	})

	return d
}

func (t *testDriver) addInitialResource(obj runtime.Object) {
	t.initialResources = append(t.initialResources, resourceutils.MustToUnstructured(obj))
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

	It("should delete it", func(ctx SpecContext) {
		expected := test.GetResource(t.sourceClient, t.resource)
		t.federator.VerifyDistribute(expected)

		Expect(t.sourceClient.Delete(ctx, t.resource.GetName(), metav1.DeleteOptions{})).To(Succeed())
		t.federator.VerifyDelete(expected)
	})
}

func (t *testDriver) verifyNoDistributeOnDeleteTest(clusterID string) {
	BeforeEach(func() {
		t.addInitialResource(test.SetClusterIDLabel(t.resource, clusterID))
	})

	It("should not delete it", func(ctx SpecContext) {
		t.federator.VerifyNoDistribute()

		Expect(t.sourceClient.Delete(ctx, t.resource.GetName(), metav1.DeleteOptions{})).To(Succeed())
		t.federator.VerifyNoDelete()
	})
}

func assertResourceList(actual []runtime.Object, expected ...*corev1.Pod) {
	expSpecs := map[string]*corev1.PodSpec{}
	for i := range expected {
		expSpecs[expected[i].Name] = &expected[i].Spec
	}

	Expect(actual).To(HaveLen(len(expSpecs)))

	for _, obj := range actual {
		meta, err := metaapi.Accessor(obj)
		Expect(err).To(Succeed())
		Expect(obj).To(BeAssignableToTypeOf(&corev1.Pod{}))
		Expect(&obj.(*corev1.Pod).Spec).To(Equal(expSpecs[meta.GetName()]))
		delete(expSpecs, meta.GetName())
	}
}
