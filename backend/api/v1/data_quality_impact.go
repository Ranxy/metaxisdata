package v1

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// The impact read's bounds. They are deliberately small: the answer is a summary a
// reviewer acts on, not the whole graph, and every one of them is reported as a
// truncation rather than silently shortening the list.
const (
	// qualityImpactDefaultDepth is how many levels downstream are walked when the
	// caller asks for none; qualityImpactMaxDepth is the upper bound.
	qualityImpactDefaultDepth = 2
	qualityImpactMaxDepth     = 3
	// qualityImpactMaxNodes bounds the downstream assets one answer lists.
	qualityImpactMaxNodes = 100
	// qualityImpactUpstreamDepth is the upstream walk behind the root causes. Two
	// levels is the range in which a failure is still plausibly explained by the
	// source; deeper walks mostly find unrelated noise.
	qualityImpactUpstreamDepth = 2
	// qualityImpactMaxUpstreamNodes bounds that walk.
	qualityImpactMaxUpstreamNodes = 50
	// qualityImpactMaxRootCauses is how many candidates the answer returns.
	qualityImpactMaxRootCauses = 20
	// qualityImpactRecentExecutions is how many failing execution links one
	// candidate carries.
	qualityImpactRecentExecutions = 3
	// qualityImpactDefaultWindowHours and qualityImpactMaxWindowHours bound the
	// root-cause window ending at the reference instant.
	qualityImpactDefaultWindowHours = 24
	qualityImpactMaxWindowHours     = 168
	// qualityImpactWalkBudget bounds the wall clock of both lineage walks. Hitting
	// it is reported as a truncation: a partial answer is still useful.
	qualityImpactWalkBudget = 5 * time.Second
	// qualityConsumptionDefaultGaps and qualityConsumptionMaxGaps bound how many
	// unmonitored consumed assets one answer lists. The counts always cover the
	// whole workspace.
	qualityConsumptionDefaultGaps = 20
	qualityConsumptionMaxGaps     = 100
)

// GetQualityTargetImpact explains a target's quality state by its lineage: what its
// data reaches downstream, and which upstream targets failed or changed structure
// in the window before a reference instant. Both are bounded walks over the
// recorded lineage, so an empty answer means "nothing is recorded", not "nothing
// consumes it".
func (s *DataQualityService) GetQualityTargetImpact(ctx context.Context, req *connect.Request[v1pb.GetQualityTargetImpactRequest]) (*connect.Response[v1pb.GetQualityTargetImpactResponse], error) {
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	target, err := qualityImpactTargetFromV1(req.Msg.GetTarget())
	if err != nil {
		return nil, err
	}
	asOf := qualityImpactReferenceTime(req.Msg.GetAsOf(), time.Now().UTC())
	response, err := s.buildQualityTargetImpact(ctx, workspaceID, target,
		qualityImpactDepth(req.Msg.GetDepth()), asOf,
		time.Duration(qualityImpactWindowHours(req.Msg.GetWindowHours()))*time.Hour)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response), nil
}

// GetQualityConsumptionCoverage measures how much of the data other assets consume
// is watched: the assets another object's lineage reads, how many of them have an
// enabled check, and the unwatched ones with the most consumers.
func (s *DataQualityService) GetQualityConsumptionCoverage(ctx context.Context, req *connect.Request[v1pb.GetQualityConsumptionCoverageRequest]) (*connect.Response[v1pb.GetQualityConsumptionCoverageResponse], error) {
	if _, err := s.workspaceID(ctx, req.Msg.GetParent()); err != nil {
		return nil, err
	}
	coverage, err := s.store.GetQualityConsumptionCoverage(ctx, qualityConsumptionGapLimit(req.Msg.GetPageSize()))
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the consumption coverage")
	}
	response := &v1pb.GetQualityConsumptionCoverageResponse{
		ConsumedAssetCount:  coverage.ConsumedAssetCount,
		MonitoredAssetCount: coverage.MonitoredAssetCount,
		GapCount:            coverage.GapCount,
	}
	for _, gap := range coverage.Gaps {
		response.Gaps = append(response.Gaps, &v1pb.QualityConsumptionGap{
			Target: &v1pb.QualityTarget{
				MetaGuid: gap.GUID,
				MetaType: v1pb.MetaType(gap.ObjectType),
				Path:     qualityTargetPathFromGUID(gap.GUID),
			},
			DownstreamAssetCount: gap.DownstreamAssetCount,
		})
	}
	return connect.NewResponse(response), nil
}

