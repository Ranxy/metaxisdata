package v1

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// A memory without a subject, a statement or evidence is not knowledge; a past
// expiry would make it stale on arrival.
func TestValidateQualityMemoryRequest(t *testing.T) {
	t.Parallel()

	base := func() *v1pb.CreateQualityMemoryRequest {
		return &v1pb.CreateQualityMemoryRequest{
			Kind:      v1pb.QualityMemoryKind_QUALITY_MEMORY_KIND_ASSET_FACT,
			Subject:   "inst;db;public;orders",
			Statement: "the table holds roughly 20k rows",
			Evidence: []*v1pb.QualityInvestigationEvidence{{
				Kind: "PROFILE", Reference: "profiles/orders",
			}},
		}
	}
	input, err := validateQualityMemoryRequest(base(), storepb.QualityMemorySource_QUALITY_MEMORY_SOURCE_AGENT,
		storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_CONFIRMED, nil)
	require.NoError(t, err)
	require.Len(t, input.Evidence.GetEvidence(), 1)

	noEvidence := base()
	noEvidence.Evidence = nil
	_, err = validateQualityMemoryRequest(noEvidence, storepb.QualityMemorySource_QUALITY_MEMORY_SOURCE_AGENT,
		storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_CONFIRMED, nil)
	require.Error(t, err, "a memory needs evidence")

	noSubject := base()
	noSubject.Subject = " "
	_, err = validateQualityMemoryRequest(noSubject, storepb.QualityMemorySource_QUALITY_MEMORY_SOURCE_AGENT,
		storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_CONFIRMED, nil)
	require.Error(t, err)

	noStatement := base()
	noStatement.Statement = ""
	_, err = validateQualityMemoryRequest(noStatement, storepb.QualityMemorySource_QUALITY_MEMORY_SOURCE_AGENT,
		storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_CONFIRMED, nil)
	require.Error(t, err)

	pastExpiry := base()
	pastExpiry.ExpiresAt = timestamppb.New(time.Now().Add(-time.Hour))
	_, err = validateQualityMemoryRequest(pastExpiry, storepb.QualityMemorySource_QUALITY_MEMORY_SOURCE_AGENT,
		storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_CONFIRMED, nil)
	require.Error(t, err, "an expiry in the past is stale on arrival")

	unknownKind := base()
	unknownKind.Kind = v1pb.QualityMemoryKind(99)
	_, err = validateQualityMemoryRequest(unknownKind, storepb.QualityMemorySource_QUALITY_MEMORY_SOURCE_AGENT,
		storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_CONFIRMED, nil)
	require.Error(t, err)
}

// A confirmed memory whose expiry has passed reads as EXPIRED, so stale knowledge is
// never shown as current.
func TestConvertQualityMemoryExpired(t *testing.T) {
	t.Parallel()

	expired := time.Now().Add(-time.Minute)
	converted := convertQualityMemoryToV1("ws1", &store.QualityMemoryMessage{
		ResourceID: "m1",
		Kind:       storepb.QualityMemoryKind_QUALITY_MEMORY_KIND_ASSET_FACT,
		Subject:    "inst;db;public;orders",
		Statement:  "row count ~20k",
		Status:     storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_CONFIRMED,
		ExpiresAt:  &expired,
	})
	require.Equal(t, v1pb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_EXPIRED, converted.GetStatus())
	require.Equal(t, "workspaces/ws1/qualityMemories/m1", converted.GetName())
}
