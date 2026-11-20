package agentrunner

import (
	"context"
	"time"

	"github.com/Ranxy/metaxisdata/backend/component/agent/tool"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/tools/quality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// triageMaxTurns bounds one triage wake's LLM calls. Triage reads context, drafts
// checks and records a conclusion, so it needs a few more turns than a monitor wake.
const triageMaxTurns = 12

// The triage capabilities and their data shapes live in the shared quality tool
// package; these aliases keep the product's wiring and tests reading them.
type (
	TableContext        = quality.TableContext
	TableContextColumn  = quality.TableContextColumn
	TableContextProfile = quality.TableContextProfile
	TableContextReader  = quality.TableContextReader
	DraftChecksFunc     = quality.DraftChecksFunc
)

// Option configures optional agent capabilities without changing the constructor's
// existing callers.
type Option func(*Engine)

// WithTableContext gives the engine the read-only table context a triage wake uses.
func WithTableContext(reader TableContextReader) Option {
	return func(engine *Engine) { engine.tableContext = reader }
}

// AdoptionFunc runs after a successful triage wake: it applies the agent's
// auto-adoption guardrails to the drafts that wake queued. since is when the wake
// started, so a draft that a previous pass merely refreshed cannot be mistaken for
// a new one. A nil hook leaves every draft in the review queue.
type AdoptionFunc func(ctx context.Context, session *store.QualityAgentSessionMessage, since time.Time)

// OwnerAdoptionFunc executes an adoption pass the owner explicitly requested in
// the current wake. It is the in-process counterpart of the adopt tool; a process
// that does not wire it keeps the tool's execution branch returning an error.
type OwnerAdoptionFunc func(ctx context.Context, req dataquality.AgentAdoptionRequest) (dataquality.AgentAdoptionReport, error)

// WithDraftChecks gives the engine the check-drafting pipeline a triage wake uses.
func WithDraftChecks(draft DraftChecksFunc) Option {
	return func(engine *Engine) { engine.draftChecks = draft }
}

// WithPermissionChecker gives the engine the IAM checker a tool call is gated by.
func WithPermissionChecker(checker tool.PermissionChecker) Option {
	return func(engine *Engine) { engine.checker = checker }
}

// WithMaxParallelReads lets one model batch run this many parallel-safe reads at
// once. A value below 2 keeps execution serial.
func WithMaxParallelReads(limit int) Option {
	return func(engine *Engine) { engine.maxParallelReads = limit }
}

// WithToolAudit records side-effecting tool calls in the deployment ledger.
func WithToolAudit(sink tool.AuditSink) Option {
	return func(engine *Engine) { engine.auditSink = sink }
}

// storepb keeps the type import used by the aliases' signatures.
var _ = storepb.MetaType_TABLE
