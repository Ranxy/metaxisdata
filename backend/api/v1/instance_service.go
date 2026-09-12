package v1

import (
	"context"
	"log/slog"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/dbfactory"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"
	"github.com/Ranxy/metaxisdata/backend/runner/schemasync"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// InstanceService implements the instance service.
type InstanceService struct {
	v1connect.UnimplementedInstanceServiceHandler
	store        *store.Store
	dbFactory    *dbfactory.DBFactory
	schemaSyncer *schemasync.Syncer
}

// NewInstanceService creates a new InstanceService.
func NewInstanceService(store *store.Store, dbFactory *dbfactory.DBFactory, schemaSyncer *schemasync.Syncer) *InstanceService {
	return &InstanceService{
		store:        store,
		dbFactory:    dbFactory,
		schemaSyncer: schemaSyncer,
	}
}

// GetInstance gets an instance.
func (s *InstanceService) GetInstance(ctx context.Context, req *connect.Request[v1pb.GetInstanceRequest]) (*connect.Response[v1pb.Instance], error) {
	instance, err := getInstanceMessage(ctx, s.store, req.Msg.Name)
	if err != nil {
		return nil, err
	}
	result := convertInstanceMessage(instance)
	return connect.NewResponse(result), nil
}

// ListInstances lists all instances.
func (s *InstanceService) ListInstances(ctx context.Context, req *connect.Request[v1pb.ListInstancesRequest]) (*connect.Response[v1pb.ListInstancesResponse], error) {
	offset, err := parseLimitAndOffset(&pageSize{
		token:   req.Msg.PageToken,
		limit:   int(req.Msg.PageSize),
		maximum: 1000,
	})
	if err != nil {
		return nil, err
	}
	limitPlusOne := offset.limit + 1

	find := &store.FindInstanceMessage{
		ShowDeleted: req.Msg.ShowDeleted,
		Limit:       &limitPlusOne,
		Offset:      &offset.offset,
	}
	filter, err := parseListInstanceFilter(req.Msg.Filter)
	if err != nil {
		return nil, err
	}
	find.Filter = filter
	instances, err := s.store.ListInstances(ctx, find)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	instances, nextPageToken, err := paginate(instances, offset)
	if err != nil {
		return nil, err
	}

	response := &v1pb.ListInstancesResponse{
		NextPageToken: nextPageToken,
	}
	for _, instance := range instances {
		ins := convertInstanceMessage(instance)
		response.Instances = append(response.Instances, ins)
	}
	return connect.NewResponse(response), nil
}

// ListInstanceDatabase list all databases in the instance.
// CreateInstance creates an instance.
func (s *InstanceService) CreateInstance(ctx context.Context, req *connect.Request[v1pb.CreateInstanceRequest]) (*connect.Response[v1pb.Instance], error) {
	if req.Msg.Instance == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("instance must be set"))
	}
	if !isValidResourceID(req.Msg.InstanceId) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid instance ID %v", req.Msg.InstanceId))
	}

	instanceMessage, err := convertInstanceToInstanceMessage(req.Msg.InstanceId, req.Msg.Instance)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	// Test connection.
	if req.Msg.ValidateOnly {
		for _, ds := range instanceMessage.Metadata.GetDataSources() {
			err := func() error {
				driver, err := s.dbFactory.GetDataSourceDriver(
					ctx, instanceMessage, ds,
					db.ConnectionContext{
						ReadOnly: ds.GetType() == storepb.DataSourceType_READ_ONLY,
					},
				)
				if err != nil {
					return connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to get database driver"))
				}
				defer driver.Close(ctx)
				if err := driver.Ping(ctx); err != nil {
					return connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid datasource %s", ds.GetType()))
				}
				return nil
			}()
			if err != nil {
				return nil, err
			}
		}

		result := convertInstanceMessage(instanceMessage)
		return connect.NewResponse(result), nil
	}

	if err := s.checkInstanceDataSources(instanceMessage, instanceMessage.Metadata.GetDataSources()); err != nil {
		return nil, err
	}

	instance, err := s.store.CreateInstance(ctx, instanceMessage)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	driver, err := s.dbFactory.GetAdminDatabaseDriver(ctx, instance, nil /* database */, db.ConnectionContext{})
	if err == nil {
		defer driver.Close(ctx)
		updatedInstance, _, _, err := s.schemaSyncer.SyncInstance(ctx, instance)
		if err != nil {
			slog.Warn("Failed to sync instance",
				slog.String("instance", instance.ResourceID),
				log.WithError(err))
		} else {
			instance = updatedInstance
		}
		// Sync all databases in the instance asynchronously.
		s.schemaSyncer.SyncAllDatabases(ctx, instance)
	}

	result := convertInstanceMessage(instance)
	return connect.NewResponse(result), nil
}

