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
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/watcher"
	testV1 "github.com/submariner-io/admiral/test/apis/admiral.submariner.io/v1"
	"github.com/submariner-io/admiral/test/e2e/util"
	"github.com/submariner-io/shipyard/test/e2e/framework"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
)

var _ = Describe("[watcher] Resource watcher tests", func() {
	t := newTestDriver()

	When(" a Toaster resource is created and deleted", func() {
		It("should notify the handler of each event", func() {
			clusterName := framework.TestContext.ClusterIDs[framework.ClusterA]
			toaster := util.CreateToaster(t.client, util.NewToaster("test-toaster", t.framework.Namespace), clusterName)
			Eventually(t.created).Should(Receive(Equal(toaster)))

			util.DeleteToaster(t.client, toaster, clusterName)
			Eventually(t.deleted).Should(Receive(Equal(toaster.Name)))
		})
	})
})

type testDriver struct {
	framework *framework.Framework
	created   chan *testV1.Toaster
	deleted   chan string
	client    dynamic.Interface
	stopCh    chan struct{}
}

func newTestDriver() *testDriver {
	f := framework.NewFramework("watcher")

	t := &testDriver{framework: f}

	BeforeEach(func() {
		t.stopCh = make(chan struct{})
		t.created = make(chan *testV1.Toaster, 100)
		t.deleted = make(chan string, 100)
	})

	JustBeforeEach(func() {
		toasterWatcher := t.newWatcher(framework.ClusterA)

		var err error

		t.client, err = dynamic.NewForConfig(framework.RestConfigs[framework.ClusterA])
		Expect(err).To(Succeed())

		Expect(toasterWatcher.Start(t.stopCh)).To(Succeed())
	})

	JustAfterEach(func() {
		close(t.stopCh)

		util.DeleteAllToasters(t.client, t.framework.Namespace, framework.TestContext.ClusterIDs[framework.ClusterA])
	})

	return t
}

func (t *testDriver) newWatcher(cluster framework.ClusterIndex) watcher.Interface {
	watcherObj, err := watcher.New(&watcher.Config{
		RestConfig: framework.RestConfigs[cluster],
		ResourceConfigs: []watcher.ResourceConfig{
			{
				Name:         "Toaster watcher",
				ResourceType: &testV1.Toaster{},
				Handler: watcher.EventHandlerFuncs{
					OnCreateFunc: func(obj runtime.Object, numRequeues int) bool {
						t.created <- obj.(*testV1.Toaster)
						return false
					},
					OnDeleteFunc: func(obj runtime.Object, numRequeues int) bool {
						t.deleted <- obj.(*testV1.Toaster).Name
						return false
					},
				},
				SourceNamespace: t.framework.Namespace,
			},
		},
	})

	Expect(err).To(Succeed())

	return watcherObj
}