// buildQualityTargetImpact assembles the impact answer. The report's target page
// calls it too, so the panel and the exported document cannot disagree about what
// an asset's lineage says.
func (s *DataQualityService) buildQualityTargetImpact(ctx context.Context, workspaceID string, target store.MetaGUIDKey, depth int, asOf time.Time, window time.Duration) (*v1pb.GetQualityTargetImpactResponse, error) {
	// The walks own the time budget; the annotation reads below are batched by the
	// node count and need no second deadline of their own.
	walkCtx, cancel := context.WithTimeout(ctx, qualityImpactWalkBudget)
	defer cancel()

	downstreamNodes, depthReached, downstreamTruncated, err := walkLineage(walkCtx, target.GUID, depth, qualityImpactMaxNodes, 1,
		func(ctx context.Context, guids []string, budget int) ([]string, error) {
			return s.store.ListDownstreamLineageGUIDs(ctx, guids, budget+1)
		})
	if err != nil {
		return nil, errors.Wrap(err, "failed to walk the target's downstream lineage")
	}
	upstreamNodes, _, upstreamTruncated, err := walkLineage(walkCtx, target.GUID, qualityImpactUpstreamDepth, qualityImpactMaxUpstreamNodes, -1,
		func(ctx context.Context, guids []string, budget int) ([]string, error) {
			return s.store.ListUpstreamLineageGUIDs(ctx, guids, budget+1)
		})
	if err != nil {
		return nil, errors.Wrap(err, "failed to walk the target's upstream lineage")
	}

	guids := make([]string, 0, len(downstreamNodes)+len(upstreamNodes))
	for _, nodes := range [][]lineageWalkNode{downstreamNodes, upstreamNodes} {
		for _, node := range nodes {
			guids = append(guids, node.GUID)
		}
	}
	types, err := s.qualityLineageObjectTypes(ctx, guids)
	if err != nil {
		return nil, err
	}
	downstreamAssets, omittedDownstream, err := s.qualityImpactAssets(ctx, workspaceID, downstreamNodes, types)
	if err != nil {
		return nil, err
	}
	windowStart := asOf.Add(-window)
	rootCauses, omittedRootCauses, err := s.qualityRootCauses(ctx, workspaceID, upstreamNodes, types, windowStart, asOf)
	if err != nil {
		return nil, err
	}
	return &v1pb.GetQualityTargetImpactResponse{
		DownstreamAssets:       downstreamAssets,
		RootCauses:             rootCauses,
		DepthReached:           depthReached,
		DownstreamTruncated:    downstreamTruncated,
		OmittedDownstreamCount: omittedDownstream,
		UpstreamTruncated:      upstreamTruncated,
		OmittedRootCauseCount:  omittedRootCauses,
		WindowStart:            timestamppb.New(windowStart),
		WindowEnd:              timestamppb.New(asOf),
	}, nil
}

// lineageWalkNode is one object a bounded lineage walk reached, with its signed
// distance from the root: positive downstream, negative upstream.
type lineageWalkNode struct {
	GUID     string
	Distance int32
}

// lineageLevelFetcher returns the peers of one level, at most budget+1 of them.
// The extra row is the probe that tells the walk whether the level was cut short.
type lineageLevelFetcher func(ctx context.Context, guids []string, budget int) ([]string, error)

