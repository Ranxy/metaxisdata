package v1

import (
	"context"
	"fmt"
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

func (*InstanceService) checkInstanceDataSources(_ *store.InstanceMessage, dataSources []*storepb.DataSource) error {
	dsIDMap := map[string]bool{}
	adminCount := 0
	for _, ds := range dataSources {
		if ds.GetId() == "" {
			return connect.NewError(connect.CodeInvalidArgument, errors.New("data source id is required"))
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
			return nil, connect.NewError(connect.CodeInvalidArgument,
				errors.New("data_sources cannot be updated through UpdateInstance; use CreateDataSource, UpdateDataSource and DeleteDataSource"))
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
//
// Each instance is processed independently and its outcome is reported in a
// BatchSyncInstanceResult, so one bad instance does not hide the syncs that
// already happened before it. Only a malformed request (no instances) fails the
// whole call.
func (s *InstanceService) BatchSyncInstances(ctx context.Context, req *connect.Request[v1pb.BatchSyncInstancesRequest]) (*connect.Response[v1pb.BatchSyncInstancesResponse], error) {
	if len(req.Msg.GetRequests()) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("requests must not be empty"))
	}

	response := &v1pb.BatchSyncInstancesResponse{}
	for _, r := range req.Msg.GetRequests() {
		result := &v1pb.BatchSyncInstanceResult{Name: r.GetName()}

		instance, err := getInstanceMessage(ctx, s.store, r.GetName())
		if err != nil {
			result.Error = err.Error()
			response.Results = append(response.Results, result)
			continue
		}
		if instance.Deleted {
			result.Error = fmt.Sprintf("instance %q has been deleted", r.GetName())
			response.Results = append(response.Results, result)
			continue
		}

		updatedInstance, _, newDatabases, err := s.schemaSyncer.SyncInstance(ctx, instance)
		if err != nil {
			result.Error = err.Error()
			response.Results = append(response.Results, result)
			continue
		}
		for _, database := range newDatabases {
			result.Databases = append(result.Databases, database.DatabaseName)
		}
		if r.GetEnableFullSync() {
			// Sync all databases in the instance asynchronously.
			s.schemaSyncer.SyncAllDatabases(ctx, updatedInstance)
		} else {
			s.schemaSyncer.SyncDatabasesAsync(newDatabases)
		}
		response.Results = append(response.Results, result)
	}

	return connect.NewResponse(response), nil
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

// CreateDataSource adds a read-only data source to an instance.
func (s *InstanceService) CreateDataSource(ctx context.Context, req *connect.Request[v1pb.CreateDataSourceRequest]) (*connect.Response[v1pb.DataSource], error) {
	if req.Msg.DataSource == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("data_source is required"))
	}
	// We only support adding RO type data source to instance now, see more details in instance_service.proto.
	if req.Msg.DataSource.Type != v1pb.DataSourceType_READ_ONLY {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("only support creating read-only data source"))
	}
	if req.Msg.DataSource.GetName() != "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("data_source.name must be empty; set data_source_id instead"))
	}

	instance, err := getInstanceMessage(ctx, s.store, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	if instance.Deleted {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("instance %q has been deleted", req.Msg.GetParent()))
	}

	requested, ok := proto.Clone(req.Msg.DataSource).(*v1pb.DataSource)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to clone data source"))
	}
	if id := req.Msg.GetDataSourceId(); id != "" {
		if !common.IsValidResourceID(id) {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid data_source_id %q", id))
		}
		requested.Name = common.FormatDataSource(instance.ResourceID, id)
	}
	dataSource, err := convertV1DataSource(instance.ResourceID, requested)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	for _, ds := range instance.Metadata.GetDataSources() {
		if ds.GetId() == dataSource.GetId() {
			return nil, connect.NewError(connect.CodeAlreadyExists, errors.Errorf("data source %q already exists", dataSource.GetId()))
		}
	}

	if req.Msg.ValidateOnly {
		if err := s.pingDataSource(ctx, instance, dataSource); err != nil {
			return nil, err
		}
		return connect.NewResponse(convertDataSource(instance.ResourceID, dataSource)), nil
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

	return connect.NewResponse(convertDataSource(instance.ResourceID, dataSource)), nil
}

