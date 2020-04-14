package broker

import (
	"fmt"
	"reflect"

	"github.com/submariner-io/admiral/pkg/federate"
	"github.com/submariner-io/admiral/pkg/syncer"
	"github.com/submariner-io/admiral/pkg/util"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

type ResourceConfig struct {
	// LocalResourceType the type of the local resource to sync to the broker.
	LocalResourceType runtime.Object

	// LocalTransform function used to transform a local resource to the equivalent broker resource.
	LocalTransform syncer.TransformFunc

	// BrokerResourceType the type of the broker resource to sync locally.
	BrokerResourceType runtime.Object

	// BrokerTransform function used to transform a broker resource to the equivalent local resource.
	BrokerTransform syncer.TransformFunc
}

type SyncerConfig struct {
	// LocalRestConfig the REST config used to access the local resources to sync.
	LocalRestConfig *rest.Config

	// LocalNamespace the namespace of the local resources to sync.
	LocalNamespace string

	// LocalClusterID the ID of the local cluster.
	LocalClusterID string

	// BrokerRestConfig the REST config used to access the broker resources to sync. If not specified, the config is
	// built from environment variables via BuildBrokerConfigFromEnv.
	BrokerRestConfig *rest.Config

	// BrokerNamespace the namespace of the broker resources to sync. If not specified, the namespace is obtained from
	// an environment variable via BuildBrokerConfigFromEnv.
	BrokerNamespace string

	// ResourceConfigs the configurations for resources to sync
	ResourceConfigs []ResourceConfig
}

type Syncer struct {
	syncers    []syncer.Interface
	federators map[reflect.Type]federate.Federator
}

// NewSyncer creates a Syncer that performs 2-way syncing of resources between a local source and a central broker.
func NewSyncer(config SyncerConfig) (*Syncer, error) {
	restMapper, err := util.BuildRestMapper(config.LocalRestConfig)
	if err != nil {
		return nil, err
	}

	localClient, err := dynamic.NewForConfig(config.LocalRestConfig)
	if err != nil {
		return nil, fmt.Errorf("error creating dynamic client: %v", err)
	}

	brokerRestConfig := config.BrokerRestConfig
	if brokerRestConfig == nil {
		brokerRestConfig, config.BrokerNamespace, err = BuildBrokerConfigFromEnv()
		if err != nil {
			return nil, err
		}
	}

	brokerClient, err := dynamic.NewForConfig(brokerRestConfig)
	if err != nil {
		return nil, fmt.Errorf("error creating dynamic client: %v", err)
	}

	return newSyncer(&config, localClient, brokerClient, restMapper)
}

func newSyncer(config *SyncerConfig, localClient, brokerClient dynamic.Interface, restMapper meta.RESTMapper) (*Syncer, error) {
	brokerSyncer := &Syncer{
		syncers:    []syncer.Interface{},
		federators: map[reflect.Type]federate.Federator{},
	}

	for _, rc := range config.ResourceConfigs {
		remoteFederator := NewFederator(brokerClient, restMapper, config.BrokerNamespace, config.LocalClusterID)
		localSyncer, err := syncer.NewResourceSyncer(&syncer.ResourceSyncerConfig{
			Name:            fmt.Sprintf("local -> broker for %T", rc.LocalResourceType),
			SourceClient:    localClient,
			SourceNamespace: config.LocalNamespace,
			LocalClusterID:  config.LocalClusterID,
			Direction:       syncer.LocalToRemote,
			RestMapper:      restMapper,
			Federator:       remoteFederator,
			ResourceType:    rc.LocalResourceType,
			Transform:       rc.LocalTransform,
		})

		if err != nil {
			return nil, err
		}

		brokerSyncer.syncers = append(brokerSyncer.syncers, localSyncer)
		brokerSyncer.federators[reflect.TypeOf(rc.LocalResourceType)] = remoteFederator

		localFederator := NewFederator(localClient, restMapper, config.LocalNamespace, "")
		remoteSyncer, err := syncer.NewResourceSyncer(&syncer.ResourceSyncerConfig{
			Name:            fmt.Sprintf("broker -> local for %T", rc.BrokerResourceType),
			SourceClient:    brokerClient,
			SourceNamespace: config.BrokerNamespace,
			LocalClusterID:  config.LocalClusterID,
			Direction:       syncer.RemoteToLocal,
			RestMapper:      restMapper,
			Federator:       localFederator,
			ResourceType:    rc.BrokerResourceType,
			Transform:       rc.BrokerTransform,
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

func (s *Syncer) GetBrokerFederatorFor(resourceType runtime.Object) (federate.Federator, error) {
	f, found := s.federators[reflect.TypeOf(resourceType)]
	if !found {
		return nil, fmt.Errorf("no Federator found for type %T", resourceType)
	}

	return f, nil
}
