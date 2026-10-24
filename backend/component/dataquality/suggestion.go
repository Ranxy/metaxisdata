package dataquality

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"slices"
	"strconv"
	"strings"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// A suggestion is a proposal about a fact, not an event. Two generation passes
// that propose the same fact must therefore converge on the same queue row: the
// queue holds one live proposal per identity, and regeneration refreshes it
// instead of appending a duplicate.
//
// The identity is derived here, not from the payload bytes, because the model may
// phrase the same proposal differently on every call:
//
//   - A label proposal is identified by the column it labels: a column has at most
//     one semantic label, so one live proposal per column is enough.
//   - A rule draft is identified by the rule type plus the columns it covers.
//     Thresholds, titles and rationales are deliberately not part of the identity:
//     "a uniqueness rule on email" is one proposal whether the model suggests a
//     zero or a one-row tolerance, and a reviewer should see it once. Config values
//     that make the proposal a different rule (the parent of a referential check,
//     the comparison of a cross-column check, an aggregate's function, custom SQL)
//     are folded in as discriminators.

const (
	suggestionSignatureLabelPrefix = "label"
	suggestionSignatureRulePrefix  = "rule"
)

// LabelSuggestionSignature is the proposal identity of a semantic-label suggestion.
func LabelSuggestionSignature(column string) string {
	return suggestionSignatureJoin(suggestionSignatureLabelPrefix, strings.TrimSpace(column))
}

// RuleSignature is the proposal identity of a rule. It is used both for drafts in
// the queue and for saved rules, so a draft that a rule already covers can be
// recognized as a duplicate.
func RuleSignature(ruleType storepb.QualityRuleType, config *storepb.QualityRuleConfig) string {
	columns := RuleConfigColumns(config)
	parts := []string{suggestionSignatureRulePrefix, ruleType.String(), suggestionSignatureColumns(columns)}
	parts = append(parts, ruleSignatureDiscriminators(ruleType, config)...)
	// A filter changes which rows the rule judges, so it makes two proposals of the
	// same type and columns genuinely different rules. Thresholds deliberately stay
	// out: a stricter bound on the same rule is the same proposal.
	if filter := config.GetFilter(); len(filter.GetClauses()) > 0 {
		parts = append(parts, "filter="+suggestionFilterDigest(filter))
	}
	// COUNT is the default unit; only an explicit PERCENT changes what the rule
	// means, so it participates in the identity.
	if config.GetThresholdMetric() == storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT {
		parts = append(parts, "metric=PERCENT")
	}
	return suggestionSignatureJoin(parts...)
}

// RuleConfigColumns lists the target columns a rule config refers to, in a stable
// order, so the identity does not depend on the order the caller named them.
func RuleConfigColumns(config *storepb.QualityRuleConfig) []string {
	if config == nil {
		return nil
	}
	switch {
	case config.GetNullPercent() != nil:
		return singleColumn(config.GetNullPercent().GetColumn())
	case config.GetUnique() != nil:
		return singleColumn(config.GetUnique().GetColumn())
	case config.GetUniqueCombination() != nil:
		return config.GetUniqueCombination().GetColumns()
	case config.GetAcceptedValues() != nil:
		return singleColumn(config.GetAcceptedValues().GetColumn())
	case config.GetRange() != nil:
		return singleColumn(config.GetRange().GetColumn())
	case config.GetRegex() != nil:
		return singleColumn(config.GetRegex().GetColumn())
	case config.GetStringLength() != nil:
		return singleColumn(config.GetStringLength().GetColumn())
	case config.GetReferentialIntegrity() != nil:
		return singleColumn(config.GetReferentialIntegrity().GetChildColumn())
	case config.GetCrossColumn() != nil:
		return twoColumns(config.GetCrossColumn().GetLeftColumn(), config.GetCrossColumn().GetRightColumn())
	case config.GetFreshness() != nil:
		return singleColumn(config.GetFreshness().GetTimestampColumn())
	case config.GetCelExpression() != nil:
		return config.GetCelExpression().GetColumns()
	case config.GetGroupCheck() != nil:
		return config.GetGroupCheck().GetColumns()
	case config.GetAggregate() != nil:
		return singleColumn(config.GetAggregate().GetColumn())
	case config.GetCrossTable() != nil:
		return twoColumns(config.GetCrossTable().GetLeftColumn(), config.GetCrossTable().GetRightColumn())
	case config.GetRowCount() != nil,
		config.GetRowCountChange() != nil,
		config.GetCustomSql() != nil,
		config.GetSchemaDrift() != nil:
		// Table-level rules cover no individual column.
		return nil
	default:
		return nil
	}
}

