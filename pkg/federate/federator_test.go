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
package federate_test

import (
	"errors"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/fake"
	"github.com/submariner-io/admiral/pkg/federate"
	"github.com/submariner-io/admiral/pkg/syncer/test"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
)

var _ = Describe("CreateOrUpdate Federator", testCreateOrUpdateFederator)
var _ = Describe("Create Federator", testCreateFederator)
var _ = Describe("Update Federator", testUpdateFederator)
var _ = Describe("Federator Delete", testDelete)

func testCreateOrUpdateFederator() {
	var (
		f federate.Federator
		t *testDriver
	)

	BeforeEach(func() {
		t = newTestDriver()
	})

	JustBeforeEach(func() {
		f = federate.NewCreateOrUpdateFederator(t.dynClient, t.restMapper, t.federatorNamespace, t.localClusterID, t.keepMetadataFields...)
	})

	When("the resource does not already exist in the datastore", func() {
		Context("and a local cluster ID is specified", func() {
			It("should create the resource with the cluster ID label", func() {
				Expect(f.Distribute(t.resource)).To(Succeed())
				t.verifyResource()
			})
		})

		Context("and a local cluster ID is not specified", func() {
			BeforeEach(func() {
				t.localClusterID = ""
			})

			It("should create the resource without the cluster ID label", func() {
				Expect(f.Distribute(t.resource)).To(Succeed())
				t.verifyResource()
			})
		})

		Context("and the resource contains Status data", func() {
			BeforeEach(func() {
				t.resource.Status = corev1.PodStatus{
					Phase:   "PodRunning",
					Message: "Pod is running",
				}
			})

			It("should create the resource with the Status data", func() {
				Expect(f.Distribute(t.resource)).To(Succeed())
				t.verifyResource()
			})
		})

		Context("and the resource contains OwnerReferences", func() {
			BeforeEach(func() {
				t.keepMetadataFields = []string{"ownerReferences"}
				t.resource.OwnerReferences = []metav1.OwnerReference{
					{
						Kind: "DaemonSet",
						Name: "foo",
					},
				}
			})

			It("should create the resource with the OwnerReferences", func() {
				Expect(f.Distribute(t.resource)).To(Succeed())
				t.verifyResource()
			})
		})

		Context("and create fails", func() {
			BeforeEach(func() {
				t.resourceClient.FailOnCreate = apierrors.NewServiceUnavailable("fake")
			})

			It("should return an error", func() {
				Expect(f.Distribute(t.resource)).ToNot(Succeed())
			})
		})

		Context("and create returns AlreadyExists error due to a simulated out-of-band create", func() {
			BeforeEach(func() {
				t.resource.SetNamespace(t.targetNamespace)
				test.CreateResource(t.resourceClient, t.resource)
				t.resource = test.NewPodWithImage(test.LocalNamespace, "apache")

				t.resourceClient.FailOnGet = apierrors.NewNotFound(schema.GroupResource{}, t.resource.GetName())
				t.resourceClient.FailOnCreate = apierrors.NewAlreadyExists(schema.GroupResource{}, t.resource.GetName())
			})

			It("should update the resource", func() {
				Expect(f.Distribute(t.resource)).To(Succeed())
				t.verifyResource()
			})
		})
	})

	When("the resource already exists in the datastore", func() {
		BeforeEach(func() {
			t.resource.SetNamespace(t.targetNamespace)
			test.CreateResource(t.resourceClient, t.resource)
			t.resource = test.NewPodWithImage(test.LocalNamespace, "apache")
		})

		It("should update the resource", func() {
			Expect(f.Distribute(t.resource)).To(Succeed())
			t.verifyResource()
		})

		Context("and update initially fails due to conflict", func() {
			BeforeEach(func() {
				t.resourceClient.FailOnUpdate = apierrors.NewConflict(schema.GroupResource{}, "", errors.New("fake"))
			})

			It("should retry until it succeeds", func() {
				Expect(f.Distribute(t.resource)).To(Succeed())
				t.verifyResource()
			})
		})

		Context("and update fails not due to conflict", func() {
			BeforeEach(func() {
				t.resourceClient.FailOnUpdate = apierrors.NewServiceUnavailable("fake")
			})

			It("should return an error", func() {
				Expect(f.Distribute(t.resource)).ToNot(Succeed())
			})
		})
	})

	When("retrieval to find an existing resource in the datastore fails", func() {
		BeforeEach(func() {
			t.resourceClient.FailOnGet = apierrors.NewServiceUnavailable("fake")
		})

		It("should return an error", func() {
			Expect(f.Distribute(t.resource)).ToNot(Succeed())
		})
	})

	When("no target namespace is specified", func() {
		BeforeEach(func() {
			t.federatorNamespace = corev1.NamespaceAll
			t.resource.SetNamespace(t.targetNamespace)
		})

		It("should create the resource in the source namespace", func() {
			Expect(f.Distribute(t.resource)).To(Succeed())
			t.verifyResource()
		})
	})
}

