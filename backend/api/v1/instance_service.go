package v1

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/dbfactory"
	"github.com/Ranxy/metaxisdata/backend/component/state"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"
	"github.com/Ranxy/metaxisdata/backend/runner/schemasync"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// maxBatchInstances bounds the two instance batch RPCs, matching the limit the
// proto documents.
const maxBatchInstances = 1000

// batchSyncBudget bounds the wall-clock time one BatchSyncInstances call may spend
// syncing. Each SyncInstance keeps its own 15-minute deadline, but a batch of up
// to maxBatchInstances would otherwise hold the request open for hours when the
// targets hang. When the budget is exhausted the remaining instances are reported
// as failed instead of starting another sync.
const batchSyncBudget = 5 * time.Minute

// dataSourcePingTimeout bounds one test connection. The RPC carries no deadline
// of its own, and the connection holds one of the instance's connection slots
// while it runs, so a target that accepts the connection and then stalls must
// not be able to keep that slot until the client gives up.
const dataSourcePingTimeout = 60 * time.Second

// dataSourceConnectionWait bounds how long a test connection waits for one of
// the instance's connection slots. A person is waiting on the result, so it gets
// the interactive class and a short queue; when even that is not enough the
// action reports backpressure instead of opening an unbudgeted connection.
const dataSourceConnectionWait = 5 * time.Second

// InstanceService implements the instance service.
type InstanceService struct {
	v1connect.UnimplementedInstanceServiceHandler
	store        *store.Store
	dbFactory    *dbfactory.DBFactory
	schemaSyncer *schemasync.Syncer
	stateCfg     *state.State
}