// ruleSignatureDiscriminators adds the config values that make two proposals of
// the same type and columns genuinely different rules.
func ruleSignatureDiscriminators(ruleType storepb.QualityRuleType, config *storepb.QualityRuleConfig) []string {
	if config == nil {
		return nil
	}
	switch ruleType {
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY:
		referential := config.GetReferentialIntegrity()
		if referential == nil {
			return nil
		}
		return []string{
			"parent=" + suggestionSignatureEscape(referential.GetParentTable()) + "." +
				suggestionSignatureEscape(referential.GetParentColumn()),
		}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_COLUMN:
		cross := config.GetCrossColumn()
		if cross == nil {
			return nil
		}
		// A cross-column comparison is directional: "a > b" and "b > a" are
		// different rules, so the left/right roles survive the sorted column set.
		parts := []string{
			"left=" + suggestionSignatureEscape(cross.GetLeftColumn()),
			"right=" + suggestionSignatureEscape(cross.GetRightColumn()),
		}
		if cross.GetOp() != storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_UNSPECIFIED {
			parts = append(parts, "op="+cross.GetOp().String())
		}
		return parts
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE:
		aggregate := config.GetAggregate()
		if aggregate == nil {
			return nil
		}
		parts := []string{"fn=" + aggregate.GetFunction().String()}
		if aggregate.GetFunction() == storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_PERCENTILE {
			parts[0] += ":" + strconv.FormatFloat(aggregate.GetPercentile(), 'g', -1, 64)
		}
		return parts
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE:
		cross := config.GetCrossTable()
		if cross == nil {
			return nil
		}
		return []string{
			"right=" + suggestionSignatureEscape(cross.GetRightTable()),
			"left_fn=" + cross.GetLeftFunction().String(),
			"right_fn=" + cross.GetRightFunction().String(),
			// The two sides of a reconciliation are not interchangeable.
			"left_col=" + suggestionSignatureEscape(cross.GetLeftColumn()),
			"right_col=" + suggestionSignatureEscape(cross.GetRightColumn()),
		}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE:
		unique := config.GetUnique()
		if unique == nil || !unique.GetCountNulls() {
			return nil
		}
		// Counting NULLs as a value is a different rule, not a threshold variant.
		return []string{"count_nulls=true"}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION:
		combination := config.GetUniqueCombination()
		if combination == nil || !combination.GetAllColumns() {
			return nil
		}
		return []string{"all_columns=true"}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES:
		accepted := config.GetAcceptedValues()
		if accepted == nil {
			return nil
		}
		values := append([]string(nil), accepted.GetValues()...)
		slices.Sort(values)
		return []string{
			"values=" + suggestionSignatureHash(strings.Join(slices.Compact(values), "\x00")),
			"allow_null=" + strconv.FormatBool(accepted.GetAllowNull()),
		}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX:
		regex := config.GetRegex()
		if regex == nil {
			return nil
		}
		return []string{"pattern=" + suggestionSignatureHash(regex.GetPattern())}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT:
		drift := config.GetSchemaDrift()
		if drift == nil {
			return nil
		}
		return []string{"expected=" + suggestionSignatureHash(schemaDriftIdentitySnapshot(drift))}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL:
		custom := config.GetCustomSql()
		if custom == nil {
			return nil
		}
		return []string{"sql=" + suggestionSignatureHash(custom.GetSql())}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION:
		cel := config.GetCelExpression()
		if cel == nil {
			return nil
		}
		return []string{"expr=" + suggestionSignatureHash(cel.GetExpression())}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED:
		return nil
	default:
		return nil
	}
}

// suggestionSignatureColumns renders a column set as one canonical segment: sorted,
// de-duplicated and escaped.
func suggestionSignatureColumns(columns []string) string {
	normalized := normalizeSignatureColumns(columns)
	escaped := make([]string, 0, len(normalized))
	for _, column := range normalized {
		escaped = append(escaped, suggestionSignatureEscape(column))
	}
	return strings.Join(escaped, ",")
}

// normalizeSignatureColumns trims, drops blanks, de-duplicates and sorts columns.
func normalizeSignatureColumns(columns []string) []string {
	normalized := make([]string, 0, len(columns))
	for _, column := range columns {
		column = strings.TrimSpace(column)
		if column == "" {
			continue
		}
		normalized = append(normalized, column)
	}
	slices.Sort(normalized)
	return slices.Compact(normalized)
}

// suggestionSignatureJoin joins the segments with "|". Column sets are rendered as
// one segment and an empty set keeps its separator, so the boundary between the
// type and the columns stays unambiguous.
func suggestionSignatureJoin(parts ...string) string {
	return strings.Join(append([]string(nil), parts...), "|")
}

