package broker

import (
	"crypto/x509"
	"fmt"
	"net/url"
	"reflect"

	"github.com/submariner-io/admiral/pkg/federate"
	"github.com/submariner-io/admiral/pkg/syncer"
	"github.com/submariner-io/admiral/pkg/util"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

type ResourceConfig struct {
	// SourceNamespace the namespace in the local source from which to retrieve the local resources to sync.
	LocalSourceNamespace string

	// LocalResourceType the type of the local resources to sync to the broker.
	LocalResourceType runtime.Object

	// LocalTransform function used to transform a local resource to the equivalent broker resource.
	LocalTransform syncer.TransformFunc

	// OnSuccessfulSync function invoked after a successful sync operation.
	LocalOnSuccessfulSync syncer.OnSuccessfulSyncFunc

	// LocalResourcesEquivalent function to compare two local resources for equivalence. See ResourceSyncerConfig.ResourcesEquivalent
	// for more details.
	LocalResourcesEquivalent syncer.ResourceEquivalenceFunc

	// BrokerResourceType the type of the broker resources to sync to the local source.
	BrokerResourceType runtime.Object

	// BrokerTransform function used to transform a broker resource to the equivalent local resource.
	BrokerTransform syncer.TransformFunc

	// BrokerResourcesEquivalent function to compare two broker resources for equivalence. See ResourceSyncerConfig.ResourcesEquivalent
	// for more details.
	BrokerResourcesEquivalent syncer.ResourceEquivalenceFunc
}

type SyncerConfig struct {
	// LocalRestConfig the REST config used to access the local resources to sync.
	LocalRestConfig *rest.Config

	// LocalNamespace the namespace in the local source to which resources from the broker will be synced.
	LocalNamespace string

	// LocalClusterID the ID of the local cluster. This is used to avoid loops when syncing the same resources between
	// the local and broker sources. If local resources are transformed to different broker resource types then
	// specify an empty LocalClusterID to disable this loop protection.
	LocalClusterID string

	// BrokerRestConfig the REST config used to access the broker resources to sync. If not specified, the config is
	// built from environment variables via BuildBrokerConfigFromEnv.
	BrokerRestConfig *rest.Config

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
	remoteFederator federate.Federator
}

// NewSyncer creates a Syncer that performs bi-directional syncing of resources between a local source and a central broker.
func NewSyncer(config SyncerConfig) (*Syncer, error) {
	if len(config.ResourceConfigs) == 0 {
		return nil, fmt.Errorf("no resources to sync")
	}

	restMapper, err := util.BuildRestMapper(config.LocalRestConfig)
	if err != nil {
		return nil, err
	}

	localClient, err := dynamic.NewForConfig(config.LocalRestConfig)
	if err != nil {
		return nil, fmt.Errorf("error creating dynamic client: %v", err)
	}

	var brokerClient dynamic.Interface
	if config.BrokerRestConfig != nil {
		// We have an existing REST configuration, assume it’s correct (but check it anyway)
		brokerClient, err = getCheckedBrokerClientset(config.BrokerRestConfig, config.ResourceConfigs[0], config.BrokerNamespace, restMapper)
		if err != nil {
			return nil, err
		}
	} else {
		var brokerRestConfig *rest.Config
		// We’ll build a REST configuration from the environment, checking whether we need to provide an explicit trust anchor
		brokerRestConfig, config.BrokerNamespace, err = BuildBrokerConfigFromEnv(false)
		if err != nil {
			return nil, err
		}
		brokerClient, err = getCheckedBrokerClientset(brokerRestConfig, config.ResourceConfigs[0], config.BrokerNamespace, restMapper)
		if err != nil {
			if urlError, ok := err.(*url.Error); ok {
				if _, ok := urlError.Unwrap().(x509.UnknownAuthorityError); ok {
					// Certificate error, try with the trust chain
					brokerRestConfig, config.BrokerNamespace, err = BuildBrokerConfigFromEnv(true)
					if err != nil {
						return nil, err
					}
					brokerClient, err = getCheckedBrokerClientset(brokerRestConfig, config.ResourceConfigs[0], config.BrokerNamespace, restMapper)
				}
			}
		}
		if err != nil {
			return nil, err
		}
	}

	return NewSyncerWithDetail(&config, localClient, brokerClient, restMapper)
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
	_, err = resourceClient.List(metav1.ListOptions{})

	return client, err
}

// NewSyncerWithDetail creates a Syncer with given additional detail. This function is intended for unit tests.
func NewSyncerWithDetail(config *SyncerConfig, localClient, brokerClient dynamic.Interface, restMapper meta.RESTMapper) (*Syncer, error) {
	brokerSyncer := &Syncer{
		syncers:      []syncer.Interface{},
		localSyncers: make(map[reflect.Type]syncer.Interface),
	}

	brokerSyncer.remoteFederator = NewFederator(brokerClient, restMapper, config.BrokerNamespace, config.LocalClusterID)
	localFederator := NewFederator(localClient, restMapper, config.LocalNamespace, "")

	for _, rc := range config.ResourceConfigs {
		localSyncer, err := syncer.NewResourceSyncer(&syncer.ResourceSyncerConfig{
			Name:                fmt.Sprintf("local -> broker for %T", rc.LocalResourceType),
			SourceClient:        localClient,
			SourceNamespace:     rc.LocalSourceNamespace,
			LocalClusterID:      config.LocalClusterID,
			Direction:           syncer.LocalToRemote,
			RestMapper:          restMapper,
			Federator:           brokerSyncer.remoteFederator,
			ResourceType:        rc.LocalResourceType,
			Transform:           rc.LocalTransform,
			OnSuccessfulSync:    rc.LocalOnSuccessfulSync,
			ResourcesEquivalent: rc.LocalResourcesEquivalent,
			Scheme:              config.Scheme,
		})

		if err != nil {
			return nil, err
		}

		brokerSyncer.syncers = append(brokerSyncer.syncers, localSyncer)
		brokerSyncer.localSyncers[reflect.TypeOf(rc.LocalResourceType)] = localSyncer

		remoteSyncer, err := syncer.NewResourceSyncer(&syncer.ResourceSyncerConfig{
			Name:                fmt.Sprintf("broker -> local for %T", rc.BrokerResourceType),
			SourceClient:        brokerClient,
			SourceNamespace:     config.BrokerNamespace,
			LocalClusterID:      config.LocalClusterID,
			Direction:           syncer.RemoteToLocal,
			RestMapper:          restMapper,
			Federator:           localFederator,
			ResourceType:        rc.BrokerResourceType,
			Transform:           rc.BrokerTransform,
			ResourcesEquivalent: rc.BrokerResourcesEquivalent,
			Scheme:              config.Scheme,
		})

		if err != nil {
			return nil, err
		}

		brokerSyncer.syncers = append(brokerSyncer.syncers, remoteSyncer)
	}

	return brokerSyncer, nil
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

func (s *Syncer) GetLocalResource(name, namespace string, ofType runtime.Object) (runtime.Object, bool, error) {
	ls, found := s.localSyncers[reflect.TypeOf(ofType)]
	if !found {
		return nil, false, fmt.Errorf("no Syncer found for %#v", ofType)
	}

	return ls.GetResource(name, namespace)
}
