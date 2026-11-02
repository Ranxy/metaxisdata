package v1

import (
	"context"
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// fakeLineageFetcher answers each level from a fixed graph, obeying the probe rule
// (at most budget+1 peers) the real store query implements with its LIMIT.
func fakeLineageFetcher(graph map[string][]string) lineageLevelFetcher {
	return func(_ context.Context, guids []string, budget int) ([]string, error) {
		peers := make([]string, 0, budget+1)
		for _, guid := range guids {
			peers = append(peers, graph[guid]...)
		}
		if len(peers) > budget+1 {
			peers = peers[:budget+1]
		}
		return peers, nil
	}
}

// The walk is breadth first, keeps each node's signed distance, and a cycle back to
// an already visited node neither loops nor duplicates a row.
func TestWalkLineageFollowsOneDirection(t *testing.T) {
	t.Parallel()

	graph := map[string][]string{
		"a": {"b", "c"},
		"b": {"d"},
		"c": {"a"},
	}
	nodes, depthReached, truncated, err := walkLineage(context.Background(), "a", 3, 100, 1, fakeLineageFetcher(graph))
	require.NoError(t, err)
	require.False(t, truncated)
	require.Equal(t, int32(2), depthReached)
	require.Equal(t, []lineageWalkNode{
		{GUID: "a", Distance: 0},
		{GUID: "b", Distance: 1},
		{GUID: "c", Distance: 1},
		{GUID: "d", Distance: 2},
	}, nodes)

	// The upstream walk keeps the opposite sign.
	upstream := map[string][]string{"leaf": {"mid"}, "mid": {"root"}}
	nodes, depthReached, _, err = walkLineage(context.Background(), "leaf", 2, 100, -1, fakeLineageFetcher(upstream))
	require.NoError(t, err)
	require.Equal(t, int32(2), depthReached)
	require.Equal(t, []lineageWalkNode{
		{GUID: "leaf", Distance: 0},
		{GUID: "mid", Distance: -1},
		{GUID: "root", Distance: -2},
	}, nodes)
}

// A level with more peers than the node budget is cut at the budget and reported as
// truncated; the extra probe row is what makes the cut visible.
func TestWalkLineageReportsTruncation(t *testing.T) {
	t.Parallel()

	graph := map[string][]string{"a": {"b", "c", "d", "e"}}
	nodes, depthReached, truncated, err := walkLineage(context.Background(), "a", 3, 3, 1, fakeLineageFetcher(graph))
	require.NoError(t, err)
	require.True(t, truncated)
	require.Equal(t, int32(1), depthReached)
	require.Len(t, nodes, 3)
}

// An expired budget is a truncation, not a failure: the caller asked for a bounded
// answer and a partial one is still useful.
func TestWalkLineageReportsExhaustedBudgetAsTruncation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	nodes, depthReached, truncated, err := walkLineage(ctx, "a", 3, 100, 1, fakeLineageFetcher(nil))
	require.NoError(t, err)
	require.True(t, truncated)
	require.Zero(t, depthReached)
	require.Equal(t, []lineageWalkNode{{GUID: "a", Distance: 0}}, nodes)
}

// A read failure that is not the deadline is an error; the walk must not turn a
// broken query into a quietly partial answer.
func TestWalkLineagePropagatesReadErrors(t *testing.T) {
	t.Parallel()

	failure := context.DeadlineExceeded
	fetch := func(_ context.Context, _ []string, _ int) ([]string, error) {
		return nil, failure
	}
	_, _, _, err := walkLineage(context.Background(), "a", 2, 10, 1, fetch)
	require.ErrorIs(t, err, failure)
}

// Only the four object types a check can watch are assets; a reported statement or
// an unknown object is walked through but not reported. The type preference keeps
// an asset from being dropped when the registry holds several rows for its GUID.
func TestQualityLineageAssetType(t *testing.T) {
	t.Parallel()

	objectType, ok := qualityLineageAssetType([]storepb.MetaType{storepb.MetaType_APPLICATION_SQL, storepb.MetaType_VIEW})
	require.True(t, ok)
	require.Equal(t, storepb.MetaType_VIEW, objectType)

	_, ok = qualityLineageAssetType([]storepb.MetaType{storepb.MetaType_APPLICATION_SQL})
	require.False(t, ok)
	_, ok = qualityLineageAssetType(nil)
	require.False(t, ok)
}

