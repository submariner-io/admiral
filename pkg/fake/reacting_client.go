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

	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type VerbType int

const (
	Get VerbType = iota
	List
	Create
	Update
	Delete
)

type ReactionFunc func(obj interface{}) (bool, error)

type ReactingClient struct {
	client.Client
	reactors map[VerbType]map[reflect.Type]ReactionFunc
}

func NewReactingClient(c client.Client) *ReactingClient {
	if c == nil {
		c = fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
	}

	return &ReactingClient{
		Client: c,
		reactors: map[VerbType]map[reflect.Type]ReactionFunc{
			Get:    {},
			List:   {},
			Create: {},
			Update: {},
			Delete: {},
		},
	}
}

func (c *ReactingClient) AddReactor(verb VerbType, objType interface{}, r ReactionFunc) *ReactingClient {
	c.reactors[verb][reflect.TypeOf(objType)] = r
	return c
}

func (c *ReactingClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object) error {
	return c.react(Get, obj, func() error {
		return c.Client.Get(ctx, key, obj)
	})
}

func (c *ReactingClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return c.react(List, list, func() error {
		return c.Client.List(ctx, list, opts...)
	})
}

func (c *ReactingClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	return c.react(Create, obj, func() error {
		return c.Client.Create(ctx, obj, opts...)
	})
}

func (c *ReactingClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	return c.react(Update, obj, func() error {
		return c.Client.Update(ctx, obj, opts...)
	})
}

func (c *ReactingClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	return c.react(Delete, obj, func() error {
		return c.Client.Delete(ctx, obj, opts...)
	})
}

func (c *ReactingClient) react(verb VerbType, obj interface{}, fallBack func() error) error {
	reactor := c.reactors[verb][reflect.TypeOf(obj)]
	if reactor != nil {
		handled, err := reactor(obj)
		if err != nil || handled {
			return err
		}
	}

	return fallBack()
}

func FailingReaction(err error) func(obj interface{}) (bool, error) {
	if err == nil {
		err = errors.New("mock error")
	}

	return func(obj interface{}) (bool, error) {
		return true, err
	}
}
