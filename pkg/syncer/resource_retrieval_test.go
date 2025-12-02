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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/syncer"
	"github.com/submariner-io/admiral/pkg/syncer/test"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

func testGetResource() {
	t := newTestDriver(test.LocalNamespace, "", syncer.LocalToRemote)

	When("the requested resource exists", func() {
		BeforeEach(func() {
			t.addInitialResource(t.resource)
		})

		It("should return the resource", func() {
			obj, exists, err := t.syncer.GetResource(t.resource.Name, t.resource.Namespace)
			Expect(err).To(Succeed())
			Expect(exists).To(BeTrue())

			pod, ok := obj.(*corev1.Pod)
			Expect(ok).To(BeTrue())
			Expect(pod.Name).To(Equal(t.resource.Name))
			Expect(pod.Spec).To(Equal(t.resource.Spec))
		})
	})

	When("the requested resource does not exist", func() {
		It("should return false", func() {
			_, exists, err := t.syncer.GetResource(t.resource.Name, t.resource.Namespace)
			Expect(err).To(Succeed())
			Expect(exists).To(BeFalse())
		})
	})
}

func testListResources() {
	t := newTestDriver(test.LocalNamespace, "", syncer.LocalToRemote)

	var resource2 *corev1.Pod

	BeforeEach(func() {
		resource2 = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "apache-pod",
				Namespace: t.resource.Namespace,
				Labels:    map[string]string{"foo": "bar"},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Image: "apache",
						Name:  "httpd",
					},
				},
			},
		}

		t.addInitialResource(t.resource)
		t.addInitialResource(resource2)
	})

	It("should return all the resources", func() {
		list := t.syncer.ListResources()
		assertResourceList(list, t.resource, resource2)
	})
}

func testListResourcesBySelector() {
	t := newTestDriver(test.LocalNamespace, "", syncer.LocalToRemote)

	newPod := func(i int, labels map[string]string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("pod%d", i),
				Namespace: fmt.Sprintf("namespace%d", i),
				Labels:    labels,
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Image: fmt.Sprintf("image%d", i),
						Name:  fmt.Sprintf("container%d", i),
					},
				},
			},
		}
	}

	var (
		pod1 = newPod(1, map[string]string{
			"foo":    "bar",
			"label1": "match",
			"label2": "no-match",
		})

		pod2 = newPod(2, map[string]string{
			"label1": "match",
		})

		pod3 = newPod(3, map[string]string{
			"label1": "no-match",
			"label2": "match",
		})

		pod4 = newPod(4, map[string]string{
			"label1": "no-match",
		})

		pod5 = newPod(5, map[string]string{
			"label1": "match",
			"label2": "match",
		})
	)

	BeforeEach(func() {
		t.addInitialResource(t.resource)
		t.addInitialResource(pod1)
		t.addInitialResource(pod2)
		t.addInitialResource(pod3)
		t.addInitialResource(pod4)
		t.addInitialResource(pod5)
	})

	It("should return correct resources for label1 selector", func() {
		list := t.syncer.ListResourcesBySelector(labels.Set(map[string]string{"label1": "match"}).AsSelector())
		assertResourceList(list, pod1, pod2, pod5)
	})

	It("should return correct resources for label2 selector", func() {
		list := t.syncer.ListResourcesBySelector(labels.Set(map[string]string{"label2": "match"}).AsSelector())
		assertResourceList(list, pod3, pod5)
	})

	It("should return correct resources for label1 and label2 selector", func() {
		list := t.syncer.ListResourcesBySelector(labels.Set(map[string]string{
			"label1": "match",
			"label2": "match",
		}).AsSelector())
		assertResourceList(list, pod5)
	})

	It("should return no resources for label3 selector", func() {
		list := t.syncer.ListResourcesBySelector(labels.Set(map[string]string{"label3": "match"}).AsSelector())
		assertResourceList(list)
	})
}