// The target names a checkable object; a GUID alone is not enough and a database or
// column has no impact surface.
func TestQualityImpactTargetValidation(t *testing.T) {
	t.Parallel()

	_, err := qualityImpactTargetFromV1(&v1pb.QualityTargetKey{TargetGuid: " ", MetaType: v1pb.MetaType_TABLE})
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	_, err = qualityImpactTargetFromV1(&v1pb.QualityTargetKey{TargetGuid: "g", MetaType: v1pb.MetaType_DATABASE})
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	key, err := qualityImpactTargetFromV1(&v1pb.QualityTargetKey{TargetGuid: " g ", MetaType: v1pb.MetaType_VIEW})
	require.NoError(t, err)
	require.Equal(t, store.MetaGUIDKey{GUID: "g", ObjectType: storepb.MetaType_VIEW}, key)
}

// The requested depth, window and gap limit follow the documented bounds: zero means
// the default, and a value above the ceiling is coerced rather than rejected.
func TestQualityImpactRequestBounds(t *testing.T) {
	t.Parallel()

	require.Equal(t, qualityImpactDefaultDepth, qualityImpactDepth(0))
	require.Equal(t, qualityImpactDefaultDepth, qualityImpactDepth(-1))
	require.Equal(t, 1, qualityImpactDepth(1))
	require.Equal(t, qualityImpactMaxDepth, qualityImpactDepth(qualityImpactMaxDepth+1))

	require.Equal(t, qualityImpactDefaultWindowHours, qualityImpactWindowHours(0))
	require.Equal(t, 1, qualityImpactWindowHours(1))
	require.Equal(t, qualityImpactMaxWindowHours, qualityImpactWindowHours(qualityImpactMaxWindowHours+1))

	require.Equal(t, qualityConsumptionDefaultGaps, qualityConsumptionGapLimit(0))
	require.Equal(t, 1, qualityConsumptionGapLimit(1))
	require.Equal(t, qualityConsumptionMaxGaps, qualityConsumptionGapLimit(qualityConsumptionMaxGaps+1))
}

// The root-cause window ends at the reference instant, and a future instant is read
// as now instead of looking for evidence that cannot exist yet.
func TestQualityImpactReferenceTime(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	require.Equal(t, now, mustQualityImpactReferenceTime(t, nil, now))

	past := now.Add(-3 * time.Hour)
	require.Equal(t, past, mustQualityImpactReferenceTime(t, timestamppbOf(past), now))
	require.Equal(t, now, mustQualityImpactReferenceTime(t, timestamppbOf(now.Add(time.Hour)), now))
}

func timestamppbOf(value time.Time) *timestamppb.Timestamp {
	return timestamppb.New(value)
}

func mustQualityImpactReferenceTime(t *testing.T, requested *timestamppb.Timestamp, now time.Time) time.Time {
	t.Helper()
	return qualityImpactReferenceTime(requested, now)
}

// Failing checks outrank a structure change, the nearer source wins next, the more
// recent evidence wins after that, and the GUID makes the order deterministic.
func TestQualityRootCauseOrdering(t *testing.T) {
	t.Parallel()

	at := func(hour int) time.Time { return time.Date(2026, 11, 2, hour, 0, 0, 0, time.UTC) }
	evidence := func(hour int) *store.QualityFailureEvidence {
		return &store.QualityFailureEvidence{LatestFailureTime: at(hour)}
	}
	changedAt := at(12)

	candidates := []qualityRootCause{
		{key: store.MetaGUIDKey{GUID: "a"}, distance: -1, evidence: evidence(10)},
		{key: store.MetaGUIDKey{GUID: "b"}, distance: -1, structureChangedAt: &changedAt},
		{key: store.MetaGUIDKey{GUID: "c"}, distance: -2, evidence: evidence(9)},
		{key: store.MetaGUIDKey{GUID: "d"}, distance: -1, evidence: evidence(11)},
	}
	slices.SortStableFunc(candidates, compareQualityRootCauses)

	got := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		got = append(got, candidate.key.GUID)
	}
	require.Equal(t, []string{"d", "a", "c", "b"}, got)
}

// A display path mirrors the rule target's segment rule and never invents one for a
// GUID that does not name an object.
func TestQualityTargetPathFromGUID(t *testing.T) {
	t.Parallel()

	path := qualityTargetPathFromGUID("inst;shop;public;orders")
	require.Equal(t, &v1pb.QualityTargetPath{
		Instance: "inst", Database: "shop", Schema: "public", Object: "orders",
	}, path)

	path = qualityTargetPathFromGUID("inst;shop;orders")
	require.Equal(t, &v1pb.QualityTargetPath{
		Instance: "inst", Database: "shop", Object: "orders",
	}, path)

	require.Nil(t, qualityTargetPathFromGUID("inst;shop"))
}
