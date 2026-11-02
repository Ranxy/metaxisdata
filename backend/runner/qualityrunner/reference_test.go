package qualityrunner

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/dbfactory"
	"github.com/Ranxy/metaxisdata/backend/component/state"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"
	"github.com/Ranxy/metaxisdata/backend/store"

	// Register the PostgreSQL driver so a reference session can be opened without
	// touching the network (the driver's Open is lazy).
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	_ "github.com/Ranxy/metaxisdata/backend/plugin/db/pg"
)

// crossTableRuleSnapshot is a minimal reconciliation rule; the runner's reference
// wiring, not the compiler, is what these tests drive.
func crossTableRuleSnapshot(referenceGUID string) *dataquality.RuleSnapshot {
	return &dataquality.RuleSnapshot{
		Type: storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE,
		Config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_CrossTable{
			CrossTable: &storepb.CrossTableConfig{
				LeftFunction:  storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT,
				RightFunction: storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT,
				Reference: &storepb.CrossTableReference{
					MetaGuid: referenceGUID,
					MetaType: storepb.MetaType_TABLE,
				},
			},
		}},
	}
}

// crossTableTargetResolution is the rule's own target: a PostgreSQL table.
func crossTableTargetResolution() *dataquality.Resolution {
	return &dataquality.Resolution{
		Target: dataquality.ResolvedTarget{
			Target: dataquality.Target{
				GUID:     "inst-a;shop;public;orders",
				MetaType: storepb.MetaType_TABLE,
			},
			Engine:             storepb.Engine_POSTGRES,
			InstanceResourceID: "inst-a",
			Database:           "shop",
			Schema:             "public",
			Table:              "orders",
		},
	}
}

// referenceTestInstance builds an instance whose only data source is READ_ONLY when
// asked for one, so a test that does not expect a session can prove it by the
// factory refusing it.
func referenceTestInstance(resourceID string, withReadOnly bool) *store.InstanceMessage {
	source := &storepb.DataSource{Type: storepb.DataSourceType_ADMIN, Host: "", Port: ""}
	if withReadOnly {
		source = &storepb.DataSource{
			Type:     storepb.DataSourceType_READ_ONLY,
			Host:     "127.0.0.1",
			Port:     "5432",
			Username: "probe",
			Password: "probe",
			Database: "probe",
		}
	}
	return &store.InstanceMessage{
		ResourceID: resourceID,
		Metadata: &storepb.Instance{
			Engine:      storepb.Engine_POSTGRES,
			DataSources: []*storepb.DataSource{source},
			Version:     "16",
		},
	}
}

// referenceTestResolution resolves a reference relation living in database of the
// given instance.
func referenceTestResolution(instance *store.InstanceMessage) *dataquality.Resolution {
	const database = "shop"
	return &dataquality.Resolution{
		Target: dataquality.ResolvedTarget{
			Target: dataquality.Target{
				GUID:     instance.ResourceID + ";" + database + ";public;customers",
				MetaType: storepb.MetaType_TABLE,
			},
			Engine:             storepb.Engine_POSTGRES,
			InstanceResourceID: instance.ResourceID,
			Database:           database,
			Schema:             "public",
			Table:              "customers",
			Columns: []dataquality.ColumnInfo{
				{Name: "id", Type: "bigint", Kind: dataquality.ColumnKindNumeric},
			},
		},
		Instance: instance,
		Database: &store.DatabaseMessage{InstanceID: instance.ResourceID, DatabaseName: database},
	}
}

// staticReferenceResolver returns the given resolution or error for every lookup.
func staticReferenceResolver(resolution *dataquality.Resolution, err error) resolveReferenceTargetFunc {
	return func(context.Context, string, storepb.MetaType) (*dataquality.Resolution, error) {
		return resolution, err
	}
}

// referenceTestQueryer answers one compiled aggregate with a canned row.
type referenceTestQueryer struct {
	columns []string
	row     []any
	queries []string
}

func (q *referenceTestQueryer) QueryReadOnly(_ context.Context, query string, _ []any, _ db.QueryOptions) (*db.QueryResult, error) {
	q.queries = append(q.queries, query)
	return &db.QueryResult{Columns: q.columns, Rows: [][]any{q.row}}, nil
}

