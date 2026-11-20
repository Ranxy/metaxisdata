package dataquality

import (
	"slices"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality/agentcapability"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// AgentAdoptionRequest is one owner-requested adoption pass for a quality agent.
// It is the in-process contract between the agent runtime's adopt tool and the
// service that owns the rule-creation path; it is not an RPC message.
type AgentAdoptionRequest struct {
	// Session is the target work session or the owner conversation that made the
	// request. A target session scopes the pass to its target; a conversation may
	// name a target or adopt every pending draft of the agent.
	Session *store.QualityAgentSessionMessage
	// TargetGUID optionally narrows the pass to one target. A target session leaves
	// it empty (its own target is implicit); a conversation may set it.
	TargetGUID string
	// DraftIDs names the drafts the owner explicitly approved. Empty means "apply the
	// configured adoption guardrails to every pending draft in scope".
	DraftIDs []string
	// Wake is the session wake that made the request, for the audit correlation.
	Wake int32
}

// AgentAdoptionItem is one draft's outcome in an adoption report.
type AgentAdoptionItem struct {
	ResourceID string `json:"id"`
	Title      string `json:"title"`
	Outcome    string `json:"outcome"`
	Reason     string `json:"reason,omitempty"`
}

// AgentAdoptionReport is what one adoption pass did. The agent runtime renders it
// back to the model, so the counts are authoritative for the reply.
type AgentAdoptionReport struct {
	Adopted   int                 `json:"adopted"`
	Skipped   int                 `json:"skipped"`
	Failed    int                 `json:"failed"`
	Remaining int                 `json:"remaining"`
	Items     []AgentAdoptionItem `json:"items"`
}

// AgentAdoptionReady reports whether an agent can adopt at all: it is active, its
// adoption mode is not off, and it holds both the suggest and adopt capabilities.
// The same predicate gates the automatic pass, the explicit retry and the model
// tool, so the prompt and the executable capability cannot disagree.
func AgentAdoptionReady(agent *store.QualityAgentMessage) bool {
	return agent != nil &&
		agent.State == store.QualityAgentStateActive &&
		(agent.AdoptionMode == store.QualityAgentAdoptionModeActive ||
			agent.AdoptionMode == store.QualityAgentAdoptionModePaused) &&
		slices.Contains(agent.Capabilities, agentcapability.Adopt) &&
		slices.Contains(agent.Capabilities, agentcapability.Suggest)
}

// RuleTypeForDraftConfig maps the oneof arm of a model-drafted rule back to its
// rule type. The draft payload stores the config only, so every path that reasons
// about a draft (adoption, the target summary, the proposal list) needs this map.
func RuleTypeForDraftConfig(config *storepb.QualityRuleConfig) (storepb.QualityRuleType, bool) {
	if config == nil {
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED, false
	}
	switch config.GetConfig().(type) {
	case *storepb.QualityRuleConfig_RowCount:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, true
	case *storepb.QualityRuleConfig_RowCountChange:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE, true
	case *storepb.QualityRuleConfig_NullPercent:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, true
	case *storepb.QualityRuleConfig_Unique:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, true
	case *storepb.QualityRuleConfig_UniqueCombination:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION, true
	case *storepb.QualityRuleConfig_AcceptedValues:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES, true
	case *storepb.QualityRuleConfig_Range:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE, true
	case *storepb.QualityRuleConfig_Regex:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX, true
	case *storepb.QualityRuleConfig_StringLength:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH, true
	case *storepb.QualityRuleConfig_ReferentialIntegrity:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY, true
	case *storepb.QualityRuleConfig_CrossColumn:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_COLUMN, true
	case *storepb.QualityRuleConfig_Freshness:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS, true
	case *storepb.QualityRuleConfig_CustomSql:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL, true
	case *storepb.QualityRuleConfig_CelExpression:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION, true
	case *storepb.QualityRuleConfig_SchemaDrift:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT, true
	case *storepb.QualityRuleConfig_GroupCheck:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK, true
	case *storepb.QualityRuleConfig_Aggregate:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE, true
	case *storepb.QualityRuleConfig_CrossTable:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE, true
	case *storepb.QualityRuleConfig_StandardCode:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_STANDARD_CODE, true
	case *storepb.QualityRuleConfig_SegmentCheck:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_SEGMENT_CHECK, true
	default:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED, false
	}
}
