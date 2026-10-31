package qualityrunner

import (
	"context"
	"errors"
	"time"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/state"
	"github.com/Ranxy/metaxisdata/backend/store"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// executionTask identifies one planned quality_execution row a batch has to
// execute. ruleID is known for a scheduled plan, where the due scan read it; a
// queued manual batch carries the row ids the API planned and resolves each rule
// id from the row itself.
type executionTask struct {
	executionID int64
	ruleID      int64
}

// batchExecutionStore is the slice of the store that executing one dispatch
// batch needs. *store.Store implements it; the runner-level tests inject a fake
// so the incomplete-batch accounting can be exercised without a database.
type batchExecutionStore interface {
	MarkQualityRunRunning(ctx context.Context, batchID int64, at time.Time) error
	GetQualityExecution(ctx context.Context, id int64) (*store.QualityExecutionMessage, error)
	ListRecentQualityExecutions(ctx context.Context, ruleID int64, limit int) ([]*store.QualityExecutionMessage, error)
	MarkQualityExecutionRunning(ctx context.Context, id int64, at time.Time) error
	FinishQualityExecution(ctx context.Context, execution *store.QualityExecutionMessage) error
	AbandonQualityExecutions(ctx context.Context, batchID int64, at time.Time) (int64, error)
	FinishQualityRun(ctx context.Context, batchID int64, status storepb.QualityExecutionState, outcome storepb.QualityStatus, counts *storepb.QualityStatusCounts, at time.Time) error
}

// manualRun is one queued manual batch. executions is the exact selection the
// API planned: a manual batch must not silently widen to every enabled rule of
// the target. instanceResourceID is the instance the API grouped the batch on;
// the queue carries it because admission needs the instance's connection ceiling
// before the batch's rows are read.
type manualRun struct {
	batchID            int64
	instanceResourceID string
	executions         []executionTask
}

// dueBatchPlan is one internal batch the due scan is about to create: one
// instance and exactly one target, with at most maxExecutionsPerBatch rules. A
// target larger than the cap (only data that bypassed the API limit) spans
// several plans. The target schedule is advanced only when the whole cycle is
// settled, so a plan never advances it by itself.
type dueBatchPlan struct {
	instanceResourceID string
	target             *store.DueQualityTarget
	rules              []*store.DueQualityRule
}

// scheduledGroup is one instance's due plans in execution order. The due scan
// hands over one group per instance, and a worker runs its plans serially: one
// instance's batches must not execute concurrently, even when a later tick finds
// more of its targets due while the earlier group is still queued.
type scheduledGroup struct {
	instanceResourceID string
	plans              []dueBatchPlan
}

// batchAdmission is what a batch holds while it executes: the instance whose
// connection budget it leased (empty when it needed none) and the priority it was
// admitted with. A rule whose target moved to another instance after admission
// cannot use it; see ruleConnectionLease.
type batchAdmission struct {
	instanceResourceID string
	priority           state.ConnectionPriority
}

// ruleExecution is one execution's stored row plus the inputs the problem ledger
// and the notifications need after the row is written: the mutable head (owner,
// severity, mute, resource id), the immutable definition that was executed, and
// the rule's status before this execution.
type ruleExecution struct {
	result   *store.QualityExecutionMessage
	head     *store.QualityRuleMessage
	snapshot *dataquality.RuleSnapshot
	previous *storepb.QualityStatus
}

// errRuleRetired reports that a rule was archived or deleted after it was picked
// up; the execution records SKIPPED rather than executing a retired definition.
var errRuleRetired = errors.New("quality rule is retired")

// ruleExecutionOutcome is everything one execution produced, in the shape the
// execution row needs. Keeping the failure paths and the evaluated path in one
// struct makes buildExecutionMessage the single mapping from a verdict to a
// stored execution.
type ruleExecutionOutcome struct {
	snapshot     *dataquality.RuleSnapshot
	startedAt    time.Time
	finishedAt   time.Time
	observation  dataquality.Observation
	execution    *dataquality.Execution
	state        dataquality.ResultState
	errorCode    string
	errorMessage string
}
