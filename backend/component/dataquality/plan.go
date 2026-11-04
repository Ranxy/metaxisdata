package dataquality

import (
	"slices"
	"time"

	"google.golang.org/protobuf/proto"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// CoverageProperty is one kind of monitoring a plan can require. The minimum
// coverage template maps a target's criticality to a set of properties; an intent
// can declare more, and a property counts as satisfied when at least one live
// rule implements it for the same scope.
//
// The numbers match the store and API enums so the three convert by cast.
type CoverageProperty int

const (
	CoverageVolume CoverageProperty = iota + 1
	CoverageFreshness
	CoverageUniqueness
	CoverageSchemaStability
	CoverageNullRate
	CoverageAcceptedValues
	CoverageFormat
	CoverageRange
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

// RequirementSource says where a requirement came from: the criticality template
// or an explicit intent statement. A template requirement can be restated by the
// intent, which upgrades it to RequirementIntent.
type RequirementSource int

const (
	RequirementTemplate RequirementSource = iota + 1
	RequirementIntent
)

// RuleShape is the part of a live rule that plan derivation reads: its type and
// the column it watches (empty for a table-level rule).
type RuleShape struct {
	Type   storepb.QualityRuleType
	Column string
}

// PlanRequirement is one required property for one scope, and whether the live
// rules satisfy it. An exempted requirement stays visible but produces no gap.
type PlanRequirement struct {
	Property  CoverageProperty
	Column    string
	Source    RequirementSource
	Satisfied bool
	Exempted  bool
}

// Plan is the read-only plan projection. It is a pure function of the contract's
// intent and the target's live rules, so it can be derived on every read without a
// second source of truth.
type Plan struct {
	State        PlanState
	Criticality  AssetCriticality
	Requirements []PlanRequirement
	GapCount     int
}

// ActiveIntent drops the exemptions that have expired by now. Expiry is a
// read-time rule, so the stored intent (and every published snapshot) keeps what
// was declared while the plan stops waiving the requirement.
func ActiveIntent(intent *storepb.QualityContractIntent, now time.Time) *storepb.QualityContractIntent {
	if intent == nil || len(intent.GetExemptions()) == 0 {
		return intent
	}
	nowUnixMilli := now.UnixMilli()
	active := make([]*storepb.QualityContractIntentExemption, 0, len(intent.GetExemptions()))
	for _, exemption := range intent.GetExemptions() {
		if expiresAt := exemption.GetExpiresAtUnixMs(); expiresAt > 0 && nowUnixMilli >= expiresAt {
			continue
		}
		active = append(active, exemption)
	}
	if len(active) == len(intent.GetExemptions()) {
		return intent
	}
	filtered, ok := proto.Clone(intent).(*storepb.QualityContractIntent)
	if !ok {
		return intent
	}
	filtered.Exemptions = active
	return filtered
}

// DerivePlan projects a target's plan from its contract intent and live rules.
//
// A contract with no explicit criticality is still treated as IMPORTANT: that is
// the S1 bridge, kept until criticality can be derived from lineage and dependents
// so publishing a contract does not silently drop every declared requirement.
func DerivePlan(hasContract bool, intent *storepb.QualityContractIntent, rules []RuleShape) Plan {
	if !hasContract {
		return Plan{State: PlanUnplanned, Criticality: CriticalityExperimental}
	}
	criticality := intentCriticality(intent)
	if criticality == 0 {
		criticality = CriticalityImportant
	}
	requirements := planRequirements(criticality)
	for _, statement := range intent.GetStatements() {
		property, ok := coveragePropertyFromStore(statement.GetProperty())
		if !ok {
			continue
		}
		requirements = upsertRequirement(requirements, PlanRequirement{
			Property: property,
			Column:   statement.GetColumn(),
			Source:   RequirementIntent,
		})
	}
	for _, exemption := range intent.GetExemptions() {
		property, ok := coveragePropertyFromStore(exemption.GetProperty())
		if !ok {
			continue
		}
		exemptRequirement(requirements, property, exemption.GetColumn())
	}
	plan := Plan{
		State:        PlanPlanned,
		Criticality:  criticality,
		Requirements: requirements,
	}
	for index := range plan.Requirements {
		requirement := &plan.Requirements[index]
		requirement.Satisfied = requirementSatisfied(*requirement, rules)
		if !requirement.Satisfied && !requirement.Exempted {
			plan.GapCount++
		}
	}
	return plan
}

// intentCriticality maps the stored intent criticality; zero means undeclared.
func intentCriticality(intent *storepb.QualityContractIntent) AssetCriticality {
	switch intent.GetCriticality() {
	case storepb.QualityContractCriticality_QUALITY_CONTRACT_CRITICALITY_EXPERIMENTAL:
		return CriticalityExperimental
	case storepb.QualityContractCriticality_QUALITY_CONTRACT_CRITICALITY_IMPORTANT:
		return CriticalityImportant
	case storepb.QualityContractCriticality_QUALITY_CONTRACT_CRITICALITY_CRITICAL:
		return CriticalityCritical
	default:
		return 0
	}
}

// planRequirements is the criticality template that applies before any explicit
// statement. An experimental target has no declaration, so it produces no gap:
// "no plan" must not masquerade as "complete coverage".
func planRequirements(criticality AssetCriticality) []PlanRequirement {
	var properties []CoverageProperty
	switch criticality {
	case CriticalityCritical:
		properties = []CoverageProperty{CoverageFreshness, CoverageVolume, CoverageUniqueness, CoverageSchemaStability}
	case CriticalityImportant:
		properties = []CoverageProperty{CoverageVolume, CoverageSchemaStability}
	}
	requirements := make([]PlanRequirement, 0, len(properties))
	for _, property := range properties {
		requirements = append(requirements, PlanRequirement{Property: property, Source: RequirementTemplate})
	}
	return requirements
}

// upsertRequirement adds a requirement or upgrades an existing one to the more
// explicit source. Identity is (property, column): a table-level promise and a
// column promise are different declarations.
func upsertRequirement(requirements []PlanRequirement, requirement PlanRequirement) []PlanRequirement {
	for index := range requirements {
		existing := &requirements[index]
		if existing.Property != requirement.Property || existing.Column != requirement.Column {
			continue
		}
		existing.Source = requirement.Source
		return requirements
	}
	return append(requirements, requirement)
}

// exemptRequirement marks the requirement an exemption waives. An exemption for a
// requirement the plan does not declare has no effect: it never adds coverage.
func exemptRequirement(requirements []PlanRequirement, property CoverageProperty, column string) {
	for index := range requirements {
		requirement := &requirements[index]
		if requirement.Property == property && requirement.Column == column {
			requirement.Exempted = true
		}
	}
}

// requirementSatisfied reports whether a live rule implements the required
// property for the requirement's scope. A column-scoped requirement only matches a
// rule on the same column; a table-level requirement matches a rule of that kind
// whatever its column, because the template names a property, not a column.
func requirementSatisfied(requirement PlanRequirement, rules []RuleShape) bool {
	for _, rule := range rules {
		if requirement.Column != "" && rule.Column != requirement.Column {
			continue
		}
		if slices.Contains(propertiesForRuleType(rule.Type), requirement.Property) {
			return true
		}
	}
	return false
}

// coveragePropertyFromStore maps the stored enum to the plan enum; the second
// result is false for an unspecified or unknown value.
func coveragePropertyFromStore(property storepb.QualityCoverageProperty) (CoverageProperty, bool) {
	if property < storepb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_VOLUME ||
		property > storepb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_RANGE {
		return 0, false
	}
	return CoverageProperty(property), true
}

// propertiesForRuleType maps a check type onto the declaration properties it
// implements. Several rule types can satisfy one property.
func propertiesForRuleType(ruleType storepb.QualityRuleType) []CoverageProperty {
	switch ruleType {
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE:
		return []CoverageProperty{CoverageVolume}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS:
		return []CoverageProperty{CoverageFreshness}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT:
		return []CoverageProperty{CoverageNullRate}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION:
		return []CoverageProperty{CoverageUniqueness}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_STANDARD_CODE:
		return []CoverageProperty{CoverageAcceptedValues, CoverageFormat}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX:
		return []CoverageProperty{CoverageFormat}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE:
		return []CoverageProperty{CoverageRange}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT:
		return []CoverageProperty{CoverageSchemaStability}
	default:
		return nil
	}
}