// UpdateDataSource updates one data source of an instance. Only the fields named
// in the update mask are written, so an update built from a read (which never
// returns credentials) keeps the stored secrets.
func (s *InstanceService) UpdateDataSource(ctx context.Context, req *connect.Request[v1pb.UpdateDataSourceRequest]) (*connect.Response[v1pb.DataSource], error) {
	if req.Msg.DataSource == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("data_source is required"))
	}
	if req.Msg.UpdateMask == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("update_mask must be set"))
	}

	instanceID, dataSourceID, err := common.GetInstanceDataSourceID(req.Msg.DataSource.GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	instance, err := getInstanceMessage(ctx, s.store, common.FormatInstance(instanceID))
	if err != nil {
		return nil, err
	}
	if instance.Deleted {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("instance %q has been deleted", common.FormatInstance(instanceID)))
	}

	metadata, ok := proto.Clone(instance.Metadata).(*storepb.Instance)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to convert instance metadata type"))
	}
	var dataSource *storepb.DataSource
	for _, ds := range metadata.GetDataSources() {
		if ds.GetId() == dataSourceID {
			dataSource = ds
			break
		}
	}
	if dataSource == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("data source %q not found", req.Msg.DataSource.GetName()))
	}

	if err := patchDataSource(dataSource, req.Msg.DataSource, req.Msg.UpdateMask.GetPaths()); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	if req.Msg.ValidateOnly {
		if err := s.pingDataSource(ctx, instance, dataSource); err != nil {
			return nil, err
		}
		return connect.NewResponse(convertDataSource(instance.ResourceID, dataSource)), nil
	}

	instance, err = s.store.UpdateInstance(ctx, &store.UpdateInstanceMessage{ResourceID: instance.ResourceID, Metadata: metadata})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(convertDataSource(instance.ResourceID, dataSource)), nil
}

// DeleteDataSource removes a read-only data source from an instance.
func (s *InstanceService) DeleteDataSource(ctx context.Context, req *connect.Request[v1pb.DeleteDataSourceRequest]) (*connect.Response[emptypb.Empty], error) {
	instanceID, dataSourceID, err := common.GetInstanceDataSourceID(req.Msg.GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	instance, err := getInstanceMessage(ctx, s.store, common.FormatInstance(instanceID))
	if err != nil {
		return nil, err
	}
	if instance.Deleted {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("instance %q has been deleted", common.FormatInstance(instanceID)))
	}

	metadata, ok := proto.Clone(instance.Metadata).(*storepb.Instance)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to convert instance metadata type"))
	}
	var updatedDataSources []*storepb.DataSource
	var dataSource *storepb.DataSource
	for _, ds := range instance.Metadata.GetDataSources() {
		if ds.GetId() == dataSourceID {
			dataSource = ds
		} else {
			updatedDataSources = append(updatedDataSources, ds)
		}
	}
	if dataSource == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("data source %q not found", req.Msg.GetName()))
	}
	// We only support removing RO type data source from an instance, see more details in instance_service.proto.
	if dataSource.GetType() != storepb.DataSourceType_READ_ONLY {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("only support deleting read-only data source"))
	}

	metadata.DataSources = updatedDataSources
	if _, err := s.store.UpdateInstance(ctx, &store.UpdateInstanceMessage{ResourceID: instance.ResourceID, Metadata: metadata}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// pingDataSource verifies that the server can actually connect with dataSource.
func (s *InstanceService) pingDataSource(ctx context.Context, instance *store.InstanceMessage, dataSource *storepb.DataSource) error {
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