func (s *InstanceService) checkInstanceDataSources(instance *store.InstanceMessage, dataSources []*storepb.DataSource) error {
	dsIDMap := map[string]bool{}
	adminCount := 0
	for _, ds := range dataSources {
		if err := s.checkDataSource(instance, ds); err != nil {
			return err
		}
		if dsIDMap[ds.GetId()] {
			return connect.NewError(connect.CodeInvalidArgument, errors.Errorf(`duplicate data source id "%s"`, ds.GetId()))
		}
		dsIDMap[ds.GetId()] = true
		if ds.GetType() == storepb.DataSourceType_ADMIN {
			adminCount++
		}
	}
	// The instance is unusable without exactly one admin data source; without
	// this the update path could persist zero or several of them.
	if adminCount != 1 {
		return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("require exactly one admin data source, got %d", adminCount))
	}

	return nil
}

// mergeDataSources keys the requested data source list, which is authoritative
// for membership, by ID and edits each entry in place of the stored one with
// the same ID. IDs absent from the request are removed, matching the
// update_mask semantics for a repeated field.

// mergeDataSource overlays the fields a request carries onto the stored data
// source. proto.Merge copies only populated proto3 scalars, so a field the
// request omits — notably every credential, which reads never return — keeps
// its stored value. Repeated fields are appended rather than overwritten, so
// one the request does carry is cleared first.

func (*InstanceService) checkDataSource(_ *store.InstanceMessage, dataSource *storepb.DataSource) error {
	if dataSource.GetId() == "" {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("data source id is required"))
	}

	return nil
}

// UpdateInstance updates an instance.
func (s *InstanceService) UpdateInstance(ctx context.Context, req *connect.Request[v1pb.UpdateInstanceRequest]) (*connect.Response[v1pb.Instance], error) {
	if req.Msg.Instance == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("instance must be set"))
	}
	if req.Msg.UpdateMask == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("update_mask must be set"))
	}

	instance, err := getInstanceMessage(ctx, s.store, req.Msg.Instance.Name)
	if err != nil {
		return nil, err
	}
	if instance.Deleted {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("instance %q has been deleted", req.Msg.Instance.Name))
	}

	metadata, ok := proto.Clone(instance.Metadata).(*storepb.Instance)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to convert instance metadata type"))
	}
	patch := &store.UpdateInstanceMessage{
		ResourceID: instance.ResourceID,
		Metadata:   metadata,
	}
	for _, path := range req.Msg.UpdateMask.Paths {
		switch path {
		case "title":
			patch.Metadata.Title = req.Msg.Instance.Title
		case "environment":
			environmentID, err := common.GetEnvironmentID(req.Msg.Instance.Environment)
			if err != nil {
				return nil, connect.NewError(connect.CodeInvalidArgument, err)
			}
			environment, err := s.store.GetEnvironmentByID(ctx, environmentID)
			if err != nil {
				return nil, connect.NewError(connect.CodeInternal, err)
			}
			if environment == nil {
				return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("environment %q not found", environmentID))
			}
			patch.EnvironmentID = &environmentID
		case "external_link":
			patch.Metadata.ExternalLink = req.Msg.Instance.ExternalLink
		case "data_sources":
			dataSources, err := convertV1DataSources(req.Msg.Instance.DataSources)
			if err != nil {
				return nil, connect.NewError(connect.CodeInvalidArgument, err)
			}
			// A data source list built from a Get response carries no credentials
			// and no store-only fields, so merge every entry into the stored one
			// with the same ID instead of overwriting it.
			dataSources = mergeDataSources(instance.Metadata.GetDataSources(), dataSources)
			if err := s.checkInstanceDataSources(instance, dataSources); err != nil {
				return nil, err
			}
			patch.Metadata.DataSources = dataSources
		case "activation":
			patch.Metadata.Activation = req.Msg.Instance.Activation
		case "sync_interval":
			patch.Metadata.SyncInterval = req.Msg.Instance.SyncInterval
		case "maximum_connections":
			patch.Metadata.MaximumConnections = req.Msg.Instance.MaximumConnections
		case "sync_databases":
			patch.Metadata.SyncDatabases = req.Msg.Instance.SyncDatabases
		default:
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf(`unsupported update_mask "%s"`, path))
		}
	}

	ins, err := s.store.UpdateInstance(ctx, patch)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	result := convertInstanceMessage(ins)
	return connect.NewResponse(result), nil
}

