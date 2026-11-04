package dataquality

import (
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// CoverageProperty is one kind of monitoring a plan can require. The minimum
// coverage template maps a target's criticality to a set of properties; a
// property counts as satisfied when at least one live rule implements it.
type CoverageProperty int

const (
	CoverageVolume CoverageProperty = iota + 1
	CoverageFreshness
	CoverageUniqueness
	CoverageSchemaStability
)

// AssetCriticality ranks a target for the autonomy policy. It is derived for
// display and policy input only; it is never written back to the contract.
type AssetCriticality int

const (
	CriticalityExperimental AssetCriticality = iota + 1
	CriticalityImportant
	CriticalityCritical
)

// PlanState says whether a target has a plan at all. A target with rules but no
// contract row is unplanned: its checks are real, but nothing records what it
// promised.
type PlanState int

const (
	PlanUnplanned PlanState = iota
	PlanPlanned
)

// PlanRequirement is one required property and whether the live rules satisfy it.
type PlanRequirement struct {
	Property  CoverageProperty
	Satisfied bool
}

// Plan is the read-only plan projection. It is a pure function of the contract's
// presence and the target's rule types, so it can be derived on every read
// without a second source of truth.
type Plan struct {
	State        PlanState
	Criticality  AssetCriticality
	Requirements []PlanRequirement
	GapCount     int
}

// DerivePlan projects a target's plan from its contract and live rule types.
//
// S1 bridge: the contract's presence is the only intent signal available until
// the intent fields land in S2, so a planned target is treated as IMPORTANT and
// gets the IMPORTANT minimum coverage template. Criticality from explicit intent
// or lineage replaces this without changing the projection shape.
func DerivePlan(hasContract bool, criticality AssetCriticality, ruleTypes []storepb.QualityRuleType) Plan {
	if !hasContract {
		return Plan{State: PlanUnplanned, Criticality: CriticalityExperimental}
	}
	if criticality == 0 {
		criticality = CriticalityImportant
	}
	requirements := minimumCoverage(criticality)
	satisfied := make(map[CoverageProperty]struct{}, len(ruleTypes))
	for _, ruleType := range ruleTypes {
		for _, property := range propertiesForRuleType(ruleType) {
			satisfied[property] = struct{}{}
		}
	}
	plan := Plan{
		State:        PlanPlanned,
		Criticality:  criticality,
		Requirements: make([]PlanRequirement, 0, len(requirements)),
	}
	for _, property := range requirements {
		_, ok := satisfied[property]
		plan.Requirements = append(plan.Requirements, PlanRequirement{Property: property, Satisfied: ok})
		if !ok {
			plan.GapCount++
		}
	}
	return plan
}

// minimumCoverage is the smallest property set a target must satisfy at a given
// criticality. An experimental target has no declaration, so it produces no gap:
// "no plan" must not masquerade as "complete coverage".
func minimumCoverage(criticality AssetCriticality) []CoverageProperty {
	switch criticality {
	case CriticalityCritical:
		return []CoverageProperty{CoverageFreshness, CoverageVolume, CoverageUniqueness, CoverageSchemaStability}
	case CriticalityImportant:
		return []CoverageProperty{CoverageVolume, CoverageSchemaStability}
	default:
		return nil
	}
}

// propertiesForRuleType maps a check type onto the declaration properties it
// implements. Several rule types can satisfy one property; a table-scoped
// property is satisfied by any check of that kind on the target (column scope
// arrives with the intent editor).
func propertiesForRuleType(ruleType storepb.QualityRuleType) []CoverageProperty {
	switch ruleType {
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE:
		return []CoverageProperty{CoverageVolume}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS:
		return []CoverageProperty{CoverageFreshness}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION:
		return []CoverageProperty{CoverageUniqueness}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT:
		return []CoverageProperty{CoverageSchemaStability}
	default:
		return nil
	}
}
