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

package federate

import (
	"k8s.io/apimachinery/pkg/runtime"
)

// ClusterIDLabelKey is the key for a label that may be added to federated resources to hold the ID of the cluster from
// which the resource originated, allowing for filtering of resources emanating from the originating cluster.
const ClusterIDLabelKey = "submariner-io/clusterID"

// Federator provides methods for accessing federated resources.
type Federator interface {
	// Distribute distributes the given resource to all federated clusters.
	// The actual distribution may occur asynchronously in which case any returned error only indicates that the request
	// failed.
	//
	// If the resource was previously distributed and the given resource differs, each previous cluster will receive the
	// updated resource.
	Distribute(resource runtime.Object) error

	// Delete stops distributing the given resource and deletes it from all clusters to which it was distributed.
	// The actual deletion may occur asynchronously in which any returned error only indicates that the request
	// failed.
	Delete(resource runtime.Object) error
}

type noopFederator struct{}

func NewNoopFederator() Federator {
	return &noopFederator{}
}

func (n noopFederator) Distribute(resource runtime.Object) error {
	return nil
}

func (n noopFederator) Delete(resource runtime.Object) error {
	return nil
}