// DeleteInstance deletes an instance.
func (s *InstanceService) DeleteInstance(ctx context.Context, req *connect.Request[v1pb.DeleteInstanceRequest]) (*connect.Response[emptypb.Empty], error) {
	instance, err := getInstanceMessage(ctx, s.store, req.Msg.Name)
	if err != nil {
		return nil, err
	}
	if instance.Deleted {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("instance %q has been deleted", req.Msg.Name))
	}

	databases, err := s.store.ListDatabases(ctx, &store.FindDatabaseMessage{InstanceID: &instance.ResourceID})
	if err != nil {
		return nil, err
	}
	if req.Msg.Force {
		if len(databases) > 0 {
			defaultProjectID := common.DefaultProjectID
			if _, err := s.store.BatchUpdateDatabases(ctx, databases, &store.BatchUpdateDatabases{ProjectID: &defaultProjectID}); err != nil {
				return nil, connect.NewError(connect.CodeInternal, err)
			}
		}
	} else {
		var databaseNames []string
		for _, database := range databases {
			if database.ProjectID != common.DefaultProjectID {
				databaseNames = append(databaseNames, database.DatabaseName)
			}
		}
		if len(databaseNames) > 0 {
			return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("all databases should be transferred to the unassigned project before deleting the instance"))
		}
	}

	metadata, ok := proto.Clone(instance.Metadata).(*storepb.Instance)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to convert instance metadata type"))
	}
	metadata.Activation = false
	if _, err := s.store.UpdateInstance(ctx, &store.UpdateInstanceMessage{
		ResourceID: instance.ResourceID,
		Deleted:    &deletePatch,
		Metadata:   metadata,
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// UndeleteInstance undeletes an instance.
func (s *InstanceService) UndeleteInstance(ctx context.Context, req *connect.Request[v1pb.UndeleteInstanceRequest]) (*connect.Response[v1pb.Instance], error) {
	instance, err := getInstanceMessage(ctx, s.store, req.Msg.Name)
	if err != nil {
		return nil, err
	}
	if !instance.Deleted {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("instance %q is active", req.Msg.Name))
	}

	ins, err := s.store.UpdateInstance(ctx, &store.UpdateInstanceMessage{
		ResourceID: instance.ResourceID,
		Deleted:    &undeletePatch,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	result := convertInstanceMessage(ins)
	return connect.NewResponse(result), nil
}

// SyncInstance syncs the instance.
func (s *InstanceService) SyncInstance(ctx context.Context, req *connect.Request[v1pb.SyncInstanceRequest]) (*connect.Response[v1pb.SyncInstanceResponse], error) {
	instance, err := getInstanceMessage(ctx, s.store, req.Msg.Name)
	if err != nil {
		return nil, err
	}
	if instance.Deleted {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("instance %q has been deleted", req.Msg.Name))
	}

	updatedInstance, allDatabases, newDatabases, err := s.schemaSyncer.SyncInstance(ctx, instance)
	if err != nil {
		return nil, err
	}
	if req.Msg.EnableFullSync {
		// Sync all databases in the instance asynchronously.
		s.schemaSyncer.SyncAllDatabases(ctx, updatedInstance)
	} else {
		s.schemaSyncer.SyncDatabasesAsync(newDatabases)
	}

	response := &v1pb.SyncInstanceResponse{}
	for _, database := range allDatabases {
		response.Databases = append(response.Databases, database.Name)
	}
	return connect.NewResponse(response), nil
}

// BatchSyncInstances syncs multiple instances.
func (s *InstanceService) BatchSyncInstances(ctx context.Context, req *connect.Request[v1pb.BatchSyncInstancesRequest]) (*connect.Response[v1pb.BatchSyncInstancesResponse], error) {
	for _, r := range req.Msg.Requests {
		instance, err := getInstanceMessage(ctx, s.store, r.Name)
		if err != nil {
			return nil, err
		}
		if instance.Deleted {
			return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("instance %q has been deleted", r.Name))
		}

		updatedInstance, _, newDatabases, err := s.schemaSyncer.SyncInstance(ctx, instance)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to sync instance"))
		}
		if r.EnableFullSync {
			// Sync all databases in the instance asynchronously.
			s.schemaSyncer.SyncAllDatabases(ctx, updatedInstance)
		} else {
			s.schemaSyncer.SyncDatabasesAsync(newDatabases)
		}
	}

	return connect.NewResponse(&v1pb.BatchSyncInstancesResponse{}), nil
}

