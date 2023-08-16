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

package broker

import (
	"fmt"
	"path/filepath"
	"reflect"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/submariner-io/admiral/pkg/federate"
	"github.com/submariner-io/admiral/pkg/log"
	"github.com/submariner-io/admiral/pkg/resource"
	"github.com/submariner-io/admiral/pkg/syncer"
	"github.com/submariner-io/admiral/pkg/util"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

type ResourceConfig struct {
	// SourceNamespace the namespace in the local source from which to retrieve the local resources to sync.
	LocalSourceNamespace string

	// LocalSourceLabelSelector optional selector to restrict the local resources to sync by their labels.
	LocalSourceLabelSelector string

	// LocalSourceFieldSelector optional selector to restrict the local resources to sync by their fields.
	LocalSourceFieldSelector string

	// LocalResourceType the type of the local resources to sync to the broker.
	LocalResourceType runtime.Object

	// TransformLocalToBroker function used to transform a local resource to the equivalent broker resource.
	TransformLocalToBroker syncer.TransformFunc

	// OnSuccessfulSyncToBroker function invoked after a successful sync operation to the broker.
	OnSuccessfulSyncToBroker syncer.OnSuccessfulSyncFunc

	// LocalResourcesEquivalent function to compare two local resources for equivalence. See ResourceSyncerConfig.ResourcesEquivalent
	// for more details.
	LocalResourcesEquivalent syncer.ResourceEquivalenceFunc

	// LocalShouldProcess function invoked to determine if a local resource should be processed.
	LocalShouldProcess syncer.ShouldProcessFunc

	// LocalWaitForCacheSync if true, waits for the local informer cache to sync on Start. Default is true.
	LocalWaitForCacheSync *bool

	// LocalResyncPeriod if non-zero, the period at which local resources will be re-synced regardless if anything changed.
	// Default is 0.
	LocalResyncPeriod time.Duration

	// BrokerResourceType the type of the broker resources to sync to the local source.
	BrokerResourceType runtime.Object

	// TransformBrokerToLocal function used to transform a broker resource to the equivalent local resource.
	TransformBrokerToLocal syncer.TransformFunc

	// OnSuccessfulSyncFromBroker function invoked after a successful sync operation from the broker.
	OnSuccessfulSyncFromBroker syncer.OnSuccessfulSyncFunc

	// BrokerResourcesEquivalent function to compare two broker resources for equivalence. See ResourceSyncerConfig.ResourcesEquivalent
	// for more details.
	BrokerResourcesEquivalent syncer.ResourceEquivalenceFunc

	// BrokerWaitForCacheSync if true, waits for the broker informer cache to sync on Start. Default is false.
	BrokerWaitForCacheSync *bool

	// BrokerResyncPeriod if non-zero, the period at which broker resources will be re-synced regardless if anything changed.
	// Default is 0.
	BrokerResyncPeriod time.Duration

	// SyncCounterOpts used to pass name and help text to resource syncer Gauge
	SyncCounterOpts *prometheus.GaugeOpts
}

type SyncerConfig struct {
	// LocalRestConfig the REST config used to access the local resources to sync.
	LocalRestConfig *rest.Config

	// LocalClient the client used to access local resources to sync. This is optional and is provided for unit testing
	// in lieu of the LocalRestConfig. If not specified, one is created from the LocalRestConfig.
	LocalClient dynamic.Interface

	// LocalNamespace the namespace in the local source to which resources from the broker will be synced.
	LocalNamespace string

	// LocalClusterID the ID of the local cluster. This is used to avoid loops when syncing the same resources between
	// the local and broker sources. If local resources are transformed to different broker resource types then
	// specify an empty LocalClusterID to disable this loop protection.
	LocalClusterID string

	// RestMapper used to obtain GroupVersionResources. This is optional and is provided for unit testing. If not specified,
	// one is created from the LocalRestConfig.
	RestMapper meta.RESTMapper

	// BrokerRestConfig the REST config used to access the broker resources to sync. If not specified and the BrokerClient
	// is not specified, the config is built from environment variables.
	BrokerRestConfig *rest.Config

	// BrokerClient the client used to access local resources to sync. This is optional and is provided for unit testing
	// in lieu of the BrokerRestConfig. If not specified, one is created from the BrokerRestConfig.
	BrokerClient dynamic.Interface

	// BrokerNamespace the namespace in the broker to which resources from the local source will be synced. If not
	// specified, the namespace is obtained from an environment variable.
	BrokerNamespace string

	// ResourceConfigs the configurations for resources to sync
	ResourceConfigs []ResourceConfig

	// Scheme used to convert resource objects. By default the global k8s Scheme is used.
	Scheme *runtime.Scheme
}

type Syncer struct {
	syncers         []syncer.Interface
	localSyncers    map[reflect.Type]syncer.Interface
	localFederator  federate.Federator
	remoteFederator federate.Federator
	brokerNamespace string
	brokerClient    dynamic.Interface
	localClient     dynamic.Interface
}

var logger = log.Logger{Logger: logf.Log.WithName("BrokerSyncer")}

// NewSyncer creates a Syncer that performs bi-directional syncing of resources between a local source and a central broker.
func NewSyncer(config SyncerConfig) (*Syncer, error) { //nolint:gocritic // Minimal performance hit, we modify our copy
	if len(config.ResourceConfigs) == 0 {
		return nil, fmt.Errorf("no resources to sync")
	}

	var err error

	if config.RestMapper == nil {
		config.RestMapper, err = util.BuildRestMapper(config.LocalRestConfig)
		if err != nil {
			return nil, errors.Wrap(err, "error building the REST mapper")
		}
	}

	if config.LocalClient == nil {
		config.LocalClient, err = resource.NewDynamicClient(config.LocalRestConfig)
		if err != nil {
			return nil, errors.Wrap(err, "error creating dynamic client")
		}
	}

	if config.BrokerClient == nil {
		if err := createBrokerClient(&config); err != nil {
			return nil, err
		}
	}

	brokerSyncer := &Syncer{
		syncers:         []syncer.Interface{},
		localSyncers:    make(map[reflect.Type]syncer.Interface),
		brokerNamespace: config.BrokerNamespace,
		brokerClient:    config.BrokerClient,
		localClient:     config.LocalClient,
	}

	brokerSyncer.remoteFederator = NewFederator(config.BrokerClient, config.RestMapper, config.BrokerNamespace, config.LocalClusterID)
	brokerSyncer.localFederator = NewFederator(config.LocalClient, config.RestMapper, config.LocalNamespace, "")

	for i := range config.ResourceConfigs {
		rc := &config.ResourceConfigs[i]
		var syncCounter *prometheus.GaugeVec
		if rc.SyncCounterOpts != nil {
			syncCounter = prometheus.NewGaugeVec(
				*rc.SyncCounterOpts,
				[]string{
					syncer.DirectionLabel,
					syncer.OperationLabel,
					syncer.SyncerNameLabel,
				},
			)
			prometheus.MustRegister(syncCounter)
		}

		localSyncer, err := syncer.NewResourceSyncer(&syncer.ResourceSyncerConfig{
			Name:                fmt.Sprintf("local -> broker for %T", rc.LocalResourceType),
			SourceClient:        config.LocalClient,
			SourceNamespace:     rc.LocalSourceNamespace,
			SourceLabelSelector: rc.LocalSourceLabelSelector,
			SourceFieldSelector: rc.LocalSourceFieldSelector,
			LocalClusterID:      config.LocalClusterID,
			Direction:           syncer.LocalToRemote,
			RestMapper:          config.RestMapper,
			Federator:           brokerSyncer.remoteFederator,
			ResourceType:        rc.LocalResourceType,
			Transform:           rc.TransformLocalToBroker,
			OnSuccessfulSync:    rc.OnSuccessfulSyncToBroker,
			ResourcesEquivalent: rc.LocalResourcesEquivalent,
			ShouldProcess:       rc.LocalShouldProcess,
			WaitForCacheSync:    rc.LocalWaitForCacheSync,
			Scheme:              config.Scheme,
			ResyncPeriod:        rc.LocalResyncPeriod,
			SyncCounter:         syncCounter,
		})
		if err != nil {
			return nil, errors.Wrap(err, "error creating local resource syncer")
		}

		brokerSyncer.syncers = append(brokerSyncer.syncers, localSyncer)
		brokerSyncer.localSyncers[reflect.TypeOf(rc.LocalResourceType)] = localSyncer

		waitForCacheSync := ptr.Deref(rc.BrokerWaitForCacheSync, false)

		remoteSyncer, err := syncer.NewResourceSyncer(&syncer.ResourceSyncerConfig{
			Name:                fmt.Sprintf("broker -> local for %T", rc.BrokerResourceType),
			SourceClient:        config.BrokerClient,
			SourceNamespace:     config.BrokerNamespace,
			SourceLabelSelector: rc.LocalSourceLabelSelector,
			SourceFieldSelector: rc.LocalSourceFieldSelector,
			LocalClusterID:      config.LocalClusterID,
			Direction:           syncer.RemoteToLocal,
			RestMapper:          config.RestMapper,
			Federator:           brokerSyncer.localFederator,
			ResourceType:        rc.BrokerResourceType,
			Transform:           rc.TransformBrokerToLocal,
			OnSuccessfulSync:    rc.OnSuccessfulSyncFromBroker,
			ResourcesEquivalent: rc.BrokerResourcesEquivalent,
			WaitForCacheSync:    &waitForCacheSync,
			Scheme:              config.Scheme,
			ResyncPeriod:        rc.BrokerResyncPeriod,
			SyncCounter:         syncCounter,
		})
		if err != nil {
			return nil, errors.Wrap(err, "error creating remote resource syncer")
		}

		brokerSyncer.syncers = append(brokerSyncer.syncers, remoteSyncer)
	}

	return brokerSyncer, nil
}

func createBrokerClient(config *SyncerConfig) error {
	_, gvr, e := util.ToUnstructuredResource(config.ResourceConfigs[0].BrokerResourceType, config.RestMapper)
	if e != nil {
		return e //nolint:wrapcheck // OK to return the error as is.
	}

	var authorized bool
	var err error

	if config.BrokerRestConfig != nil {
		// We have an existing REST configuration, assume it’s correct (but check it anyway)
		authorized, err = resource.IsAuthorizedFor(config.BrokerRestConfig, *gvr, config.BrokerNamespace)
	} else {
		spec, e := getBrokerSpecification()
		if e != nil {
			return e
		}

		config.BrokerNamespace = spec.RemoteNamespace

		// If we have a secret, try to use it
		if spec.Secret != "" {
			config.BrokerRestConfig, authorized, err = resource.GetAuthorizedRestConfigFromFiles(spec.APIServer,
				filepath.Join(SecretPath(spec.Secret), "token"), filepath.Join(SecretPath(spec.Secret), "ca.crt"),
				&rest.TLSClientConfig{Insecure: spec.Insecure}, *gvr, spec.RemoteNamespace)
			if err != nil {
				logger.Errorf(err, "Error accessing the %s secret", spec.Secret)
			}
		}

		// If we encountered an error, or we don't have a secret, use the values in the spec
		if spec.Secret == "" || err != nil {
			config.BrokerRestConfig, authorized, err = resource.GetAuthorizedRestConfigFromData(spec.APIServer, spec.APIServerToken, spec.Ca,
				&rest.TLSClientConfig{Insecure: spec.Insecure}, *gvr, spec.RemoteNamespace)
		}
	}

	if !authorized {
		return errors.Wrap(err, "error authorizing access to the broker API server")
	}

	if err != nil {
		logger.Error(err, "Error accessing the broker API server")
	}

	config.BrokerClient, err = resource.NewDynamicClient(config.BrokerRestConfig)

	return errors.Wrap(err, "error creating dynamic client")
}

func (s *Syncer) Start(stopCh <-chan struct{}) error {
	for _, syncer := range s.syncers {
		err := syncer.Start(stopCh)
		if err != nil {
			return err //nolint:wrapcheck // OK to return the error as is.
		}
	}

	for i := 0; i < len(s.syncers); i += 2 {
		localSyncer := s.syncers[i]
		remoteSyncer := s.syncers[i+1]

		remoteSyncer.Reconcile(localSyncer.ListResources)
		localSyncer.Reconcile(remoteSyncer.ListResources)
	}

	return nil
}

func (s *Syncer) GetBrokerFederator() federate.Federator {
	return s.remoteFederator
}

func (s *Syncer) GetLocalFederator() federate.Federator {
	return s.localFederator
}

func (s *Syncer) localSyncerFor(ofType runtime.Object) syncer.Interface {
	ls, found := s.localSyncers[reflect.TypeOf(ofType)]
	if !found {
		panic(fmt.Errorf("no Syncer found for %#v", ofType))
	}

	return ls
}

func (s *Syncer) GetLocalResource(name, namespace string, ofType runtime.Object) (runtime.Object, bool, error) {
	return s.localSyncerFor(ofType).GetResource(name, namespace) //nolint:wrapcheck // OK to return the error as is.
}

func (s *Syncer) ListLocalResources(ofType runtime.Object) []runtime.Object {
	return s.localSyncerFor(ofType).ListResources()
}

func (s *Syncer) ListLocalResourcesBySelector(ofType runtime.Object, selector labels.Selector) []runtime.Object {
	return s.localSyncerFor(ofType).ListResourcesBySelector(selector)
}

func (s *Syncer) GetBrokerNamespace() string {
	return s.brokerNamespace
}

func (s *Syncer) GetBrokerClient() dynamic.Interface {
	return s.brokerClient
}

func (s *Syncer) GetLocalClient() dynamic.Interface {
	return s.localClient
}