// walkLineage walks one direction of the recorded lineage breadth first, one level
// per fetcher call. step is the distance a level adds: +1 downstream, -1 upstream.
//
// The walk reports a truncation instead of failing when its node budget or the
// context's deadline cuts it short: the caller asked for a bounded answer and a
// partial one is still useful. A read error that is not the deadline is still an
// error.
func walkLineage(ctx context.Context, root string, depth, maxNodes int, step int32, fetch lineageLevelFetcher) ([]lineageWalkNode, int32, bool, error) {
	nodes := []lineageWalkNode{{GUID: root}}
	visited := map[string]struct{}{root: {}}
	frontier := []string{root}
	var depthReached int32

	for level := 1; level <= depth; level++ {
		if len(frontier) == 0 || ctx.Err() != nil {
			return nodes, depthReached, ctx.Err() != nil, nil //nolint:nilerr // an exhausted budget is a truncation
		}
		remaining := maxNodes - len(nodes)
		if remaining <= 0 {
			return nodes, depthReached, true, nil
		}
		peers, err := fetch(ctx, frontier, remaining)
		if err != nil {
			if ctx.Err() != nil {
				return nodes, depthReached, true, nil //nolint:nilerr // an exhausted budget is a truncation
			}
			return nil, 0, false, err
		}
		truncated := false
		if len(peers) > remaining {
			truncated = true
			peers = peers[:remaining]
		}
		var next []string
		for _, peer := range peers {
			if _, ok := visited[peer]; ok {
				continue
			}
			visited[peer] = struct{}{}
			nodes = append(nodes, lineageWalkNode{GUID: peer, Distance: step * int32(level)})
			next = append(next, peer)
		}
		if len(next) == 0 {
			break
		}
		frontier = next
		depthReached = int32(level)
		if truncated {
			return nodes, depthReached, true, nil
		}
	}
	return nodes, depthReached, false, nil
}

// qualityLineageObjectTypes resolves the registry type of every walked GUID in one
// read. A GUID the registry does not know (an external dataset, an object that was
// deleted) is absent, and the caller counts it as an omitted asset rather than
// inventing a type for it.
func (s *DataQualityService) qualityLineageObjectTypes(ctx context.Context, guids []string) (map[string][]storepb.MetaType, error) {
	types := make(map[string][]storepb.MetaType, len(guids))
	if len(guids) == 0 {
		return types, nil
	}
	digests, err := s.store.ListMetaRegistryResourceDigest(ctx, &store.FindMetaRegistryResourceMessage{GUIDs: &guids})
	if err != nil {
		return nil, errors.Wrap(err, "failed to resolve the lineage objects' types")
	}
	for _, digest := range digests {
		types[digest.GUID] = append(types[digest.GUID], digest.ObjectType)
	}
	return types, nil
}

// qualityLineageAssetType picks the object type to report for a walked GUID.
// Only the four object types a check can watch are reported: a reported statement
// or an external dataset is walked through but is not an asset this module can
// monitor. A GUID that the registry knows under several types prefers the most
// specific of the four so an affected asset is not dropped.
func qualityLineageAssetType(types []storepb.MetaType) (storepb.MetaType, bool) {
	for _, candidate := range []storepb.MetaType{
		storepb.MetaType_TABLE,
		storepb.MetaType_EXTERNAL_TABLE,
		storepb.MetaType_VIEW,
		storepb.MetaType_MATERIALIZED_VIEW,
	} {
		if slices.Contains(types, candidate) {
			return candidate, true
		}
	}
	return storepb.MetaType_UNSPECIFIED, false
}

// qualityImpactAssets turns the walked downstream nodes into the answer's asset
// rows, annotating each with the same exact-target selection the metadata browser
// reads. A row without a summary is unmonitored; the read never turns "no check"
// into a verdict.
func (s *DataQualityService) qualityImpactAssets(ctx context.Context, workspaceID string, nodes []lineageWalkNode, types map[string][]storepb.MetaType) ([]*v1pb.QualityImpactAsset, int32, error) {
	type assetRef struct {
		key      store.MetaGUIDKey
		distance int32
	}
	refs := make([]assetRef, 0, len(nodes))
	var omitted int32
	for _, node := range nodes {
		if node.Distance <= 0 {
			continue
		}
		objectType, ok := qualityLineageAssetType(types[node.GUID])
		if !ok {
			omitted++
			continue
		}
		refs = append(refs, assetRef{
			key:      store.MetaGUIDKey{GUID: node.GUID, ObjectType: objectType},
			distance: node.Distance,
		})
	}
	if len(refs) == 0 {
		return nil, omitted, nil
	}

	keys := make([]store.MetaGUIDKey, 0, len(refs))
	for _, ref := range refs {
		keys = append(keys, ref.key)
	}
	targets, err := s.store.ListQualityTargets(ctx, &store.FindQualityTargetMessage{TargetKeys: keys})
	if err != nil {
		return nil, 0, errors.Wrap(err, "failed to read the downstream assets' quality state")
	}
	summaries := make(map[store.MetaGUIDKey]*v1pb.QualityTargetSummary, len(targets))
	for _, target := range targets {
		summaries[store.MetaGUIDKey{GUID: target.GUID, ObjectType: target.Type}] = convertQualityTargetSummary(workspaceID, target)
	}

	assets := make([]*v1pb.QualityImpactAsset, 0, len(refs))
	for _, ref := range refs {
		assets = append(assets, &v1pb.QualityImpactAsset{
			Target: &v1pb.QualityTarget{
				MetaGuid: ref.key.GUID,
				MetaType: v1pb.MetaType(ref.key.ObjectType),
				Path:     qualityTargetPathFromGUID(ref.key.GUID),
			},
			Distance: ref.distance,
			Summary:  summaries[ref.key],
		})
	}
	return assets, omitted, nil
}

