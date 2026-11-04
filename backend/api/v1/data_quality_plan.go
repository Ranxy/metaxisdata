package v1

import (
	"context"
	"slices"
	"strings"
	"time"

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
// qualityPlansFor derives the workspace's plans and applies the directory scope.
// Both list RPCs share it so the plan directory and the gap list can never
// disagree about a target's state.
func (s *DataQualityService) qualityPlansFor(ctx context.Context, instance, database, schema, table, search string) ([]*v1pb.QualityPlan, error) {
	rules, err := s.store.ListQualityRules(ctx, &store.FindQualityRuleMessage{})
	if err != nil {
		return nil, err
	}
	contracts, err := s.store.ListQualityContracts(ctx)
	if err != nil {
		return nil, err
	}
	plans := make([]*v1pb.QualityPlan, 0, len(rules))
	for _, entry := range qualityPlanEntries(rules, contracts) {
		if !qualityPlanMatchesScope(entry.guid, instance, database, schema, table) {
			continue
		}
		plan := qualityPlanView(entry.guid, entry.metaType, entry.rules, entry.contract)
		if search := strings.ToLower(strings.TrimSpace(search)); search != "" &&
			!strings.Contains(strings.ToLower(qualityPlanSearchText(plan)), search) {
			continue
		}
		plans = append(plans, plan)
	}
	slices.SortFunc(plans, func(left, right *v1pb.QualityPlan) int {
		return strings.Compare(left.GetTarget().GetMetaGuid(), right.GetTarget().GetMetaGuid())
	})

	return plans, nil
}

// qualityPlanEntry is one target's live rules and contract row.
type qualityPlanEntry struct {
	guid     string
	metaType storepb.MetaType
	rules    []*store.QualityRuleMessage
	contract *store.QualityContractMessage
}

// qualityPlanEntries joins the live rules and the contract rows by target. A target
// that declared a contract but has no checks yet is still a plan: it is exactly the
// state in which the declared gaps matter most.
func qualityPlanEntries(rules []*store.QualityRuleMessage, contracts []*store.QualityContractMessage) []qualityPlanEntry {
	byTarget := make(map[store.MetaGUIDKey]*qualityPlanEntry, len(rules)+len(contracts))
	order := make([]store.MetaGUIDKey, 0, len(rules)+len(contracts))
	entryFor := func(key store.MetaGUIDKey, guid string, metaType storepb.MetaType) *qualityPlanEntry {
		entry := byTarget[key]
		if entry == nil {
			entry = &qualityPlanEntry{guid: guid, metaType: metaType}
			byTarget[key] = entry
			order = append(order, key)
		}
		return entry
	}
	for _, rule := range rules {
		key := store.MetaGUIDKey{GUID: rule.TargetGUID, ObjectType: rule.TargetType}
		entry := entryFor(key, rule.TargetGUID, rule.TargetType)
		entry.rules = append(entry.rules, rule)
	}
	for _, contract := range contracts {
		key := store.MetaGUIDKey{GUID: contract.TargetGUID, ObjectType: contract.TargetType}
		entryFor(key, contract.TargetGUID, contract.TargetType).contract = contract
	}
	entries := make([]qualityPlanEntry, 0, len(order))
	for _, key := range order {
		entries = append(entries, *byTarget[key])
	}
	return entries
}

func (s *DataQualityService) ListQualityPlans(ctx context.Context, req *connect.Request[v1pb.ListQualityPlansRequest]) (*connect.Response[v1pb.ListQualityPlansResponse], error) {
	if _, err := s.workspaceID(ctx, req.Msg.GetParent()); err != nil {
		return nil, err
	}
	plans, err := s.qualityPlansFor(ctx, req.Msg.GetInstance(), req.Msg.GetDatabase(), req.Msg.GetSchema(), req.Msg.GetTable(), req.Msg.GetSearch())
	if err != nil {
		return nil, err
	}
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

// ListQualityCoverageGaps flattens the same plan projection to the unsatisfied
// declared properties. It reads exactly like ListQualityPlans and writes nothing.
func (s *DataQualityService) ListQualityCoverageGaps(ctx context.Context, req *connect.Request[v1pb.ListQualityCoverageGapsRequest]) (*connect.Response[v1pb.ListQualityCoverageGapsResponse], error) {
	if _, err := s.workspaceID(ctx, req.Msg.GetParent()); err != nil {
		return nil, err
	}
	plans, err := s.qualityPlansFor(ctx, req.Msg.GetInstance(), req.Msg.GetDatabase(), req.Msg.GetSchema(), req.Msg.GetTable(), req.Msg.GetSearch())
	if err != nil {
		return nil, err
	}
	gaps := qualityCoverageGapsFromPlans(plans)

	offset, err := parseLimitAndOffset(&pageSize{
		token:        req.Msg.GetPageToken(),
		limit:        int(req.Msg.GetPageSize()),
		maximum:      200,
		defaultLimit: 50,
	})
	if err != nil {
		return nil, err
	}
	total := int32(len(gaps))
	page, next, err := paginateInMemory(gaps, offset)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1pb.ListQualityCoverageGapsResponse{
		Gaps:          page,
		NextPageToken: next,
		TotalSize:     total,
	}), nil
}

// qualityCoverageGapsFromPlans flattens the plans to the requirements no enabled
// check satisfies. An exempted requirement is not a gap: the declaration waived it,
// and the plan's gap count already excludes it.
func qualityCoverageGapsFromPlans(plans []*v1pb.QualityPlan) []*v1pb.QualityCoverageGap {
	gaps := make([]*v1pb.QualityCoverageGap, 0)
	for _, plan := range plans {
		for _, requirement := range plan.GetRequirements() {
			if requirement.GetSatisfied() || requirement.GetExempted() {
				continue
			}
			gaps = append(gaps, &v1pb.QualityCoverageGap{
				Target:      plan.GetTarget(),
				Property:    requirement.GetProperty(),
				Criticality: plan.GetCriticality(),
				Column:      requirement.GetColumn(),
			})
		}
	}
	return gaps
}

// qualityPlanView derives one plan and renders it. The contract's intent decides
// the criticality and the explicit statements; without an intent the presence of a
// contract is still the S1 bridge to IMPORTANT.
func qualityPlanView(guid string, metaType storepb.MetaType, rules []*store.QualityRuleMessage, contract *store.QualityContractMessage) *v1pb.QualityPlan {
	shapes := qualityRuleShapes(rules)
	var settings *storepb.QualityContractSettings
	if contract != nil {
		settings = contract.Settings
	}
	intent := dataquality.ActiveIntent(settings.GetIntent(), time.Now())
	plan := dataquality.DerivePlan(contract != nil, intent, shapes)
	requirements := make([]*v1pb.QualityCoverageRequirement, 0, len(plan.Requirements))
	for _, requirement := range plan.Requirements {
		requirements = append(requirements, &v1pb.QualityCoverageRequirement{
			Property:  convertPlanProperty(requirement.Property),
			Satisfied: requirement.Satisfied,
			Column:    requirement.Column,
			Source:    convertPlanRequirementSource(requirement.Source),
			Exempted:  requirement.Exempted,
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
		Coverage:     coverageToV1(coverageFromRequirements(plan.Requirements)),
	}
}

// qualityRuleShapes is the part of the live rules plan derivation reads. A paused
// rule is live but deliberately not running, so it does not satisfy the
// requirement it would otherwise cover.
func qualityRuleShapes(rules []*store.QualityRuleMessage) []dataquality.RuleShape {
	shapes := make([]dataquality.RuleShape, 0, len(rules))
	for _, rule := range rules {
		if rule.State != storepb.QualityRuleState_QUALITY_RULE_STATE_UNSPECIFIED &&
			rule.State != storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED {
			continue
		}
		shapes = append(shapes, dataquality.RuleShape{
			Type:    rule.RuleType,
			Column:  dataquality.ConfigColumn(rule.Config),
			Enabled: true,
		})
	}
	return shapes
}

// qualityContractPlanSnapshot builds the plan payload a publish records: the
// coverage the plan declares at publish time plus the target's schedule projection.
// It is the same derivation the read view renders, so a published version can never
// promise something the plan card did not show.
func qualityContractPlanSnapshot(intent *storepb.QualityContractIntent, rules []*store.QualityRuleMessage, schedule *store.QualityTargetScheduleMessage) *storepb.QualityContractPlan {
	derived := dataquality.DerivePlan(true, intent, qualityRuleShapes(rules))
	payload := &storepb.QualityContractPlan{
		Coverage: coverageFromRequirements(derived.Requirements),
	}
	if schedule != nil {
		payload.Cadence = &storepb.QualityContractPlanCadence{
			IntervalSeconds: int64(schedule.ScheduleInterval / time.Second),
			Cron:            schedule.Cron,
			Timezone:        schedule.TimeZone,
		}
	}
	return payload
}

// coverageFromRequirements renders the declared coverage the plan requires. The
// plan payload and the read projection share it so the two cannot disagree.
func coverageFromRequirements(requirements []dataquality.PlanRequirement) []*storepb.QualityContractPlanCoverage {
	coverage := make([]*storepb.QualityContractPlanCoverage, 0, len(requirements))
	for _, requirement := range requirements {
		coverage = append(coverage, &storepb.QualityContractPlanCoverage{
			Property:  storepb.QualityCoverageProperty(requirement.Property),
			Column:    requirement.Column,
			MinChecks: 1,
			Source:    qualityContractPlanSource(requirement.Source),
		})
	}
	return coverage
}

// coverageToV1 renders stored declarations for the API.
func coverageToV1(coverage []*storepb.QualityContractPlanCoverage) []*v1pb.QualityPlanCoverage {
	rendered := make([]*v1pb.QualityPlanCoverage, 0, len(coverage))
	for _, declaration := range coverage {
		rendered = append(rendered, &v1pb.QualityPlanCoverage{
			Property:  v1pb.QualityCoverageProperty(declaration.GetProperty()),
			Column:    declaration.GetColumn(),
			MinChecks: declaration.GetMinChecks(),
			Source:    v1pb.QualityCoverageSource(declaration.GetSource()),
		})
	}
	return rendered
}

// convertQualityContractPlanToV1 renders a published plan payload. It carries only
// the declarations and cadence: the target, state and gap projection belong to the
// live view, never to a stored version.
func convertQualityContractPlanToV1(plan *storepb.QualityContractPlan) *v1pb.QualityPlan {
	if plan == nil {
		return nil
	}
	rendered := &v1pb.QualityPlan{Coverage: coverageToV1(plan.GetCoverage())}
	if cadence := plan.GetCadence(); cadence != nil {
		rendered.Cadence = &v1pb.QualityPlanCadence{
			IntervalSeconds: cadence.GetIntervalSeconds(),
			Cron:            cadence.GetCron(),
			Timezone:        cadence.GetTimezone(),
		}
	}
	return rendered
}

// qualityContractPlanSource maps a plan requirement's source onto the stored
// declaration source.
func qualityContractPlanSource(source dataquality.RequirementSource) storepb.QualityCoverageSource {
	switch source {
	case dataquality.RequirementTemplate:
		return storepb.QualityCoverageSource_QUALITY_COVERAGE_SOURCE_TEMPLATE
	case dataquality.RequirementIntent:
		return storepb.QualityCoverageSource_QUALITY_COVERAGE_SOURCE_INTENT
	default:
		return storepb.QualityCoverageSource_QUALITY_COVERAGE_SOURCE_UNSPECIFIED
	}
}

// qualityPlanMatchesScope filters the plan page by the same instance → table
// path the target directory uses. Table is a prefix, matching the directory's
// table filter.
func qualityPlanMatchesScope(guid, instance, database, schema, table string) bool {
	parts := strings.Split(guid, ";")
	for len(parts) < 4 {
		parts = append(parts, "")
	}
	if instance := strings.TrimSpace(instance); instance != "" && parts[0] != instance {
		return false
	}
	if database := strings.TrimSpace(database); database != "" && parts[1] != database {
		return false
	}
	if schema := strings.TrimSpace(schema); schema != "" && parts[2] != schema {
		return false
	}
	if table := strings.TrimSpace(table); table != "" && !strings.HasPrefix(parts[3], table) {
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
	// The plan, store and API enums share their numbering.
	return v1pb.QualityCoverageProperty(property)
}

func convertPlanRequirementSource(source dataquality.RequirementSource) v1pb.QualityCoverageSource {
	switch source {
	case dataquality.RequirementTemplate:
		return v1pb.QualityCoverageSource_QUALITY_COVERAGE_SOURCE_TEMPLATE
	case dataquality.RequirementIntent:
		return v1pb.QualityCoverageSource_QUALITY_COVERAGE_SOURCE_INTENT
	default:
		return v1pb.QualityCoverageSource_QUALITY_COVERAGE_SOURCE_UNSPECIFIED
	}
}