func testCreateFederator() {
	var (
		f federate.Federator
		t *testDriver
	)

	BeforeEach(func() {
		t = newTestDriver()
		t.localClusterID = ""
	})

	JustBeforeEach(func() {
		f = federate.NewCreateFederator(t.dynClient, t.restMapper, t.federatorNamespace)
	})

	When("the resource does not already exist in the datastore", func() {
		It("create the resource", func() {
			Expect(f.Distribute(t.resource)).To(Succeed())
			t.verifyResource()
		})

		Context("and create fails", func() {
			BeforeEach(func() {
				t.resourceClient.FailOnCreate = apierrors.NewServiceUnavailable("fake")
			})

			It("should return an error", func() {
				Expect(f.Distribute(t.resource)).ToNot(Succeed())
			})
		})
	})

	When("the resource already exists in the datastore", func() {
		BeforeEach(func() {
			t.resource.SetNamespace(t.targetNamespace)
			test.CreateResource(t.resourceClient, t.resource)
		})

		It("should succeed and not update the resource", func() {
			Expect(f.Distribute(test.NewPodWithImage(test.LocalNamespace, "apache"))).To(Succeed())
			t.verifyResource()
		})
	})
}

func testUpdateFederator() {
	var (
		f federate.Federator
		t *testDriver
	)

	BeforeEach(func() {
		t = newTestDriver()
		t.localClusterID = ""
	})

	JustBeforeEach(func() {
		f = federate.NewUpdateFederator(t.dynClient, t.restMapper, t.federatorNamespace)
	})

	When("the resource exists in the datastore", func() {
		BeforeEach(func() {
			t.resource.SetNamespace(t.targetNamespace)
			test.CreateResource(t.resourceClient, t.resource)
			t.resource = test.NewPodWithImage(test.LocalNamespace, "apache")
			t.resource.Labels["newLabel"] = "xyz"
			t.resource.Annotations["newAnnotation"] = "abc"
		})

		It("should update the resource", func() {
			Expect(f.Distribute(t.resource)).To(Succeed())
			t.verifyResource()
		})

		Context("and update initially fails due to conflict", func() {
			BeforeEach(func() {
				t.resourceClient.FailOnUpdate = apierrors.NewConflict(schema.GroupResource{}, "", errors.New("fake"))
			})

			It("should retry until it succeeds", func() {
				Expect(f.Distribute(t.resource)).To(Succeed())
				t.verifyResource()
			})

			Context("and retrieval to find the existing resource fails", func() {
				BeforeEach(func() {
					t.resourceClient.FailOnGet = apierrors.NewServiceUnavailable("fake")
				})

				It("should return an error", func() {
					Expect(f.Distribute(t.resource)).ToNot(Succeed())
				})
			})
		})

		Context("and update fails not due to conflict", func() {
			BeforeEach(func() {
				t.resourceClient.FailOnUpdate = apierrors.NewServiceUnavailable("fake")
			})

			It("should return an error", func() {
				Expect(f.Distribute(t.resource)).ToNot(Succeed())
			})
		})
	})

	When("the resource does not exist in the datastore", func() {
		It("should succeed", func() {
			Expect(f.Distribute(t.resource)).To(Succeed())
		})
	})
}

func testDelete() {
	var (
		f federate.Federator
		t *testDriver
	)

	BeforeEach(func() {
		t = newTestDriver()
	})

	JustBeforeEach(func() {
		f = federate.NewCreateOrUpdateFederator(t.dynClient, t.restMapper, t.federatorNamespace, "")
	})

	When("the resource exists in the datastore", func() {
		BeforeEach(func() {
			existing := t.resource.DeepCopy()
			existing.SetNamespace(t.targetNamespace)
			test.CreateResource(t.resourceClient, existing)
		})

		It("should delete the resource", func() {
			Expect(f.Delete(t.resource)).To(Succeed())

			_, err := test.GetResourceAndError(t.resourceClient, t.resource)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		Context("and delete fails", func() {
			BeforeEach(func() {
				t.resourceClient.FailOnDelete = apierrors.NewServiceUnavailable("fake")
			})

			It("should return an error", func() {
				Expect(f.Delete(t.resource)).ToNot(Succeed())
			})
		})

		Context("and no target namespace is specified", func() {
			BeforeEach(func() {
				t.federatorNamespace = corev1.NamespaceAll
				t.resource.SetNamespace(t.targetNamespace)
			})

			It("should delete the resource from the source namespace", func() {
				Expect(f.Delete(t.resource)).To(Succeed())

				_, err := test.GetResourceAndError(t.resourceClient, t.resource)
				Expect(apierrors.IsNotFound(err)).To(BeTrue())
			})
		})
	})

	When("the resource does not exist in the datastore", func() {
		It("should return NotFound error", func() {
			Expect(apierrors.IsNotFound(f.Delete(t.resource))).To(BeTrue())
		})
	})
}

type testDriver struct {
	resource           *corev1.Pod
	localClusterID     string
	federatorNamespace string
	targetNamespace    string
	keepMetadataFields []string
	dynClient          *fake.DynamicClient
	resourceClient     *fake.DynamicResourceClient
	restMapper         meta.RESTMapper
	initObjs           []runtime.Object
}

func newTestDriver() *testDriver {
	t := &testDriver{
		resource:           test.NewPod(test.LocalNamespace),
		localClusterID:     "east",
		federatorNamespace: test.RemoteNamespace,
		targetNamespace:    test.RemoteNamespace,
	}

	var gvr *schema.GroupVersionResource

	t.dynClient = fake.NewDynamicClient(scheme.Scheme, test.PrepInitialClientObjs("", t.localClusterID, t.initObjs...)...)
	t.restMapper, gvr = test.GetRESTMapperAndGroupVersionResourceFor(t.resource)
	t.resourceClient, _ = t.dynClient.Resource(*gvr).Namespace(t.targetNamespace).(*fake.DynamicResourceClient)

	return t
}

func (t *testDriver) verifyResource() {
	test.VerifyResource(t.resourceClient, t.resource, t.targetNamespace, t.localClusterID)
}
