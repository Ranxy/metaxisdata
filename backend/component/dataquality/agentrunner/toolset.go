package agentrunner

import (
	"context"

	"github.com/Ranxy/metaxisdata/backend/component/agent/tool"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// maxToolResultBytes bounds one tool result fed back to the model. The context is
// the scarce resource: an oversized result is cut, and the transcript records the
// truncation.
const maxToolResultBytes = 8 << 10

// toolSet binds the mode's declarations to this wake's execution body. The set is
// per wake because a tool body reads the session and its wake state; the
// declarations are shared.
func (e *Engine) toolSet(session *store.QualityAgentSessionMessage, agent *store.QualityAgentMessage, control *wakeControl) (*tool.Set, error) {
	specs := toolsFor(specForSession(session), agent)
	registry := tool.NewRegistry()
	names := make([]string, 0, len(specs))
	for _, spec := range specs {
		spec := spec
		// The adoption tool is the one capability-granted write, and it is reachable
		// only while answering an owner's question with adoption configured. Filtering
		// here keeps it out of the schema, not merely rejected at execution time.
		if spec.name == "adopt_pending_drafts" &&
			(!control.ownerAsked || !dataquality.AgentAdoptionReady(agent)) {
			continue
		}
		names = append(names, spec.name)
		err := registry.Register(tool.Tool{
			Name:        spec.name,
			Description: spec.description,
			Parameters:  spec.parameters,
			Meta:        spec.meta,
			Execute: func(ctx context.Context, call tool.Call, _ *tool.Invocation) (tool.Result, error) {
				content, err := e.executeTool(ctx, session, agent, spec.name, string(call.Arguments), control)
				if err != nil {
					return tool.Result{}, err
				}
				return tool.Result{Content: content}, nil
			},
		})
		if err != nil {
			return nil, err
		}
	}
	return registry.Resolve(names)
}
