package audit

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/structpb"
)

// A handler records audit-only detail the request message cannot carry; the
// interceptor merges it into the stored request payload under "auditDetail".
func TestDetailRecorderAttachesToTheAuditPayload(t *testing.T) {
	t.Parallel()

	ctx := WithDetailRecorder(context.Background())
	RecordDetail(ctx, "deletedGrant", map[string]any{
		"name":     "iamGrants/42",
		"resource": "instances/inst-1/databases/sales",
		"role":     "roles/qualityOwner",
	})

	payload, err := structpb.NewStruct(map[string]any{"name": "iamGrants/42"})
	require.NoError(t, err)
	merged := AttachDetail(ctx, payload)
	require.Equal(t, "iamGrants/42", merged.GetFields()["name"].GetStringValue())

	detail := merged.GetFields()["auditDetail"].GetStructValue()
	require.NotNil(t, detail)
	grant := detail.GetFields()["deletedGrant"].GetStructValue()
	require.NotNil(t, grant)
	require.Equal(t, "instances/inst-1/databases/sales", grant.GetFields()["resource"].GetStringValue())
	require.Equal(t, "roles/qualityOwner", grant.GetFields()["role"].GetStringValue())

	// A second attach sees the same fields: the recorder is not consumed.
	again := AttachDetail(ctx, &structpb.Struct{})
	require.NotNil(t, again.GetFields()["auditDetail"].GetStructValue())
}

func TestAuditDetailIsANoOpWithoutARecorder(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	RecordDetail(ctx, "deletedGrant", map[string]any{"name": "iamGrants/42"})

	payload, err := structpb.NewStruct(map[string]any{"name": "iamGrants/42"})
	require.NoError(t, err)
	require.Equal(t, payload, AttachDetail(ctx, payload))

	// A recorder with no fields leaves the payload untouched too.
	empty := WithDetailRecorder(context.Background())
	require.Equal(t, payload, AttachDetail(empty, payload))
	require.Equal(t, "iamGrants/42", AttachDetail(empty, payload).GetFields()["name"].GetStringValue())
}
