package v1

import (
	"context"
	"slices"
	"strings"

	"connectrpc.com/connect"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// ListQualityPlans reads the workspace's monitoring plans. The plan is a pure
// projection over the live rules and the contract rows, so this slice has no write
// path: a target with rules but no contract is UNPLANNED rather than silently
// treated as covered.
func (s *DataQualityService) ListQualityPlans(ctx context.Context, req *connect.Request[v1pb.ListQualityPlansRequest]) (*connect.Response[v1pb.ListQualityPlansResponse], error) {
	if _, err := s.workspaceID(ctx, req.Msg.GetParent()); err != nil {
		return nil, err
	}
	rules, err := s.store.ListQualityRules(ctx, &store.FindQualityRuleMessage{})
	if err != nil {
		return nil, err
	}
	contracts, err := s.store.ListQualityContracts(ctx)
	if err != nil {
		return nil, err
	}
	contracted := make(map[store.MetaGUIDKey]struct{}, len(contracts))
	for _, contract := range contracts {
		contracted[store.MetaGUIDKey{GUID: contract.TargetGUID, ObjectType: contract.TargetType}] = struct{}{}
	}

	type targetRules struct {
		guid  string
		types storepb.MetaType
		rules []*store.QualityRuleMessage
	}
	byTarget := make(map[store.MetaGUIDKey]*targetRules, len(rules))
	for _, rule := range rules {
		key := store.MetaGUIDKey{GUID: rule.TargetGUID, ObjectType: rule.TargetType}
		entry := byTarget[key]
		if entry == nil {
			entry = &targetRules{guid: rule.TargetGUID, types: rule.TargetType}
			byTarget[key] = entry
		}
		entry.rules = append(entry.rules, rule)
	}

	plans := make([]*v1pb.QualityPlan, 0, len(byTarget))
	for key, entry := range byTarget {
		if !qualityPlanMatchesScope(key.GUID, req.Msg) {
			continue
		}
		_, hasContract := contracted[key]
		plan := qualityPlanView(key.GUID, entry.types, entry.rules, hasContract)
		if search := strings.ToLower(strings.TrimSpace(req.Msg.GetSearch())); search != "" &&
			!strings.Contains(strings.ToLower(qualityPlanSearchText(plan)), search) {
			continue
		}
		plans = append(plans, plan)
	}
	slices.SortFunc(plans, func(left, right *v1pb.QualityPlan) int {
		return strings.Compare(left.GetTarget().GetMetaGuid(), right.GetTarget().GetMetaGuid())
	})

	offset, err := parseLimitAndOffset(&pageSize{
		token:        req.Msg.GetPageToken(),
		limit:        int(req.Msg.GetPageSize()),
		maximum:      200,
		defaultLimit: 50,
	})
	if err != nil {
		return nil, err
	}
	total := int32(len(plans))
	page, next, err := paginateInMemory(plans, offset)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1pb.ListQualityPlansResponse{
		Plans:         page,
		NextPageToken: next,
		TotalSize:     total,
	}), nil
}

// qualityPlanView derives one plan and renders it. The contract's presence is the
// S1 intent signal; explicit criticality and coverage declarations arrive with the
// plan write path.
func qualityPlanView(guid string, metaType storepb.MetaType, rules []*store.QualityRuleMessage, contracted bool) *v1pb.QualityPlan {
	ruleTypes := make([]storepb.QualityRuleType, 0, len(rules))
	for _, rule := range rules {
		ruleTypes = append(ruleTypes, rule.RuleType)
	}
	criticality := dataquality.CriticalityExperimental
	if contracted {
		criticality = dataquality.CriticalityImportant
	}
	plan := dataquality.DerivePlan(contracted, criticality, ruleTypes)
	requirements := make([]*v1pb.QualityCoverageRequirement, 0, len(plan.Requirements))
	for _, requirement := range plan.Requirements {
		requirements = append(requirements, &v1pb.QualityCoverageRequirement{
			Property:  convertPlanProperty(requirement.Property),
			Satisfied: requirement.Satisfied,
		})
	}
	return &v1pb.QualityPlan{
		Target: &v1pb.QualityTarget{
			MetaGuid: guid,
			MetaType: v1pb.MetaType(metaType),
			Path:     convertQualityTargetPathToV1(qualityContractTargetPath(rules)),
		},
		State:        convertPlanState(plan.State),
		Criticality:  convertPlanCriticality(plan.Criticality),
		CheckCount:   int32(len(rules)),
		Requirements: requirements,
		GapCount:     int32(plan.GapCount),
	}
}

// qualityPlanMatchesScope filters the plan page by the same instance → table
// path the target directory uses. Table is a prefix, matching the directory's
// table filter.
func qualityPlanMatchesScope(guid string, msg *v1pb.ListQualityPlansRequest) bool {
	parts := strings.Split(guid, ";")
	for len(parts) < 4 {
		parts = append(parts, "")
	}
	if instance := strings.TrimSpace(msg.GetInstance()); instance != "" && parts[0] != instance {
		return false
	}
	if database := strings.TrimSpace(msg.GetDatabase()); database != "" && parts[1] != database {
		return false
	}
	if schema := strings.TrimSpace(msg.GetSchema()); schema != "" && parts[2] != schema {
		return false
	}
	if table := strings.TrimSpace(msg.GetTable()); table != "" && !strings.HasPrefix(parts[3], table) {
		return false
	}
	return true
}

// qualityPlanSearchText is the searchable projection of a plan target. It uses the
// path snapshot stored with the rules, not a live registry read.
func qualityPlanSearchText(plan *v1pb.QualityPlan) string {
	path := plan.GetTarget().GetPath()
	if path == nil {
		return plan.GetTarget().GetMetaGuid()
	}
	return strings.Join([]string{path.GetInstance(), path.GetDatabase(), path.GetSchema(), path.GetObject()}, " ")
}

func convertPlanState(state dataquality.PlanState) v1pb.QualityPlanState {
	switch state {
	case dataquality.PlanPlanned:
		return v1pb.QualityPlanState_QUALITY_PLAN_STATE_PLANNED
	case dataquality.PlanUnplanned:
		return v1pb.QualityPlanState_QUALITY_PLAN_STATE_UNPLANNED
	default:
		return v1pb.QualityPlanState_QUALITY_PLAN_STATE_UNSPECIFIED
	}
}

func convertPlanCriticality(criticality dataquality.AssetCriticality) v1pb.QualityAssetCriticality {
	switch criticality {
	case dataquality.CriticalityCritical:
		return v1pb.QualityAssetCriticality_QUALITY_ASSET_CRITICALITY_CRITICAL
	case dataquality.CriticalityImportant:
		return v1pb.QualityAssetCriticality_QUALITY_ASSET_CRITICALITY_IMPORTANT
	case dataquality.CriticalityExperimental:
		return v1pb.QualityAssetCriticality_QUALITY_ASSET_CRITICALITY_EXPERIMENTAL
	default:
		return v1pb.QualityAssetCriticality_QUALITY_ASSET_CRITICALITY_UNSPECIFIED
	}
}

func convertPlanProperty(property dataquality.CoverageProperty) v1pb.QualityCoverageProperty {
	switch property {
	case dataquality.CoverageVolume:
		return v1pb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_VOLUME
	case dataquality.CoverageFreshness:
		return v1pb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_FRESHNESS
	case dataquality.CoverageUniqueness:
		return v1pb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_UNIQUENESS
	case dataquality.CoverageSchemaStability:
		return v1pb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_SCHEMA_STABILITY
	default:
		return v1pb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_UNSPECIFIED
	}
}