// qualityRootCause is one upstream target and the evidence found against it, kept
// as a value so the answer can be sorted before it is converted.
type qualityRootCause struct {
	key                store.MetaGUIDKey
	distance           int32
	evidence           *store.QualityFailureEvidence
	structureChangedAt *time.Time
}

// evidenceTime is the newest moment the candidate has evidence for.
func (c qualityRootCause) evidenceTime() time.Time {
	var latest time.Time
	if c.evidence != nil {
		latest = c.evidence.LatestFailureTime
	}
	if c.structureChangedAt != nil && c.structureChangedAt.After(latest) {
		latest = *c.structureChangedAt
	}
	return latest
}

// qualityRootCauses reads the window's evidence against the walked upstream
// targets and assembles the candidates. A target with neither a failing check nor
// a structure change inside the window is not a candidate at all.
func (s *DataQualityService) qualityRootCauses(ctx context.Context, workspaceID string, nodes []lineageWalkNode, types map[string][]storepb.MetaType, from, to time.Time) ([]*v1pb.QualityRootCauseCandidate, int32, error) {
	candidates := make([]qualityRootCause, 0, len(nodes))
	for _, node := range nodes {
		if node.Distance >= 0 {
			continue
		}
		objectType, ok := qualityLineageAssetType(types[node.GUID])
		if !ok {
			continue
		}
		candidates = append(candidates, qualityRootCause{
			key:      store.MetaGUIDKey{GUID: node.GUID, ObjectType: objectType},
			distance: node.Distance,
		})
	}
	if len(candidates) == 0 {
		return nil, 0, nil
	}

	keys := make([]store.MetaGUIDKey, 0, len(candidates))
	for _, candidate := range candidates {
		keys = append(keys, candidate.key)
	}
	evidence, err := s.store.ListQualityTargetFailureEvidence(ctx, keys, from, to, qualityImpactRecentExecutions)
	if err != nil {
		return nil, 0, errors.Wrap(err, "failed to read the upstream targets' failing executions")
	}
	changes, err := s.store.ListMetaRegistrySchemaChangeTimes(ctx, keys, from, to)
	if err != nil {
		return nil, 0, errors.Wrap(err, "failed to read the upstream targets' structure changes")
	}

	found := make([]qualityRootCause, 0, len(candidates))
	for _, candidate := range candidates {
		candidate.evidence = evidence[candidate.key]
		if changedAt, ok := changes[candidate.key]; ok {
			changed := changedAt
			candidate.structureChangedAt = &changed
		}
		if candidate.evidence == nil && candidate.structureChangedAt == nil {
			continue
		}
		found = append(found, candidate)
	}
	slices.SortStableFunc(found, compareQualityRootCauses)
	var omitted int32
	if len(found) > qualityImpactMaxRootCauses {
		omitted = int32(len(found) - qualityImpactMaxRootCauses)
		found = found[:qualityImpactMaxRootCauses]
	}

	rootCauses := make([]*v1pb.QualityRootCauseCandidate, 0, len(found))
	for _, candidate := range found {
		converted := &v1pb.QualityRootCauseCandidate{
			Target: &v1pb.QualityTarget{
				MetaGuid: candidate.key.GUID,
				MetaType: v1pb.MetaType(candidate.key.ObjectType),
				Path:     qualityTargetPathFromGUID(candidate.key.GUID),
			},
			Distance:             candidate.distance,
			StructureChangedTime: timeToProtoTimestampPtr(candidate.structureChangedAt),
		}
		if evidence := candidate.evidence; evidence != nil {
			converted.FailingRuleCount = evidence.FailingRuleCount
			converted.FailingExecutionCount = evidence.FailingExecutionCount
			converted.LatestFailureTime = timeToProtoTimestampPtr(&evidence.LatestFailureTime)
			for _, executionID := range evidence.RecentExecutionIDs {
				converted.RecentFailingExecutions = append(converted.RecentFailingExecutions,
					formatQualityExecutionName(workspaceID, executionID))
			}
		}
		rootCauses = append(rootCauses, converted)
	}
	return rootCauses, omitted, nil
}