// BatchUpdateInstances update multiple instances.
func (s *InstanceService) BatchUpdateInstances(ctx context.Context, req *connect.Request[v1pb.BatchUpdateInstancesRequest]) (*connect.Response[v1pb.BatchUpdateInstancesResponse], error) {
	response := &v1pb.BatchUpdateInstancesResponse{}
	for _, updateReq := range req.Msg.GetRequests() {
		updated, err := s.UpdateInstance(ctx, connect.NewRequest(updateReq))
		if err != nil {
			return nil, err
		}
		response.Instances = append(response.Instances, updated.Msg)
	}
	return connect.NewResponse(response), nil
}

// AddDataSource adds a data source to an instance.
func (s *InstanceService) AddDataSource(ctx context.Context, req *connect.Request[v1pb.AddDataSourceRequest]) (*connect.Response[v1pb.Instance], error) {
	if req.Msg.DataSource == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("data sources is required"))
	}
	// We only support add RO type datasouce to instance now, see more details in instance_service.proto.
	if req.Msg.DataSource.Type != v1pb.DataSourceType_READ_ONLY {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("only support adding read-only data source"))
	}

	dataSource, err := convertV1DataSource(req.Msg.DataSource)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("failed to convert data source"))
	}

	instance, err := getInstanceMessage(ctx, s.store, req.Msg.Name)
	if err != nil {
		return nil, err
	}
	if instance.Deleted {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("instance %q has been deleted", req.Msg.Name))
	}
	for _, ds := range instance.Metadata.GetDataSources() {
		if ds.GetId() == req.Msg.DataSource.Id {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("data source already exists with the same name"))
		}
	}
	if err := s.checkDataSource(instance, dataSource); err != nil {
		return nil, err
	}

	// Test connection.
	if req.Msg.ValidateOnly {
		err := func() error {
			driver, err := s.dbFactory.GetDataSourceDriver(
				ctx, instance, dataSource,
				db.ConnectionContext{
					ReadOnly: dataSource.GetType() == storepb.DataSourceType_READ_ONLY,
				},
			)
			if err != nil {
				return connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to get database driver"))
			}
			defer driver.Close(ctx)
			if err := driver.Ping(ctx); err != nil {
				return connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid datasource %s", dataSource.GetType()))
			}
			return nil
		}()
		if err != nil {
			return nil, err
		}
		result := convertInstanceMessage(instance)
		return connect.NewResponse(result), nil
	}

	if dataSource.GetType() != storepb.DataSourceType_READ_ONLY {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("only read-only data source can be added"))
	}
	metadata, ok := proto.Clone(instance.Metadata).(*storepb.Instance)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to convert instance metadata type"))
	}
	metadata.DataSources = append(metadata.DataSources, dataSource)
	instance, err = s.store.UpdateInstance(ctx, &store.UpdateInstanceMessage{ResourceID: instance.ResourceID, Metadata: metadata})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	result := convertInstanceMessage(instance)
	return connect.NewResponse(result), nil
}