// NewInstanceService creates a new InstanceService.
func NewInstanceService(store *store.Store, dbFactory *dbfactory.DBFactory, schemaSyncer *schemasync.Syncer, stateCfg *state.State) *InstanceService {
	return &InstanceService{
		store:        store,
		dbFactory:    dbFactory,
		schemaSyncer: schemaSyncer,
		stateCfg:     stateCfg,
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
		token:        req.Msg.PageToken,
		limit:        int(req.Msg.PageSize),
		maximum:      1000,
		defaultLimit: defaultPageLimit,
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

	// The engine and the environment are not validated by conversion: the enum
	// carries reserved values and the environment only has to exist.
	if !isSupportedStoreEngine(instanceMessage.Metadata.GetEngine()) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unsupported engine %s", instanceMessage.Metadata.GetEngine().String()))
	}
	if instanceMessage.EnvironmentID != "" {
		environment, err := s.store.GetEnvironmentByID(ctx, instanceMessage.EnvironmentID)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		if environment == nil {
			return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("environment %q not found", instanceMessage.EnvironmentID))
		}
	}

	// Test connection.
	if req.Msg.ValidateOnly {
		for _, ds := range instanceMessage.Metadata.GetDataSources() {
			if err := s.pingDataSource(ctx, instanceMessage, ds); err != nil {
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

	// The initial discovery is best-effort: SyncInstance opens the one admin
	// driver, counted by the per-instance limiter. Opening one here only to test
	// reachability would hold a second, unmetered connection for the whole sync,
	// which is exactly what the limit exists to bound.
	// The initial discovery and the databases it queues are one operation: the
	// creator is the one waiting for the instance to be usable.
	operation := s.schemaSyncer.StartOperation(storepb.SyncTrigger_SYNC_TRIGGER_MANUAL, currentUserID(ctx), instance)
	// The operation reports the instance step and then waits for its databases;
	// if this handler returns before either happened, the defer turns the operation
	// into an error message instead of leaving it to expire as a quiet success. It
	// is a no-op on every normal path.
	defer s.schemaSyncer.AbandonOperation(ctx, operation, schemasync.ErrOperationAbandoned)
	updatedInstance, _, _, syncErr := s.schemaSyncer.SyncInstance(ctx, instance)
	s.schemaSyncer.RecordInstanceResult(ctx, operation, syncErr)
	if syncErr != nil {
		slog.Warn("Failed to sync instance",
			slog.String("instance", instance.ResourceID),
			log.WithError(syncErr))
		s.schemaSyncer.FinishOperation(ctx, operation)
	} else {
		instance = updatedInstance
		// Sync all databases in the instance asynchronously.
		s.schemaSyncer.SyncAllDatabases(ctx, operation, instance)
	}

	result := convertInstanceMessage(instance)
	return connect.NewResponse(result), nil
}

// supportedStoreEngines are the engines the server has a driver for. The proto
// enum also carries reserved values that no driver answers, so creation must
// reject them instead of storing an instance that can never be opened.
var supportedStoreEngines = map[storepb.Engine]bool{
	storepb.Engine_MYSQL:     true,
	storepb.Engine_POSTGRES:  true,
	storepb.Engine_TIDB:      true,
	storepb.Engine_MARIADB:   true,
	storepb.Engine_OCEANBASE: true,
	storepb.Engine_STARROCKS: true,
	storepb.Engine_DORIS:     true,
	storepb.Engine_MSSQL:     true,
}

func isSupportedStoreEngine(engine storepb.Engine) bool {
	return supportedStoreEngines[engine]
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

	// Each masked metadata field is applied to the metadata read under the store's
	// row lock instead of a clone made here. A whole-metadata write built before
	// the lock would revert every field the mask does not name — most notably the
	// schema syncer's last_sync_time, version and mysql_lower_case_table_names,
	// which it writes while the administrator is editing.
	patch := &store.UpdateInstanceMessage{ResourceID: instance.ResourceID}
	for _, path := range req.Msg.UpdateMask.Paths {
		switch path {
		case "title":
			patch.MetadataUpdates = append(patch.MetadataUpdates, func(metadata *storepb.Instance) {
				metadata.Title = req.Msg.Instance.Title
			})
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
			patch.MetadataUpdates = append(patch.MetadataUpdates, func(metadata *storepb.Instance) {
				metadata.ExternalLink = req.Msg.Instance.ExternalLink
			})
		case "data_sources":
			return nil, connect.NewError(connect.CodeInvalidArgument,
				errors.New("data_sources cannot be updated through UpdateInstance; use CreateDataSource, UpdateDataSource and DeleteDataSource"))
		case "activation":
			patch.MetadataUpdates = append(patch.MetadataUpdates, func(metadata *storepb.Instance) {
				metadata.Activation = req.Msg.Instance.Activation
			})
		case "sync_interval":
			patch.MetadataUpdates = append(patch.MetadataUpdates, func(metadata *storepb.Instance) {
				metadata.SyncInterval = req.Msg.Instance.SyncInterval
			})
		case "maximum_connections":
			patch.MetadataUpdates = append(patch.MetadataUpdates, func(metadata *storepb.Instance) {
				metadata.MaximumConnections = req.Msg.Instance.MaximumConnections
			})
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

	_, err = s.store.UpdateInstance(ctx, &store.UpdateInstanceMessage{
		ResourceID: instance.ResourceID,
		Deleted:    &deletePatch,
		// Deactivating the instance is one metadata field; writing a whole clone
		// would revert a concurrent data-source or sync-time change.
		MetadataUpdates: []func(*storepb.Instance){
			func(metadata *storepb.Instance) { metadata.Activation = false },
		},
	})
	if err != nil {
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

	operation := s.schemaSyncer.StartOperation(storepb.SyncTrigger_SYNC_TRIGGER_MANUAL, currentUserID(ctx), instance)
	// See CreateInstance: the abandon is the fallback for a handler that returns
	// before the operation reported everything, and a no-op otherwise.
	defer s.schemaSyncer.AbandonOperation(ctx, operation, schemasync.ErrOperationAbandoned)
	updatedInstance, allDatabases, newDatabases, err := s.schemaSyncer.SyncInstance(ctx, instance)
	s.schemaSyncer.RecordInstanceResult(ctx, operation, err)
	if err != nil {
		s.schemaSyncer.FinishOperation(ctx, operation)
		return nil, err
	}
	if fullSyncRequested(req.Msg) {
		// Sync all databases in the instance asynchronously. The operation waits
		// for every one of them and reports the batch as one message.
		s.schemaSyncer.SyncAllDatabases(ctx, operation, updatedInstance)
	} else {
		s.schemaSyncer.EnqueueDatabases(ctx, operation, newDatabases)
	}

	response := &v1pb.SyncInstanceResponse{}
	for _, database := range allDatabases {
		response.Databases = append(response.Databases, database.Name)
	}
	return connect.NewResponse(response), nil
}

// fullSyncRequested reports whether a sync request asked for a full refresh.
// enable_full_sync is proto3 optional: unset means full, because the background
// scan and instance creation refresh everything, and an explicit false is the
// discovery-only mode (queue only the databases this enumeration discovered). Both
// SyncInstance and BatchSyncInstances must answer this identically, so the presence
// check lives here instead of being repeated.
func fullSyncRequested(request *v1pb.SyncInstanceRequest) bool {
	return request == nil || request.EnableFullSync == nil || request.GetEnableFullSync()
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
	if len(req.Msg.GetRequests()) > maxBatchInstances {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("a batch can contain at most %d instances", maxBatchInstances))
	}

	batchCtx, cancel := context.WithTimeout(ctx, batchSyncBudget)
	defer cancel()

	response := &v1pb.BatchSyncInstancesResponse{}
	for _, r := range req.Msg.GetRequests() {
		response.Results = append(response.Results, s.batchSyncInstance(batchCtx, r))
	}

	return connect.NewResponse(response), nil
}

// batchSyncInstance syncs one instance of a batch and reports its outcome. It is a
// function of its own so the operation's abandon fallback is a defer of this call:
// a defer registered in the batch's loop would run only when the whole RPC
// returned, leaving every operation un-abandoned until then.
func (s *InstanceService) batchSyncInstance(ctx context.Context, r *v1pb.SyncInstanceRequest) *v1pb.BatchSyncInstanceResult {
	result := &v1pb.BatchSyncInstanceResult{Name: r.GetName()}

	if ctx.Err() != nil {
		result.Error = fmt.Sprintf("the batch sync budget (%s) was exhausted before this instance was synced", batchSyncBudget)
		return result
	}

	instance, err := getInstanceMessage(ctx, s.store, r.GetName())
	if err != nil {
		result.Error = err.Error()
		return result
	}
	if instance.Deleted {
		result.Error = fmt.Sprintf("instance %q has been deleted", r.GetName())
		return result
	}

	// Each instance in the batch is its own operation, so the message the user
	// gets names the instance that finished instead of a batch of them.
	operation := s.schemaSyncer.StartOperation(storepb.SyncTrigger_SYNC_TRIGGER_MANUAL, currentUserID(ctx), instance)
	// See CreateInstance: the abandon is the fallback for a handler that returns
	// before the operation reported everything, and a no-op otherwise.
	defer s.schemaSyncer.AbandonOperation(ctx, operation, schemasync.ErrOperationAbandoned)
	updatedInstance, _, newDatabases, err := s.schemaSyncer.SyncInstance(ctx, instance)
	s.schemaSyncer.RecordInstanceResult(ctx, operation, err)
	if err != nil {
		s.schemaSyncer.FinishOperation(ctx, operation)
		result.Error = err.Error()
		return result
	}
	for _, database := range newDatabases {
		result.Databases = append(result.Databases, database.DatabaseName)
	}
	if r.GetEnableFullSync() {
		// Sync all databases in the instance asynchronously.
		s.schemaSyncer.SyncAllDatabases(ctx, operation, updatedInstance)
	} else {
		s.schemaSyncer.EnqueueDatabases(ctx, operation, newDatabases)
	}
	return result
}

// BatchUpdateInstances update multiple instances. Per-instance failures are
// reported in the response instead of aborting the whole batch after earlier
// updates were already committed.
func (s *InstanceService) BatchUpdateInstances(ctx context.Context, req *connect.Request[v1pb.BatchUpdateInstancesRequest]) (*connect.Response[v1pb.BatchUpdateInstancesResponse], error) {
	if len(req.Msg.GetRequests()) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("requests must not be empty"))
	}
	if len(req.Msg.GetRequests()) > maxBatchInstances {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("a batch can contain at most %d instances", maxBatchInstances))
	}

	response := &v1pb.BatchUpdateInstancesResponse{}
	for _, updateReq := range req.Msg.GetRequests() {
		result := &v1pb.BatchUpdateInstanceResult{Name: updateReq.GetInstance().GetName()}
		updated, err := s.UpdateInstance(ctx, connect.NewRequest(updateReq))
		if err != nil {
			result.Error = err.Error()
		} else {
			result.Instance = updated.Msg
		}
		response.Results = append(response.Results, result)
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

	if findDataSource(instance.Metadata, dataSource.GetId()) != nil {
		return nil, connect.NewError(connect.CodeAlreadyExists, errors.Errorf("data source %q already exists", dataSource.GetId()))
	}

	if req.Msg.ValidateOnly {
		if err := s.pingDataSource(ctx, instance, dataSource); err != nil {
			return nil, err
		}
		return connect.NewResponse(convertDataSource(instance.ResourceID, dataSource)), nil
	}

	// The append happens under the instance's row lock, so the duplicate check
	// and the write are one step and a concurrent create of the same ID loses
	// instead of storing both. A whole-metadata clone written here would also
	// revert the schema syncer's last_sync_time.
	alreadyExists := false
	_, err = s.store.UpdateInstance(ctx, &store.UpdateInstanceMessage{
		ResourceID: instance.ResourceID,
		MetadataUpdates: []func(*storepb.Instance){
			func(metadata *storepb.Instance) {
				if findDataSource(metadata, dataSource.GetId()) != nil {
					alreadyExists = true
					return
				}
				metadata.DataSources = append(metadata.DataSources, dataSource)
			},
		},
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if alreadyExists {
		return nil, connect.NewError(connect.CodeAlreadyExists, errors.Errorf("data source %q already exists", dataSource.GetId()))
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

	// The clone is used to validate the update mask and to test the connection;
	// the write below patches the data source found in the metadata read under
	// the store's row lock, so a concurrent change to another data source or to
	// the sync time is not reverted.
	metadata, ok := proto.Clone(instance.Metadata).(*storepb.Instance)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to convert instance metadata type"))
	}
	dataSource := findDataSource(metadata, dataSourceID)
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

	dataSourceMissing := false
	var patchErr error
	updated, err := s.store.UpdateInstance(ctx, &store.UpdateInstanceMessage{
		ResourceID: instance.ResourceID,
		MetadataUpdates: []func(*storepb.Instance){
			func(metadata *storepb.Instance) {
				stored := findDataSource(metadata, dataSourceID)
				if stored == nil {
					dataSourceMissing = true
					return
				}
				// The mask was validated against the clone above, so this cannot
				// fail here.
				patchErr = patchDataSource(stored, req.Msg.DataSource, req.Msg.UpdateMask.GetPaths())
			},
		},
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if patchErr != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, patchErr)
	}
	if dataSourceMissing {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("data source %q not found", req.Msg.DataSource.GetName()))
	}
	return connect.NewResponse(convertDataSource(updated.ResourceID, findDataSource(updated.Metadata, dataSourceID))), nil
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

	dataSource := findDataSource(instance.Metadata, dataSourceID)
	if dataSource == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("data source %q not found", req.Msg.GetName()))
	}
	// We only support removing RO type data source from an instance, see more details in instance_service.proto.
	if dataSource.GetType() != storepb.DataSourceType_READ_ONLY {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("only support deleting read-only data source"))
	}

	// Removing one entry is a targeted patch evaluated against the metadata read
	// under the row lock. A whole clone written here would revert a concurrent
	// change to another data source or to the schema syncer's last_sync_time.
	dataSourceMissing := false
	_, err = s.store.UpdateInstance(ctx, &store.UpdateInstanceMessage{
		ResourceID: instance.ResourceID,
		MetadataUpdates: []func(*storepb.Instance){
			func(metadata *storepb.Instance) {
				dataSources := metadata.GetDataSources()
				kept := make([]*storepb.DataSource, 0, len(dataSources))
				found := false
				for _, ds := range dataSources {
					if ds.GetId() == dataSourceID {
						found = true
						continue
					}
					kept = append(kept, ds)
				}
				if !found {
					dataSourceMissing = true
					return
				}
				metadata.DataSources = kept
			},
		},
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if dataSourceMissing {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("data source %q not found", req.Msg.GetName()))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// pingDataSource verifies that the server can actually connect with dataSource.
// It backs the UI's test connection action, so the returned error keeps the
// driver's own explanation — bad credentials, refused connection, TLS failure —
// because that reason is the point of the call. The caller is an authenticated
// instance admin who supplied the connection info, and the full error is logged
// as well.
//
// The connection is counted by the same per-instance limiter as a sync: it is a
// connection the instance has to carry, and leaving it unmetered let a caller
// stack unbounded attempts at a target that accepts TCP and then stalls. It is
// bounded by dataSourcePingTimeout for the same reason — a stalled test
// connection must not be able to hold the slots the syncs need.
func (s *InstanceService) pingDataSource(ctx context.Context, instance *store.InstanceMessage, dataSource *storepb.DataSource) error {
	ctx, cancel := context.WithTimeout(ctx, dataSourcePingTimeout)
	defer cancel()

	maximumConnections := int(instance.Metadata.GetMaximumConnections())
	if maximumConnections <= 0 {
		maximumConnections = common.DefaultInstanceMaximumConnections
	}
	waitCtx, cancelWait := context.WithTimeout(ctx, dataSourceConnectionWait)
	defer cancelWait()
	release, err := s.stateCfg.AcquireInstanceConnection(waitCtx, state.InstanceConnectionRequest{
		InstanceID:         instance.ResourceID,
		MaximumConnections: maximumConnections,
		Priority:           state.ConnectionPriorityInteractive,
	})
	if err != nil {
		return connect.NewError(connect.CodeResourceExhausted,
			errors.Errorf("the instance is using all of its connections right now; retry the test connection shortly: %v", err))
	}
	defer release()

	driver, err := s.dbFactory.GetDataSourceDriver(
		ctx, instance, dataSource,
		db.ConnectionContext{ReadOnly: dataSource.GetType() == storepb.DataSourceType_READ_ONLY},
	)
	if err != nil {
		slog.Error("failed to build the data source driver", "instance", instance.ResourceID, "type", dataSource.GetType().String(), log.WithError(err))
		return dataSourceConnectionError(dataSource, err)
	}
	defer driver.Close(ctx)
	if err := driver.Ping(ctx); err != nil {
		slog.Error("failed to connect to the data source", "instance", instance.ResourceID, "type", dataSource.GetType().String(), log.WithError(err))
		return dataSourceConnectionError(dataSource, err)
	}
	return nil
}

// dataSourceConnectionError names the data source that failed a test connection
// and carries the underlying driver cause, so the user can tell a wrong
// password from an unreachable host or a TLS problem.
//
// Echoing the driver's own error is deliberate. Testing a connection requires
// instance or data-source write permission (workspaceAdmin by default), and the
// caller supplied the host, port and credentials, so the resolved address in the
// message is not a secret from them. Reducing every failure to
// "invalid datasource <type>", as this once did, left the test connection action
// with nothing to report.
func dataSourceConnectionError(dataSource *storepb.DataSource, err error) error {
	name := dataSource.GetId()
	if name == "" {
		name = strings.ToLower(dataSource.GetType().String())
	}
	return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("failed to connect to data source %q: %v", name, err))
}

// findDataSource returns the data source with id, or nil when the instance does
// not have it. It is also called from the store's metadata patch closures, where
// it sees the row read under the lock rather than this handler's older clone.
func findDataSource(metadata *storepb.Instance, id string) *storepb.DataSource {
	for _, dataSource := range metadata.GetDataSources() {
		if dataSource.GetId() == id {
			return dataSource
		}
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