func referenceTestRunner(stateCfg *state.State) *Runner {
	return &Runner{
		stateCfg:  stateCfg,
		dbFactory: dbfactory.New(nil),
		exec:      dataquality.NewExecutor(),
	}
}

// TestSharedReferenceConnectionDoesNotOpenItsOwnSession is the shared-connection
// contract: the reference lives in the target's own database, so the executor
// keeps both aggregates in one statement. The reference instance deliberately has
// no READ_ONLY data source, so any attempt to open a second session would fail the
// test instead of silently succeeding.
func TestSharedReferenceConnectionDoesNotOpenItsOwnSession(t *testing.T) {
	t.Parallel()

	stateCfg, err := state.New(nil)
	require.NoError(t, err)
	runner := referenceTestRunner(stateCfg)
	reference := referenceTestResolution(referenceTestInstance("inst-a", false))
	runner.resolveReferenceTarget = staticReferenceResolver(reference, nil)

	snapshot := crossTableRuleSnapshot(reference.Target.GUID)
	option, err := runner.prepareReferenceExecution(context.Background(), snapshot, batchAdmission{})
	require.NoError(t, err)

	queryer := &referenceTestQueryer{columns: []string{"left_value", "right_value"}, row: []any{int64(4), int64(4)}}
	execution, err := runner.exec.Execute(context.Background(), queryer, crossTableTargetResolution(), *snapshot,
		dataquality.ExecOptions{Reference: option})
	require.NoError(t, err, "a shared reference must not need its own session")
	require.Empty(t, execution.ReferenceExecutedSQL, "both sides ran in one statement")
	require.Len(t, queryer.queries, 1)
	require.Zero(t, stateCfg.ConnectionInUse(), "no reference lease was taken")
}

// TestBusyReferenceInstanceSkipsTheCheck covers the non-blocking lease: while the
// reference instance's only slot is held elsewhere, the check is a transient
// capacity skip, not an ERROR, and the opener hands back nothing.
func TestBusyReferenceInstanceSkipsTheCheck(t *testing.T) {
	t.Parallel()

	stateCfg, err := state.New(nil)
	require.NoError(t, err)
	runner := referenceTestRunner(stateCfg)
	busyInstance := referenceTestInstance("ref-inst", false)
	busyInstance.Metadata.MaximumConnections = 1
	reference := referenceTestResolution(busyInstance)
	runner.resolveReferenceTarget = staticReferenceResolver(reference, nil)

	hold, err := stateCfg.AcquireInstanceConnection(context.Background(), state.InstanceConnectionRequest{
		InstanceID:         "ref-inst",
		MaximumConnections: 1,
		Priority:           state.ConnectionPriorityBackground,
	})
	require.NoError(t, err)
	defer hold()
	require.Equal(t, 1, stateCfg.ConnectionInUse())

	snapshot := crossTableRuleSnapshot(reference.Target.GUID)
	option, err := runner.prepareReferenceExecution(context.Background(), snapshot, batchAdmission{})
	require.NoError(t, err)

	_, err = runner.exec.Execute(context.Background(), &referenceTestQueryer{}, crossTableTargetResolution(), *snapshot,
		dataquality.ExecOptions{Reference: option})
	require.ErrorIs(t, err, errReferenceConnectionUnavailable)

	status, code, message := executionErrorVerdict(err)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_SKIPPED, status,
		"a busy reference instance is skipped and retried, never an ERROR")
	require.Empty(t, code)
	require.Contains(t, message, "no free connection slot")
	require.Equal(t, 1, stateCfg.ConnectionInUse(), "the failed opener did not take a slot")
}

// TestReferenceWithoutReadOnlyDataSourceIsNoReadOnlyError covers the factory error
// path: the reference instance has no READ_ONLY data source, so the check is
// ERROR(NO_READ_ONLY_SOURCE) and the lease the opener took is handed back.
func TestReferenceWithoutReadOnlyDataSourceIsNoReadOnlyError(t *testing.T) {
	t.Parallel()

	stateCfg, err := state.New(nil)
	require.NoError(t, err)
	runner := referenceTestRunner(stateCfg)
	reference := referenceTestResolution(referenceTestInstance("ref-inst", false))
	runner.resolveReferenceTarget = staticReferenceResolver(reference, nil)

	snapshot := crossTableRuleSnapshot(reference.Target.GUID)
	option, err := runner.prepareReferenceExecution(context.Background(), snapshot, batchAdmission{})
	require.NoError(t, err)

	_, err = runner.exec.Execute(context.Background(), &referenceTestQueryer{}, crossTableTargetResolution(), *snapshot,
		dataquality.ExecOptions{Reference: option})
	require.ErrorIs(t, err, dbfactory.ErrNoReadOnlyDataSource)

	status, code, _ := executionErrorVerdict(err)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_ERROR, status)
	require.Equal(t, dataquality.ErrCodeNoReadOnly, code)
	require.Zero(t, stateCfg.ConnectionInUse(), "the opener released the lease it took")
}