// UpdateDataSource updates a data source of an instance.
func (s *InstanceService) UpdateDataSource(ctx context.Context, req *connect.Request[v1pb.UpdateDataSourceRequest]) (*connect.Response[v1pb.Instance], error) {
	if req.Msg.DataSource == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("datasource is required"))
	}
	if req.Msg.UpdateMask == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("update_mask must be set"))
	}

	instance, err := getInstanceMessage(ctx, s.store, req.Msg.Name)
	if err != nil {
		return nil, err
	}
	if instance.Deleted {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("instance %q has been deleted", req.Msg.Name))
	}
	metadata, ok := proto.Clone(instance.Metadata).(*storepb.Instance)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to convert instance metadata type"))
	}
	var dataSource *storepb.DataSource
	for _, ds := range metadata.GetDataSources() {
		if ds.GetId() == req.Msg.DataSource.Id {
			dataSource = ds
			break
		}
	}
	if dataSource == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf(`cannot found data source "%s"`, req.Msg.DataSource.Id))
	}

	for _, path := range req.Msg.UpdateMask.Paths {
		switch path {
		case "username":
			dataSource.Username = req.Msg.DataSource.Username
		case "password":
			dataSource.Password = req.Msg.DataSource.Password
		case "ssl_ca":
			dataSource.SslCa = req.Msg.DataSource.SslCa
		case "ssl_cert":
			dataSource.SslCert = req.Msg.DataSource.SslCert
		case "ssl_key":
			dataSource.SslKey = req.Msg.DataSource.SslKey
		case "host":
			dataSource.Host = req.Msg.DataSource.Host
		case "port":
			dataSource.Port = req.Msg.DataSource.Port
		case "database":
			dataSource.Database = req.Msg.DataSource.Database
		case "ssh_host":
			dataSource.SshHost = req.Msg.DataSource.SshHost
		case "ssh_port":
			dataSource.SshPort = req.Msg.DataSource.SshPort
		case "ssh_user":
			dataSource.SshUser = req.Msg.DataSource.SshUser
		case "ssh_password":
			dataSource.SshPassword = req.Msg.DataSource.SshPassword
		case "ssh_private_key":
			dataSource.SshPrivateKey = req.Msg.DataSource.SshPrivateKey
		case "use_ssl":
			dataSource.UseSsl = req.Msg.DataSource.UseSsl
		case "extra_connection_parameters":
			dataSource.ExtraConnectionParameters = req.Msg.DataSource.ExtraConnectionParameters
		default:
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf(`unsupported update_mask "%s"`, path))
		}
	}

	if err := s.checkDataSource(instance, dataSource); err != nil {
		return nil, err
	}

	// Test connection.
	if req.Msg.ValidateOnly {
		err := func() error {
			driver, err := s.dbFactory.GetDataSourceDriver(
				ctx, instance, dataSource,
				db.ConnectionContext{ReadOnly: dataSource.GetType() == storepb.DataSourceType_READ_ONLY},
			)
			if err != nil {
				return connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to get database driver"))
			}
			defer driver.Close(ctx)
			if err := driver.Ping(ctx); err != nil {
				return connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid datasource %s", dataSource.GetType()))
			}
			return nil
		}()
		if err != nil {
			return nil, err
		}
		result := convertInstanceMessage(instance)
		return connect.NewResponse(result), nil
	}

	instance, err = s.store.UpdateInstance(ctx, &store.UpdateInstanceMessage{ResourceID: instance.ResourceID, Metadata: metadata})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	result := convertInstanceMessage(instance)
	return connect.NewResponse(result), nil
}

// RemoveDataSource removes a data source to an instance.
func (s *InstanceService) RemoveDataSource(ctx context.Context, req *connect.Request[v1pb.RemoveDataSourceRequest]) (*connect.Response[v1pb.Instance], error) {
	if req.Msg.DataSource == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("data sources is required"))
	}

	instance, err := getInstanceMessage(ctx, s.store, req.Msg.Name)
	if err != nil {
		return nil, err
	}
	if instance.Deleted {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("instance %q has been deleted", req.Msg.Name))
	}

	metadata, ok := proto.Clone(instance.Metadata).(*storepb.Instance)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to convert instance metadata type"))
	}
	var updatedDataSources []*storepb.DataSource
	var dataSource *storepb.DataSource
	for _, ds := range instance.Metadata.GetDataSources() {
		if ds.GetId() == req.Msg.DataSource.Id {
			dataSource = ds
		} else {
			updatedDataSources = append(updatedDataSources, ds)
		}
	}
	if dataSource == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("data source not found"))
	}

	// We only support remove RO type datasource to instance now, see more details in instance_service.proto.
	if dataSource.GetType() != storepb.DataSourceType_READ_ONLY {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("only support remove read-only data source"))
	}

	metadata.DataSources = updatedDataSources
	instance, err = s.store.UpdateInstance(ctx, &store.UpdateInstanceMessage{ResourceID: instance.ResourceID, Metadata: metadata})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	instance, err = s.store.GetInstance(ctx, &store.FindInstanceMessage{
		ResourceID: &instance.ResourceID,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	result := convertInstanceMessage(instance)
	return connect.NewResponse(result), nil
}

func getInstanceMessage(ctx context.Context, stores *store.Store, name string) (*store.InstanceMessage, error) {
	instanceID, err := common.GetInstanceID(name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	find := &store.FindInstanceMessage{
		ResourceID: &instanceID,
	}
	instance, err := stores.GetInstance(ctx, find)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if instance == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("instance %q not found", name))
	}

	return instance, nil
}
