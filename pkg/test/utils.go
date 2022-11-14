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

package test

import (
	"context"

	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/resource"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func SetDeleting(client resource.Interface, name string) {
	obj, err := client.Get(context.TODO(), name, metav1.GetOptions{})
	Expect(err).To(Succeed())

	m, err := meta.Accessor(obj)
	Expect(err).To(Succeed())

	now := metav1.Now()
	m.SetDeletionTimestamp(&now)

	_, err = client.Update(context.TODO(), obj, metav1.UpdateOptions{})
	Expect(err).To(Succeed())
}

func GetFinalizers(client resource.Interface, name string) []string {
	obj, err := client.Get(context.TODO(), name, metav1.GetOptions{})
	Expect(err).To(Succeed())

	metaObj, err := meta.Accessor(obj)
	Expect(err).To(Succeed())

	return metaObj.GetFinalizers()
}
