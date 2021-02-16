/*
© 2020 Red Hat, Inc.

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
	"crypto/x509"
	"fmt"
	"net/url"
	"reflect"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/submariner-io/admiral/pkg/federate"
	"github.com/submariner-io/admiral/pkg/syncer"
	"github.com/submariner-io/admiral/pkg/util"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
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

	// LocalTransform function used to transform a local resource to the equivalent broker resource.
	LocalTransform syncer.TransformFunc

	// OnSuccessfulSync function invoked after a successful sync operation.
	LocalOnSuccessfulSync syncer.OnSuccessfulSyncFunc

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

	// BrokerTransform function used to transform a broker resource to the equivalent local resource.
	BrokerTransform syncer.TransformFunc

	// BrokerResourcesEquivalent function to compare two broker resources for equivalence. See ResourceSyncerConfig.ResourcesEquivalent
	// for more details.
	BrokerResourcesEquivalent syncer.ResourceEquivalenceFunc

	// BrokerWaitForCacheSync if true, waits for the broker informer cache to sync on Start. Default is false.
	BrokerWaitForCacheSync *bool

	// BrokerResyncPeriod if non-zero, the period at which broker resources will be re-synced regardless if anything changed.
	// Default is 0.
	BrokerResyncPeriod time.Duration

	// MetricOpts used to pass name and help text to resource syncer Gauge
	MetricOpts *prometheus.GaugeOpts
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
	// is not specified, the config is built from environment variables via BuildBrokerConfigFromEnv.
	BrokerRestConfig *rest.Config

	// BrokerClient the client used to access local resources to sync. This is optional and is provided for unit testing
	// in lieu of the BrokerRestConfig. If not specified, one is created from the BrokerRestConfig.
	BrokerClient dynamic.Interface

	// BrokerNamespace the namespace in the broker to which resources from the local source will be synced. If not
	// specified, the namespace is obtained from an environment variable via BuildBrokerConfigFromEnv.
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
}

// NewSyncer creates a Syncer that performs bi-directional syncing of resources between a local source and a central broker.
func NewSyncer(config SyncerConfig) (*Syncer, error) {
	if len(config.ResourceConfigs) == 0 {
		return nil, fmt.Errorf("no resources to sync")
	}

	var err error

	if config.RestMapper == nil {
		config.RestMapper, err = util.BuildRestMapper(config.LocalRestConfig)
		if err != nil {
			return nil, err
		}
	}

	if config.LocalClient == nil {
		config.LocalClient, err = dynamic.NewForConfig(config.LocalRestConfig)
		if err != nil {
			return nil, fmt.Errorf("error creating dynamic client: %v", err)
		}
	}

	if config.BrokerClient == nil {
		if err := createBrokerClient(&config); err != nil {
			return nil, err
		}
	}

	brokerSyncer := &Syncer{
		syncers:      []syncer.Interface{},
		localSyncers: make(map[reflect.Type]syncer.Interface),
	}

	brokerSyncer.remoteFederator = NewFederator(config.BrokerClient, config.RestMapper, config.BrokerNamespace, config.LocalClusterID)
	brokerSyncer.localFederator = NewFederator(config.LocalClient, config.RestMapper, config.LocalNamespace, "")

	for _, rc := range config.ResourceConfigs {
		var syncCounter *prometheus.GaugeVec
		if rc.MetricOpts != nil {
			syncCounter = prometheus.NewGaugeVec(
				*rc.MetricOpts,
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
			Transform:           rc.LocalTransform,
			OnSuccessfulSync:    rc.LocalOnSuccessfulSync,
			ResourcesEquivalent: rc.LocalResourcesEquivalent,
			ShouldProcess:       rc.LocalShouldProcess,
			WaitForCacheSync:    rc.LocalWaitForCacheSync,
			Scheme:              config.Scheme,
			ResyncPeriod:        rc.LocalResyncPeriod,
			SyncCounter:         syncCounter,
		})

		if err != nil {
			return nil, err
		}

		brokerSyncer.syncers = append(brokerSyncer.syncers, localSyncer)
		brokerSyncer.localSyncers[reflect.TypeOf(rc.LocalResourceType)] = localSyncer

		waitForCacheSync := rc.BrokerWaitForCacheSync
		if waitForCacheSync == nil {
			f := false
			waitForCacheSync = &f
		}

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
			Transform:           rc.BrokerTransform,
			ResourcesEquivalent: rc.BrokerResourcesEquivalent,
			WaitForCacheSync:    waitForCacheSync,
			Scheme:              config.Scheme,
			ResyncPeriod:        rc.BrokerResyncPeriod,
			SyncCounter:         syncCounter,
		})

		if err != nil {
			return nil, err
		}

		brokerSyncer.syncers = append(brokerSyncer.syncers, remoteSyncer)
	}

	return brokerSyncer, nil
}

func createBrokerClient(config *SyncerConfig) error {
	var err error

	if config.BrokerRestConfig != nil {
		// We have an existing REST configuration, assume it’s correct (but check it anyway)
		config.BrokerClient, err = getCheckedBrokerClientset(config.BrokerRestConfig, config.ResourceConfigs[0], config.BrokerNamespace,
			config.RestMapper)
		if err != nil {
			return err
		}
	} else {
		var brokerRestConfig *rest.Config
		// We’ll build a REST configuration from the environment, checking whether we need to provide an explicit trust anchor
		brokerRestConfig, config.BrokerNamespace, err = BuildBrokerConfigFromEnv(false)
		if err != nil {
			return err
		}

		config.BrokerClient, err = getCheckedBrokerClientset(brokerRestConfig, config.ResourceConfigs[0], config.BrokerNamespace,
			config.RestMapper)
		if err != nil {
			// Certificate error, try with the trust chain
			brokerRestConfig, config.BrokerNamespace, err = BuildBrokerConfigFromEnv(true)
			if err != nil {
				return err
			}
			config.BrokerClient, err = getCheckedBrokerClientset(brokerRestConfig, config.ResourceConfigs[0], config.BrokerNamespace,
				config.RestMapper)
		}

		if err != nil {
			return err
		}
	}

	return nil
}

func getCheckedBrokerClientset(restConfig *rest.Config, rc ResourceConfig, brokerNamespace string,
	restMapper meta.RESTMapper) (dynamic.Interface, error) {
	client, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}
	// Try retrieving the resource
	_, gvr, err := util.ToUnstructuredResource(rc.BrokerResourceType, restMapper)
	if err != nil {
		return nil, err
	}

	resourceClient := client.Resource(*gvr).Namespace(brokerNamespace)
	_, err = resourceClient.Get("any", metav1.GetOptions{})

	if err != nil {
		if urlError, ok := err.(*url.Error); ok {
			if _, ok := urlError.Unwrap().(x509.UnknownAuthorityError); ok {
				return client, errors.Wrap(err, "cannot access the broker API server")
			}
		}

		if !apierrors.IsNotFound(err) {
			klog.Errorf("Error accessing the broker API server: %v", err)
		}
	}

	return client, nil
}

func (s *Syncer) Start(stopCh <-chan struct{}) error {
	for _, syncer := range s.syncers {
		err := syncer.Start(stopCh)
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *Syncer) GetBrokerFederator() federate.Federator {
	return s.remoteFederator
}

func (s *Syncer) GetLocalFederator() federate.Federator {
	return s.localFederator
}

func (s *Syncer) GetLocalResource(name, namespace string, ofType runtime.Object) (runtime.Object, bool, error) {
	ls, found := s.localSyncers[reflect.TypeOf(ofType)]
	if !found {
		return nil, false, fmt.Errorf("no Syncer found for %#v", ofType)
	}

	return ls.GetResource(name, namespace)
}

func (s *Syncer) ListLocalResources(ofType runtime.Object) ([]runtime.Object, error) {
	ls, found := s.localSyncers[reflect.TypeOf(ofType)]
	if !found {
		return nil, fmt.Errorf("no Syncer found for %#v", ofType)
	}

	return ls.ListResources()
}