// TestReferenceResolutionFailureIsUnsupported covers a reconciliation whose
// reference relation was dropped or moved: the stored rule cannot execute, so it is
// ERROR(UNSUPPORTED), like a target that lost a named column.
func TestReferenceResolutionFailureIsUnsupported(t *testing.T) {
	t.Parallel()

	runner := &Runner{
		resolveReferenceTarget: staticReferenceResolver(nil,
			common.Errorf(common.NotFound, "reference relation is gone")),
	}
	_, err := runner.prepareReferenceExecution(context.Background(),
		crossTableRuleSnapshot("inst-a;shop;public;customers"), batchAdmission{})
	require.ErrorContains(t, err, "reference relation is gone")
	require.Equal(t, common.Invalid, common.ErrorCode(err))

	status, code, message := executionErrorVerdict(err)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_ERROR, status)
	require.Equal(t, dataquality.ErrCodeUnsupported, code)
	require.Contains(t, message, "reference relation is gone")
}

// A transient store failure while resolving the reference is not a definition that
// cannot execute: it keeps ERROR(EXECUTION) instead of being reported as unsupported.
func TestReferenceResolutionTransientFailureKeepsItsClassification(t *testing.T) {
	t.Parallel()

	runner := &Runner{
		resolveReferenceTarget: staticReferenceResolver(nil,
			common.Errorf(common.Internal, "the metadata store is unavailable")),
	}
	_, err := runner.prepareReferenceExecution(context.Background(),
		crossTableRuleSnapshot("inst-a;shop;public;customers"), batchAdmission{})
	require.Equal(t, common.Internal, common.ErrorCode(err),
		"a transient failure must not be rewritten as an invalid definition")

	status, code, _ := executionErrorVerdict(err)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_ERROR, status)
	require.Equal(t, dataquality.ErrCodeExecution, code)
}

// TestOpenReferenceSessionReleasesTheLeaseOnce pins the session's lifecycle: the
// non-blocking lease is held while the session is open, and the returned release is
// idempotent so a double call cannot free a slot twice.
func TestOpenReferenceSessionReleasesTheLeaseOnce(t *testing.T) {
	t.Parallel()

	stateCfg, err := state.New(nil)
	require.NoError(t, err)
	runner := referenceTestRunner(stateCfg)
	reference := referenceTestResolution(referenceTestInstance("ref-inst", true))

	queryer, release, err := runner.openReferenceSession(context.Background(), reference, batchAdmission{})
	require.NoError(t, err)
	require.NotNil(t, queryer)
	require.NotNil(t, release)
	require.Equal(t, 1, stateCfg.ConnectionInUse())

	release()
	release()
	require.Zero(t, stateCfg.ConnectionInUse(), "the lease is released exactly once")
}

// TestBuildExecutionMessageStoresReferenceExecutedSQL is the evidence contract: the
// reference-side statement the executor ran reaches the row FinishQualityExecution
// writes.
func TestBuildExecutionMessageStoresReferenceExecutedSQL(t *testing.T) {
	t.Parallel()

	startedAt := time.Now().UTC()
	execution := buildExecutionMessage(&store.QualityExecutionMessage{ID: 3, RunID: 4, RuleID: 5},
		&ruleExecutionOutcome{
			startedAt:  startedAt,
			finishedAt: startedAt.Add(time.Second),
			state:      dataquality.ResultState{Status: storepb.QualityStatus_QUALITY_STATUS_PASS},
			execution: &dataquality.Execution{
				ExecutedSQL:          "SELECT left",
				ReferenceExecutedSQL: "SELECT right",
			},
		})
	require.Equal(t, "SELECT left", execution.ExecutedSQL)
	require.Equal(t, "SELECT right", execution.ReferenceExecutedSQL)
}
