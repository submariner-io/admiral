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
	"context"
	"fmt"
	"slices"

	"github.com/submariner-io/admiral/pkg/log"
	"github.com/submariner-io/admiral/pkg/resource"
	"github.com/submariner-io/admiral/pkg/util"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
)

type CreateOrUpdateOptions struct {
	Client             dynamic.Interface
	RestMapper         meta.RESTMapper
	TargetNamespace    string
	LocalClusterID     string
	IdentifyingLabels  []string
	KeepMetadataFields []string
}

type createOrUpdateFederator struct {
	*baseFederator
	localClusterID    string
	identifyingLabels []string
}

//nolint:gocritic // Ignore hugeParam
func NewCreateOrUpdateFederator(options CreateOrUpdateOptions) FederatorExt {
	return &createOrUpdateFederator{
		baseFederator:     newBaseFederator(options.Client, options.RestMapper, options.TargetNamespace, options.KeepMetadataFields),
		localClusterID:    options.LocalClusterID,
		identifyingLabels: options.IdentifyingLabels,
	}
}

func (f *createOrUpdateFederator) Distribute(ctx context.Context, obj runtime.Object) error {
	f.logger.V(log.DEBUG).Infof("In Distribute for %s", resource.JSONStringer{Obj: obj})

	toDistribute, resourceClient, err := f.toUnstructured(obj)
	if err != nil {
		return err
	}

	if f.localClusterID != "" {
		util.SetNestedField(toDistribute.Object, f.localClusterID, util.MetadataField, util.LabelsField, ClusterIDLabelKey)
	}

	f.prepareResourceForSync(toDistribute)

	result, newObj, err := util.CreateOrUpdateWithOptions[*unstructured.Unstructured](ctx,
		util.CreateOrUpdateOptions[*unstructured.Unstructured]{
			Client:            resource.ForDynamic(resourceClient),
			Obj:               toDistribute,
			IdentifyingLabels: f.getIdentifyingLabels(toDistribute),
			MutateOnUpdate: func(obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
				return util.CopyImmutableMetadata(obj, toDistribute), nil
			},
		})

	if f.eventLogName != "" {
		if result == util.OperationResultCreated {
			f.logger.Infof("%s: Created %s \"%s/%s\" ", f.eventLogName, newObj.GetKind(), newObj.GetNamespace(),
				newObj.GetName())
		} else if result == util.OperationResultUpdated {
			f.logger.Infof("%s: Updated %s \"%s/%s\" ", f.eventLogName, newObj.GetKind(), newObj.GetNamespace(),
				newObj.GetName())
		}
	}

	return err
}

func (f *createOrUpdateFederator) Delete(ctx context.Context, obj runtime.Object) error {
	objMeta := resource.MustToMeta(obj)
	if objMeta.GetName() == "" && len(f.identifyingLabels) > 0 {
		toDelete, resourceClient, err := f.toUnstructured(obj)
		if err != nil {
			return err
		}

		identifyingLabels := f.getIdentifyingLabels(toDelete)

		list, err := resourceClient.List(ctx, metav1.ListOptions{
			LabelSelector: labels.SelectorFromSet(identifyingLabels).String(),
		})
		if err != nil {
			return err //nolint:wrapcheck // No need to wrap
		}

		// Filter out resources whose GenerateName field doesn't match.
		list.Items = slices.DeleteFunc(list.Items, func(u unstructured.Unstructured) bool {
			return resource.MustToMeta(&u).GetGenerateName() != objMeta.GetGenerateName()
		})

		if len(list.Items) > 1 {
			return fmt.Errorf("found %d resources with labels %#v, expected 1",
				len(list.Items), identifyingLabels)
		}

		if len(list.Items) == 1 {
			obj = obj.DeepCopyObject()
			resource.MustToMeta(obj).SetName(list.Items[0].GetName())
		}
	}

	return f.baseFederator.Delete(ctx, obj)
}

func (f *createOrUpdateFederator) getIdentifyingLabels(obj *unstructured.Unstructured) map[string]string {
	if len(f.identifyingLabels) == 0 {
		return nil
	}

	identifyingLabels := map[string]string{}

	for _, l := range f.identifyingLabels {
		identifyingLabels[l] = obj.GetLabels()[l]
	}

	return identifyingLabels
}
