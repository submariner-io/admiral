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

package fake

import (
	"context"
	"errors"
	"reflect"

	ctrlClient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func FailingGetInterceptor[T ctrlClient.Object]() interceptor.Funcs {
	return interceptor.Funcs{
		Get: func(ctx context.Context, client ctrlClient.WithWatch, key ctrlClient.ObjectKey, obj ctrlClient.Object,
			opts ...ctrlClient.GetOption,
		) error {
			if reflect.TypeOf(obj) == reflect.TypeFor[T]() {
				return errors.New("mock Get error")
			}

			return client.Get(ctx, key, obj, opts...)
		},
	}
}

func FailingListInterceptor[T ctrlClient.ObjectList]() interceptor.Funcs {
	return interceptor.Funcs{
		List: func(ctx context.Context, client ctrlClient.WithWatch, list ctrlClient.ObjectList, opts ...ctrlClient.ListOption) error {
			if reflect.TypeOf(list) == reflect.TypeFor[T]() {
				return errors.New("mock List error")
			}

			return client.List(ctx, list, opts...)
		},
	}
}

func FailingCreateInterceptor[T ctrlClient.Object]() interceptor.Funcs {
	return interceptor.Funcs{
		Create: func(ctx context.Context, client ctrlClient.WithWatch, obj ctrlClient.Object, opts ...ctrlClient.CreateOption) error {
			if reflect.TypeOf(obj) == reflect.TypeFor[T]() {
				return errors.New("mock Create error")
			}

			return client.Create(ctx, obj, opts...)
		},
	}
}

func FailingUpdateInterceptor[T ctrlClient.Object]() interceptor.Funcs {
	return interceptor.Funcs{
		Update: func(ctx context.Context, client ctrlClient.WithWatch, obj ctrlClient.Object, opts ...ctrlClient.UpdateOption) error {
			if reflect.TypeOf(obj) == reflect.TypeFor[T]() {
				return errors.New("mock Update error")
			}

			return client.Update(ctx, obj, opts...)
		},
	}
}

func FailingDeleteInterceptor[T ctrlClient.Object]() interceptor.Funcs {
	return interceptor.Funcs{
		Delete: func(ctx context.Context, client ctrlClient.WithWatch, obj ctrlClient.Object, opts ...ctrlClient.DeleteOption) error {
			if reflect.TypeOf(obj) == reflect.TypeFor[T]() {
				return errors.New("mock Delete error")
			}

			return client.Delete(ctx, obj, opts...)
		},
	}
}
