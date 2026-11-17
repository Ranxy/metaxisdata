package v1

import (
	"time"

	"github.com/Ranxy/metaxisdata/backend/component/agent/tool"
	"github.com/Ranxy/metaxisdata/backend/component/tools/metadata"
	"github.com/Ranxy/metaxisdata/backend/metrics"
)

// explainSQLToolSet resolves ExplainSQL's read-only tool set. The declarations and
// bodies live in the shared metadata package; this function keeps the service's
// call site and its request scope.
func (s *ExplainSQLService) explainSQLToolSet(scopePrefix, instanceID string) (*tool.Set, error) {
	return metadata.NewExplainToolSet(s.store, scopePrefix, instanceID)
}

// explainToolObserver records the ExplainSQL tool calls under the explain_sql
// product label in the shared tool metrics.
type explainToolObserver struct {
	metrics *metrics.Metrics
}

// ObserveToolCall records one ExplainSQL tool execution.
func (o explainToolObserver) ObserveToolCall(toolName, outcome string, duration time.Duration, resultBytes int) {
	if o.metrics == nil {
		return
	}
	o.metrics.ObserveAgentToolCall("explain_sql", toolName, outcome, duration, resultBytes)
}