// suggestionSignatureEscape keeps separators and control characters out of the
// identity: column names are database identifiers, and quoted identifiers are
// case-sensitive, so only escaping (not case folding) is applied.
func suggestionSignatureEscape(value string) string {
	return url.QueryEscape(strings.TrimSpace(value))
}

// suggestionSignatureHash is the stable digest used for free-form config text that
// must not be copied into a signature.
func suggestionSignatureHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// suggestionFilterDigest hashes the filter clauses in a canonical order, so the
// identity does not depend on the order the model wrote them in.
func suggestionFilterDigest(filter *storepb.QualityFilter) string {
	clauses := make([]string, 0, len(filter.GetClauses()))
	for _, clause := range filter.GetClauses() {
		if clause == nil {
			continue
		}
		values := append([]string(nil), clause.GetValues()...)
		slices.Sort(values)
		clauses = append(clauses, suggestionSignatureEscape(clause.GetColumn())+":"+
			clause.GetOp().String()+":"+suggestionSignatureEscape(strings.Join(values, ",")))
	}
	slices.Sort(clauses)
	return suggestionSignatureHash(strings.Join(clauses, ";"))
}

// schemaDriftIdentitySnapshot renders the schema contract a schema_drift rule pins. The
// expected column order only matters when the rule checks it, so an unordered
// snapshot folds order away.
func schemaDriftIdentitySnapshot(drift *storepb.SchemaDriftConfig) string {
	checkOrder := drift.GetCheckOrder()
	entries := make([]string, 0, len(drift.GetExpectedColumns()))
	for _, column := range drift.GetExpectedColumns() {
		if column == nil {
			continue
		}
		nullable := "unknown"
		if column.Nullable != nil {
			nullable = strconv.FormatBool(column.GetNullable())
		}
		entries = append(entries, strings.Join([]string{
			suggestionSignatureEscape(column.GetName()), column.GetType(), nullable,
		}, "\x00"))
	}
	if !checkOrder {
		slices.Sort(entries)
	}
	checkTypes := true
	if drift.CheckTypes != nil {
		checkTypes = drift.GetCheckTypes()
	}
	return strings.Join([]string{
		strconv.FormatBool(checkTypes),
		strconv.FormatBool(checkOrder),
		strconv.FormatBool(drift.GetAllowExtraColumns()),
		strings.Join(entries, "\x01"),
	}, "\x02")
}

func singleColumn(column string) []string {
	if strings.TrimSpace(column) == "" {
		return nil
	}
	return []string{column}
}

func twoColumns(left, right string) []string {
	columns := make([]string, 0, 2)
	if strings.TrimSpace(left) != "" {
		columns = append(columns, left)
	}
	if strings.TrimSpace(right) != "" {
		columns = append(columns, right)
	}
	return columns
}

// RuleSignatures is the set of identities the target's saved rules already cover.
// Generation skips a draft whose signature is in it: the rule exists, so proposing
// it again would only ask a reviewer to duplicate it.
func RuleSignatures(rules []*store.QualityRuleMessage) map[string]struct{} {
	signatures := make(map[string]struct{}, len(rules))
	for _, rule := range rules {
		signatures[RuleSignature(rule.RuleType, rule.Config)] = struct{}{}
	}
	return signatures
}

// SuggestionDecisionSuppressesRuleDraft reports whether a previous decision means a
// rule draft with the same identity should not be proposed again unless the caller
// explicitly asks for regeneration. Accepting a draft means the reviewer wants that
// rule; rejecting it means they do not. Either way, re-proposing it is noise.
func SuggestionDecisionSuppressesRuleDraft(decision storepb.QualitySuggestionStatus) bool {
	switch decision {
	case storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_ACCEPTED,
		storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_REJECTED:
		return true
	case storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_UNSPECIFIED,
		storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING,
		storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_SUPERSEDED:
		return false
	default:
		return false
	}
}

// SuggestionDecisionSuppressesLabel reports whether a previous decision means a
// label proposal for the same column should not come back. Only a rejection
// suppresses: an accepted label is a fact stored in column_semantic_label, and if
// somebody clears it the column is explicitly unlabeled again, so a later analysis
// may propose a label once more.
func SuggestionDecisionSuppressesLabel(decision storepb.QualitySuggestionStatus) bool {
	switch decision {
	case storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_REJECTED:
		return true
	case storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_UNSPECIFIED,
		storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING,
		storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_ACCEPTED,
		storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_SUPERSEDED:
		return false
	default:
		return false
	}
}
