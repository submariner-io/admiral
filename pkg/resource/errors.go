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

package resource

import (
	"errors"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
)

func IsNotFoundErr(err error) bool {
	if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
		return true
	}

	var errGDF *discovery.ErrGroupDiscoveryFailed
	if !errors.As(err, &errGDF) {
		return false
	}

	for _, err := range errGDF.Groups {
		return IsNotFoundErr(err)
	}

	return false
}

func IsMissingNamespaceErr(err error) bool {
	if !apierrors.IsNotFound(err) {
		return false
	}

	var status apierrors.APIStatus
	_ = errors.As(err, &status)

	d := status.Status().Details

	return d != nil && d.Kind == "namespaces" && d.Group == ""
}

func ExtractMissingNamespaceFromErr(err error) string {
	if !IsMissingNamespaceErr(err) {
		return ""
	}

	var status apierrors.APIStatus
	_ = errors.As(err, &status)

	d := status.Status().Details

	return d.Name
}
