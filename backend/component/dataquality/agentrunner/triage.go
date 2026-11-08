package agentrunner

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/pkg/errors"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// triageMaxTurns bounds one triage wake's LLM calls. Triage reads context, drafts
// checks and records a conclusion, so it needs a few more turns than a monitor wake.
const triageMaxTurns = 12

// maxTriageDraftColumns bounds the columns one draft_checks call may scope.
const maxTriageDraftColumns = 8

// TableContextColumn is one column of the target as the registry records it.
type TableContextColumn struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
	Comment  string `json:"comment,omitempty"`
	Key      bool   `json:"key,omitempty"`
}

// TableContextProfile is one column's stored profile summary. Only statistics are
// exposed: no sample rows, and the profile service has already redacted personal data.
type TableContextProfile struct {
	Column        string  `json:"column"`
	NullFraction  float32 `json:"nullFraction,omitempty"`
	DistinctCount int64   `json:"distinctCount,omitempty"`
	Min           string  `json:"min,omitempty"`
	Max           string  `json:"max,omitempty"`
	TopValues     string  `json:"topValues,omitempty"`
	ProfiledAt    string  `json:"profiledAt,omitempty"`
}

// TableContext is the bounded, read-only view of a target a triage wake reads. It never
// contains sample rows.
type TableContext struct {
	TargetGUID  string                `json:"target"`
	TargetType  string                `json:"type"`
	Engine      string                `json:"engine"`
	Instance    string                `json:"instance"`
	Database    string                `json:"database"`
	Schema      string                `json:"schema,omitempty"`
	Object      string                `json:"object"`
	Columns     []TableContextColumn  `json:"columns"`
	Profiles    []TableContextProfile `json:"profiles,omitempty"`
	Labels      map[string]string     `json:"labels,omitempty"`
	Downstream  int32                 `json:"downstreamConsumers"`
	Upstream    int32                 `json:"upstreamSources"`
	SchemaStale bool                  `json:"schemaChangedRecently,omitempty"`
}

// TableContextReader loads the table context one triage wake reads. Production wires it
// to the registry, profiles, labels and lineage; a test can return a fixed value.
type TableContextReader interface {
	ReadTableContext(ctx context.Context, targetGUID string, targetType storepb.MetaType) (*TableContext, error)
}

// DraftChecksFunc drafts checks for one target and returns the queued draft titles. It
// reuses the same Suggester the suggestion center uses, so the drafts land in the review
// queue; the model cannot create enabled checks.
type DraftChecksFunc func(ctx context.Context, targetGUID string, targetType storepb.MetaType, columns []string, intent string, createdBy int32) ([]string, error)

// Option configures optional agent capabilities without changing the constructor's
// existing callers.
type Option func(*Engine)

// WithTableContext gives the engine the read-only table context a triage wake uses.
func WithTableContext(reader TableContextReader) Option {
	return func(engine *Engine) { engine.tableContext = reader }
}

// WithDraftChecks gives the engine the check-drafting pipeline a triage wake uses.
func WithDraftChecks(draft DraftChecksFunc) Option {
	return func(engine *Engine) { engine.draftChecks = draft }
}

// getTableContextTool renders the target context as tool JSON.
func (e *Engine) getTableContextTool(ctx context.Context, session *store.QualityAgentSessionMessage) (string, error) {
	if e.tableContext == nil {
		return "", errors.New("the table context is not configured in this process")
	}
	target, err := e.tableContext.ReadTableContext(ctx, session.TargetGUID, storepb.MetaType(session.TargetType))
	if err != nil {
		return "", err
	}
	return marshalJSON(target)
}

// draftChecksTool drafts checks for the target from the model's intent and columns.
func (e *Engine) draftChecksTool(ctx context.Context, session *store.QualityAgentSessionMessage, arguments string) (string, error) {
	if e.draftChecks == nil {
		return "", errors.New("check drafting is not configured in this process")
	}
	var input struct {
		Intent  string   `json:"intent"`
		Columns []string `json:"columns"`
	}
	if err := json.Unmarshal([]byte(arguments), &input); err != nil {
		return "", errors.Wrap(err, "invalid draft_checks arguments")
	}
	intent := strings.TrimSpace(input.Intent)
	if intent == "" {
		return "", errors.New("draft_checks needs an intent")
	}
	if len(intent) > 2000 {
		intent = intent[:2000]
	}
	columns := make([]string, 0, len(input.Columns))
	for _, column := range input.Columns {
		column = strings.TrimSpace(column)
		if column == "" {
			continue
		}
		columns = append(columns, column)
		if len(columns) >= maxTriageDraftColumns {
			break
		}
	}
	titles, err := e.draftChecks(ctx, session.TargetGUID, storepb.MetaType(session.TargetType), columns, intent, session.AgentPrincipalID)
	if err != nil {
		return "", err
	}
	return marshalJSON(map[string]any{"queued": len(titles), "drafts": titles})
}
