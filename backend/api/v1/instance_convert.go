package v1

import (
	"strings"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func convertInstanceMessage(instance *store.InstanceMessage) *v1pb.Instance {
	engine := convertToEngine(instance.Metadata.GetEngine())
	dataSources := convertDataSources(instance.ResourceID, instance.Metadata.GetDataSources())

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
	datasources, err := convertV1DataSources(instanceID, instance.DataSources)
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

func convertV1DataSources(instanceID string, dataSources []*v1pb.DataSource) ([]*storepb.DataSource, error) {
	var values []*storepb.DataSource
	for _, ds := range dataSources {
		dataSource, err := convertV1DataSource(instanceID, ds)
		if err != nil {
			return nil, err
		}
		values = append(values, dataSource)
	}

	return values, nil
}

func convertDataSources(instanceID string, dataSources []*storepb.DataSource) []*v1pb.DataSource {
	v1DataSources := make([]*v1pb.DataSource, 0, len(dataSources))
	for _, ds := range dataSources {
		v1DataSources = append(v1DataSources, convertDataSource(instanceID, ds))
	}

	return v1DataSources
}

func convertToDataSourceType(tp storepb.DataSourceType) v1pb.DataSourceType {
	switch tp {
	case storepb.DataSourceType_ADMIN:
		return v1pb.DataSourceType_ADMIN
	case storepb.DataSourceType_READ_ONLY:
		return v1pb.DataSourceType_READ_ONLY
	default:
		return v1pb.DataSourceType_DATA_SOURCE_UNSPECIFIED
	}
}

// convertDataSource converts one stored data source into its public shape.
func convertDataSource(instanceID string, dataSource *storepb.DataSource) *v1pb.DataSource {
	return &v1pb.DataSource{
		Name:                      common.FormatDataSource(instanceID, dataSource.GetId()),
		Type:                      convertToDataSourceType(dataSource.GetType()),
		Username:                  dataSource.GetUsername(),
		Host:                      dataSource.GetHost(),
		Port:                      dataSource.GetPort(),
		Database:                  dataSource.GetDatabase(),
		SshHost:                   dataSource.GetSshHost(),
		SshPort:                   dataSource.GetSshPort(),
		SshUser:                   dataSource.GetSshUser(),
		UseSsl:                    dataSource.GetUseSsl(),
		ExtraConnectionParameters: dataSource.GetExtraConnectionParameters(),
	}
}

// dataSourceID returns the store ID of a data source. A resource name must
// belong to instanceID; an empty name asks the server to generate an ID.
func dataSourceID(instanceID, name string) (string, error) {
	if name == "" {
		generated, err := common.RandomString(8)
		if err != nil {
			return "", errors.Wrap(err, "failed to generate a data source ID")
		}
		return strings.ToLower(generated), nil
	}
	owner, id, err := common.GetInstanceDataSourceID(name)
	if err != nil {
		return "", err
	}
	if owner != instanceID {
		return "", errors.Errorf("data source %q does not belong to instance %q", name, instanceID)
	}
	if !common.IsValidResourceID(id) {
		return "", errors.Errorf("invalid data source ID %q", id)
	}
	return id, nil
}

func convertV1DataSource(instanceID string, dataSource *v1pb.DataSource) (*storepb.DataSource, error) {
	dsType, err := convertV1DataSourceType(dataSource.Type)
	if err != nil {
		return nil, err
	}
	id, err := dataSourceID(instanceID, dataSource.GetName())
	if err != nil {
		return nil, err
	}

	return &storepb.DataSource{
		Id:                        id,
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

// patchDataSource applies the fields named in paths from requested onto the
// stored data source. Fields outside the mask keep their stored value: a request
// built from a read never carries credentials, and an empty password must not
// silently overwrite one.
func patchDataSource(stored *storepb.DataSource, requested *v1pb.DataSource, paths []string) error {
	for _, path := range paths {
		switch path {
		case "username":
			stored.Username = requested.GetUsername()
		case "password":
			stored.Password = requested.GetPassword()
		case "ssl_ca":
			stored.SslCa = requested.GetSslCa()
		case "ssl_cert":
			stored.SslCert = requested.GetSslCert()
		case "ssl_key":
			stored.SslKey = requested.GetSslKey()
		case "host":
			stored.Host = requested.GetHost()
		case "port":
			stored.Port = requested.GetPort()
		case "database":
			stored.Database = requested.GetDatabase()
		case "ssh_host":
			stored.SshHost = requested.GetSshHost()
		case "ssh_port":
			stored.SshPort = requested.GetSshPort()
		case "ssh_user":
			stored.SshUser = requested.GetSshUser()
		case "ssh_password":
			stored.SshPassword = requested.GetSshPassword()
		case "ssh_private_key":
			stored.SshPrivateKey = requested.GetSshPrivateKey()
		case "use_ssl":
			stored.UseSsl = requested.GetUseSsl()
		case "extra_connection_parameters":
			stored.ExtraConnectionParameters = requested.GetExtraConnectionParameters()
		default:
			return errors.Errorf(`unsupported update_mask %q`, path)
		}
	}
	return nil
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