// compareQualityRootCauses orders candidates strongest first. A failing check
// outranks a structure change: a failed check is evidence the upstream data is
// wrong, while a structure change is only a possible explanation. Then the nearer
// source wins, then the more recent evidence, and the GUID breaks the tie so the
// order is deterministic.
func compareQualityRootCauses(a, b qualityRootCause) int {
	if (a.evidence != nil) != (b.evidence != nil) {
		if a.evidence != nil {
			return -1
		}
		return 1
	}
	// Distances are negative: the larger one is nearer to the target.
	if a.distance != b.distance {
		return cmp.Compare(b.distance, a.distance)
	}
	if result := b.evidenceTime().Compare(a.evidenceTime()); result != 0 {
		return result
	}
	return cmp.Compare(a.key.GUID, b.key.GUID)
}

// qualityImpactTargetFromV1 validates the requested target. The target does not
// have to be monitored or to still resolve — lineage it left behind is still
// walked — but it does have to name one of the four object types a check can
// watch, because a walk from a database or a column has no meaning here.
func qualityImpactTargetFromV1(target *v1pb.QualityTargetKey) (store.MetaGUIDKey, error) {
	guid := strings.TrimSpace(target.GetTargetGuid())
	if guid == "" {
		return store.MetaGUIDKey{}, connectErrorInvalid("target.target_guid is required")
	}
	metaType := storepb.MetaType(target.GetMetaType())
	if _, ok := qualityLineageAssetType([]storepb.MetaType{metaType}); !ok {
		return store.MetaGUIDKey{}, connectErrorInvalid("target.meta_type must be a table, external table, view or materialized view")
	}
	return store.MetaGUIDKey{GUID: guid, ObjectType: metaType}, nil
}

// qualityImpactReferenceTime clamps the window's end: absent means now, and a
// future instant is read as now rather than looking for evidence that cannot exist
// yet.
func qualityImpactReferenceTime(requested *timestamppb.Timestamp, now time.Time) time.Time {
	if requested == nil {
		return now
	}
	asOf := requested.AsTime().UTC()
	if asOf.After(now) {
		return now
	}
	return asOf
}

// qualityImpactDepth clamps the requested downstream depth to its bounds.
func qualityImpactDepth(requested int32) int {
	switch {
	case requested <= 0:
		return qualityImpactDefaultDepth
	case requested > qualityImpactMaxDepth:
		return qualityImpactMaxDepth
	default:
		return int(requested)
	}
}

// qualityImpactWindowHours clamps the requested root-cause window to its bounds.
func qualityImpactWindowHours(requested int32) int {
	switch {
	case requested <= 0:
		return qualityImpactDefaultWindowHours
	case requested > qualityImpactMaxWindowHours:
		return qualityImpactMaxWindowHours
	default:
		return int(requested)
	}
}

// qualityConsumptionGapLimit clamps how many gaps the coverage answer lists.
func qualityConsumptionGapLimit(requested int32) int {
	switch {
	case requested <= 0:
		return qualityConsumptionDefaultGaps
	case requested > qualityConsumptionMaxGaps:
		return qualityConsumptionMaxGaps
	default:
		return int(requested)
	}
}

// qualityTargetPathFromGUID names where an object lives, using the same segment
// rule the rule target resolution uses: instance, database, schema (only for a
// four-segment GUID) and the object name. It is a display path; the GUID remains
// the identity.
func qualityTargetPathFromGUID(guid string) *v1pb.QualityTargetPath {
	segments := common.SplitMetaGUID(guid)
	if len(segments) < 3 {
		return nil
	}
	path := &v1pb.QualityTargetPath{
		Instance: segments[0],
		Database: segments[1],
		Object:   segments[len(segments)-1],
	}
	if len(segments) == 4 {
		path.Schema = segments[2]
	}
	return path
}
