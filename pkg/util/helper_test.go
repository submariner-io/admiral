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

package util_test

import (
	"crypto/x509"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/resource"
	"github.com/submariner-io/admiral/pkg/syncer/test"
	"github.com/submariner-io/admiral/pkg/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
)

var _ = Describe("DeeplyEmpty", func() {
	Specify("with an empty map should return true", func() {
		Expect(util.DeeplyEmpty(map[string]interface{}{})).To(BeTrue())
	})

	Specify("with nested empty maps should return true", func() {
		Expect(util.DeeplyEmpty(map[string]interface{}{
			"nested": map[string]interface{}{},
		})).To(BeTrue())

		Expect(util.DeeplyEmpty(map[string]interface{}{
			"nested1": map[string]interface{}{},
			"nested2": map[string]interface{}{},
		})).To(BeTrue())

		Expect(util.DeeplyEmpty(map[string]interface{}{
			"nested1": map[string]interface{}{
				"nested2": map[string]interface{}{},
			},
		})).To(BeTrue())
	})

	Specify("with a non-empty map should return false", func() {
		Expect(util.DeeplyEmpty(map[string]interface{}{"foo": "bar"})).To(BeFalse())
	})

	Specify("with nested non-empty maps should return false", func() {
		Expect(util.DeeplyEmpty(map[string]interface{}{
			"nested": map[string]interface{}{},
			"foo":    "bar",
		})).To(BeFalse())

		Expect(util.DeeplyEmpty(map[string]interface{}{
			"foo":    "bar",
			"nested": map[string]interface{}{},
		})).To(BeFalse())

		Expect(util.DeeplyEmpty(map[string]interface{}{
			"nested1": map[string]interface{}{
				"nested2": map[string]interface{}{},
				"foo":     "bar",
			},
		})).To(BeFalse())
	})

	Specify("an empty Service status should return true", func() {
		Expect(util.DeeplyEmpty(util.GetNestedField(resource.MustToUnstructuredUsingDefaultConverter(&corev1.Service{}),
			util.StatusField).(map[string]interface{}))).To(BeTrue())
	})
})

var _ = Describe("FindGroupVersionResource", func() {
	Specify("should return the GVR if existing", func() {
		restMapper := test.GetRESTMapperFor(&corev1.Pod{})

		gvr, err := util.FindGroupVersionResource(resource.MustToUnstructured(&corev1.Pod{}), restMapper)
		Expect(err).To(Succeed())
		Expect(*gvr).To(Equal(corev1.SchemeGroupVersion.WithResource("pods")))
	})

	Specify("should return an error if non-existent", func() {
		restMapper := test.GetRESTMapperFor(&corev1.Pod{})

		_, err := util.FindGroupVersionResource(resource.MustToUnstructured(&corev1.Service{}), restMapper)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("ToUnstructuredResource", func() {
	Specify("should return the Unstructured obj and GVR on success", func() {
		restMapper := test.GetRESTMapperFor(&corev1.Pod{})

		obj, gvr, err := util.ToUnstructuredResource(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-pod",
			},
			Spec: corev1.PodSpec{
				NodeName: "node",
			},
		}, restMapper)

		Expect(err).To(Succeed())
		Expect(*gvr).To(Equal(corev1.SchemeGroupVersion.WithResource("pods")))
		Expect(obj.GetName()).To(Equal("test-pod"))

		spec := &corev1.PodSpec{}
		_ = runtime.DefaultUnstructuredConverter.FromUnstructured(util.GetSpec(obj).(map[string]interface{}), spec)
		Expect(spec.NodeName).To(Equal("node"))
	})

	Specify("should return an error if GVR non-existent", func() {
		restMapper := test.GetRESTMapperFor(&corev1.Service{})

		_, _, err := util.ToUnstructuredResource(&corev1.Pod{}, restMapper)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("AddCertificateErrorHandler", func() {
	unknownAuthorityError := x509.UnknownAuthorityError{Cert: &x509.Certificate{
		Raw: []byte{1, 2, 3},
	}}

	certificateInvalidError := x509.CertificateInvalidError{Cert: &x509.Certificate{
		Raw: []byte{4, 5, 6},
	}}

	var (
		errorLogged chan error
		fatalLogged chan error
	)

	BeforeEach(func() {
		errorLogged = make(chan error, 20)
		fatalLogged = make(chan error, 20)

		util.ErrorHook = func(err error, _ string, _ ...interface{}) {
			errorLogged <- err
		}

		util.FatalHook = func(err error, _ string, _ ...interface{}) {
			fatalLogged <- err
		}

		savedErrorHandlers := utilruntime.ErrorHandlers
		DeferCleanup(func() {
			utilruntime.ErrorHandlers = savedErrorHandlers
		})
	})

	testCertificateErrorHandler := func(fatal bool, logged chan error) {
		util.AddCertificateErrorHandler(fatal)

		utilruntime.HandleError(unknownAuthorityError)
		Expect(logged).To(Receive())

		utilruntime.HandleError(unknownAuthorityError)
		Expect(logged).ToNot(Receive())

		utilruntime.HandleError(certificateInvalidError)
		Expect(logged).To(Receive())

		utilruntime.HandleError(certificateInvalidError)
		Expect(logged).ToNot(Receive())
	}

	Context("with non-fatal", func() {
		It("should log an error message on first certificate error", func() {
			testCertificateErrorHandler(false, errorLogged)
		})
	})

	Context("with fatal", func() {
		It("should log a fatal message on first certificate error", func() {
			testCertificateErrorHandler(true, fatalLogged)
		})
	})
})

var _ = Describe("GetMetadata", func() {
	Specify("should return the metadata map if existing", func() {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "test-pod",
				Labels: map[string]string{"foo": "bar"},
			},
		}

		objMeta := util.GetMetadata(resource.MustToUnstructured(pod))
		Expect(objMeta).ToNot(BeNil())
		Expect(objMeta).To(HaveKeyWithValue("name", pod.Name))
		Expect(objMeta).To(HaveKeyWithValue("labels", map[string]interface{}{"foo": "bar"}))

		Expect(util.GetMetadata(&unstructured.Unstructured{})).To(BeEmpty())
	})
})
