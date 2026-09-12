package v1

import (
	"github.com/pkg/errors"
	"google.golang.org/protobuf/proto"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func convertInstanceMessage(instance *store.InstanceMessage) *v1pb.Instance {
	engine := convertToEngine(instance.Metadata.GetEngine())
	dataSources := convertDataSources(instance.Metadata.GetDataSources())

	return &v1pb.Instance{
		Name:               common.FormatInstance(instance.ResourceID),
		Title:              instance.Metadata.GetTitle(),
		Engine:             engine,
		EngineVersion:      instance.Metadata.GetVersion(),
		ExternalLink:       instance.Metadata.GetExternalLink(),
		DataSources:        dataSources,
		State:              convertDeletedToState(instance.Deleted),
		Environment:        common.FormatEnvironment(instance.EnvironmentID),
		Activation:         instance.Metadata.GetActivation(),
		SyncInterval:       instance.Metadata.GetSyncInterval(),
		MaximumConnections: instance.Metadata.GetMaximumConnections(),
		SyncDatabases:      instance.Metadata.GetSyncDatabases(),
		LastSyncTime:       instance.Metadata.GetLastSyncTime(),
	}
}

func convertInstanceToInstanceMessage(instanceID string, instance *v1pb.Instance) (*store.InstanceMessage, error) {
	datasources, err := convertV1DataSources(instance.DataSources)
	if err != nil {
		return nil, err
	}
	environmentID, err := common.GetEnvironmentID(instance.Environment)
	if err != nil {
		return nil, err
	}

	return &store.InstanceMessage{
		ResourceID:    instanceID,
		EnvironmentID: environmentID,
		Metadata: &storepb.Instance{
			Title:              instance.GetTitle(),
			Engine:             convertEngine(instance.Engine),
			ExternalLink:       instance.GetExternalLink(),
			Activation:         instance.GetActivation(),
			DataSources:        datasources,
			SyncInterval:       instance.GetSyncInterval(),
			MaximumConnections: instance.GetMaximumConnections(),
			SyncDatabases:      instance.GetSyncDatabases(),
		},
	}, nil
}

func convertInstanceMessageToInstanceResource(instanceMessage *store.InstanceMessage) *v1pb.InstanceResource {
	instance := convertInstanceMessage(instanceMessage)
	return &v1pb.InstanceResource{
		Name:          instance.Name,
		Title:         instance.Title,
		Engine:        instance.Engine,
		EngineVersion: instance.EngineVersion,
		DataSources:   instance.DataSources,
		Activation:    instance.Activation,
		Environment:   instance.Environment,
	}
}

func convertV1DataSources(dataSources []*v1pb.DataSource) ([]*storepb.DataSource, error) {
	var values []*storepb.DataSource
	for _, ds := range dataSources {
		dataSource, err := convertV1DataSource(ds)
		if err != nil {
			return nil, err
		}
		values = append(values, dataSource)
	}

	return values, nil
}

func convertDataSources(dataSources []*storepb.DataSource) []*v1pb.DataSource {
	var v1DataSources []*v1pb.DataSource
	for _, ds := range dataSources {
		dataSourceType := v1pb.DataSourceType_DATA_SOURCE_UNSPECIFIED
		switch ds.GetType() {
		case storepb.DataSourceType_ADMIN:
			dataSourceType = v1pb.DataSourceType_ADMIN
		case storepb.DataSourceType_READ_ONLY:
			dataSourceType = v1pb.DataSourceType_READ_ONLY
		default:
		}

		v1DataSources = append(v1DataSources, &v1pb.DataSource{
			Id:       ds.GetId(),
			Type:     dataSourceType,
			Username: ds.GetUsername(),
			// We don't return the password and SSLs on reads.
			Host:                      ds.GetHost(),
			Port:                      ds.GetPort(),
			Database:                  ds.GetDatabase(),
			SshHost:                   ds.GetSshHost(),
			SshPort:                   ds.GetSshPort(),
			SshUser:                   ds.GetSshUser(),
			UseSsl:                    ds.GetUseSsl(),
			ExtraConnectionParameters: ds.GetExtraConnectionParameters(),
		})
	}

	return v1DataSources
}

func convertV1DataSource(dataSource *v1pb.DataSource) (*storepb.DataSource, error) {
	dsType, err := convertV1DataSourceType(dataSource.Type)
	if err != nil {
		return nil, err
	}

	return &storepb.DataSource{
		Id:                        dataSource.Id,
		Type:                      dsType,
		Username:                  dataSource.Username,
		Password:                  dataSource.Password,
		SslCa:                     dataSource.SslCa,
		SslCert:                   dataSource.SslCert,
		SslKey:                    dataSource.SslKey,
		Host:                      dataSource.Host,
		Port:                      dataSource.Port,
		Database:                  dataSource.Database,
		SshHost:                   dataSource.SshHost,
		SshPort:                   dataSource.SshPort,
		SshUser:                   dataSource.SshUser,
		SshPassword:               dataSource.SshPassword,
		SshPrivateKey:             dataSource.SshPrivateKey,
		UseSsl:                    dataSource.UseSsl,
		ExtraConnectionParameters: dataSource.ExtraConnectionParameters,
	}, nil
}

func convertV1DataSourceType(tp v1pb.DataSourceType) (storepb.DataSourceType, error) {
	switch tp {
	case v1pb.DataSourceType_READ_ONLY:
		return storepb.DataSourceType_READ_ONLY, nil
	case v1pb.DataSourceType_ADMIN:
		return storepb.DataSourceType_ADMIN, nil
	default:
		return storepb.DataSourceType_DATA_SOURCE_UNSPECIFIED, errors.Errorf("invalid data source type %v", tp)
	}
}

func mergeDataSources(stored, requested []*storepb.DataSource) []*storepb.DataSource {
	storedByID := make(map[string]*storepb.DataSource, len(stored))
	for _, ds := range stored {
		storedByID[ds.GetId()] = ds
	}
	merged := make([]*storepb.DataSource, 0, len(requested))
	for _, ds := range requested {
		existing, ok := storedByID[ds.GetId()]
		if !ok {
			merged = append(merged, ds)
			continue
		}
		merged = append(merged, mergeDataSource(existing, ds))
	}
	return merged
}

func mergeDataSource(stored, requested *storepb.DataSource) *storepb.DataSource {
	merged, ok := proto.Clone(stored).(*storepb.DataSource)
	if !ok {
		return requested
	}
	proto.Merge(merged, requested)
	return merged
}
