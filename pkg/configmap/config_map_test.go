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

package configmap_test

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/configmap"
	"github.com/submariner-io/admiral/pkg/fake"
	"github.com/submariner-io/admiral/pkg/resource"
	"github.com/submariner-io/admiral/pkg/syncer/test"
	assert "github.com/submariner-io/admiral/pkg/test"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

var _ = Describe("", func() {
	Describe("Get", testGet)
	Describe("WatchAndSignalOnChange", testWatchAndSignalOnChange)
})

func testGet() {
	var (
		k8sClient *k8sfake.Clientset
		client    resource.Interface[*corev1.ConfigMap]
	)

	BeforeEach(func() {
		k8sClient = k8sfake.NewClientset()
		client = resource.ForConfigMap(k8sClient, test.LocalNamespace)
	})

	When("the ConfigMap exists", func() {
		It("should return it", func(ctx SpecContext) {
			expected, err := client.Create(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cm",
				},
				Data: map[string]string{"foo": "bar"},
			}, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			actual, err := configmap.Get(ctx, client, expected.Name)
			Expect(err).ToNot(HaveOccurred())
			Expect(actual).To(Equal(expected))
		})
	})

	When("the ConfigMap does not exist", func() {
		It("should return an empty ConfigMap", func(ctx SpecContext) {
			cm, err := configmap.Get(ctx, client, "test-cm")
			Expect(err).ToNot(HaveOccurred())
			Expect(cm.Name).To(Equal("test-cm"))
			Expect(cm.Data).To(BeEmpty())
		})
	})

	When("ConfigMap retrieval fails", func() {
		It("should return an error", func(ctx SpecContext) {
			fake.FailOnAction(&k8sClient.Fake, "configmaps", "get", nil, false)

			_, err := configmap.Get(ctx, client, "test-cm")
			Expect(err).To(HaveOccurred())
		})
	})
}

func testWatchAndSignalOnChange() {
	const (
		configMap1 = "configMap1"
		configMap2 = "configMap2"
		signalNum  = syscall.SIGUSR2
	)

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, signalNum)

	var (
		k8sClient *k8sfake.Clientset
		configMap *corev1.ConfigMap
	)

	BeforeEach(func() {
		k8sClient = k8sfake.NewClientset()
	})

	JustBeforeEach(func() {
		ctx, cancel := context.WithCancel(context.Background())

		DeferCleanup(func() {
			cancel()
		})

		configmap.WatchAndSignalOnChange(ctx, k8sClient, test.LocalNamespace, signalNum, configMap1, configMap2)

		assert.AwaitWatchAction(&k8sClient.Fake, "configmaps")
	})

	createConfigMap := func(ctx context.Context, name string, data map[string]string) *corev1.ConfigMap {
		cm, err := k8sClient.CoreV1().ConfigMaps(test.LocalNamespace).Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Data:       data,
		}, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		return cm
	}

	When("a target ConfigMap is created", func() {
		It("should send a signal", func(ctx context.Context) {
			createConfigMap(ctx, configMap1, nil)

			Eventually(signalCh).Should(Receive(Equal(signalNum)))
		})
	})

	When("an existing target ConfigMap is updated", func() {
		BeforeEach(func(ctx context.Context) {
			configMap = createConfigMap(ctx, configMap2, map[string]string{"foo": "initial"})
		})

		It("should send a signal", func(ctx context.Context) {
			Consistently(signalCh).Within(time.Millisecond * 500).ShouldNot(Receive())

			configMap.Data["foo"] = "updated"
			_, err := k8sClient.CoreV1().ConfigMaps(test.LocalNamespace).Update(ctx, configMap, metav1.UpdateOptions{})
			Expect(err).ToNot(HaveOccurred())

			Eventually(signalCh).Should(Receive(Equal(signalNum)))
		})
	})

	When("an existing target ConfigMap is deleted", func() {
		BeforeEach(func(ctx context.Context) {
			configMap = createConfigMap(ctx, configMap1, map[string]string{"foo": "initial"})
		})

		It("should send a signal", func(ctx context.Context) {
			Consistently(signalCh).Within(time.Millisecond * 500).ShouldNot(Receive())

			Expect(k8sClient.CoreV1().ConfigMaps(test.LocalNamespace).Delete(ctx, configMap1, metav1.DeleteOptions{})).To(Succeed())

			Eventually(signalCh).Should(Receive(Equal(signalNum)))
		})
	})

	When("a non-target ConfigMap is created", func() {
		It("should not send a signal", func(ctx context.Context) {
			createConfigMap(ctx, "other", nil)

			Consistently(signalCh).ShouldNot(Receive())
		})
	})
}
