package v1

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// A wake must not hand a person a question that already has an answer. A case whose class describes
// the target is settled by the server, so escalating it is refused with the reason and the move the
// model should make instead; sixteen empty-table cases once reached the owner this way.
func TestFactCaseEscalationIsRefused(t *testing.T) {
	t.Parallel()

	require.NoError(t, factCaseEscalationRefusal(&store.QualityAgentCaseMessage{
		FailureClass: string(dataquality.ClassConfigUnsupported),
	}), "a check that cannot run is a decision a person or a repair can still take")

	err := factCaseEscalationRefusal(&store.QualityAgentCaseMessage{
		FailureClass: string(dataquality.ClassNoVerdictEmpty),
	})
	require.Error(t, err)
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
	require.Contains(t, err.Error(), "fact about the target")
	require.Contains(t, err.Error(), "record_asset_fact")
}
