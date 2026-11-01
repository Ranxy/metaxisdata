package dataquality

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// The deterministic recommendation pass turns one table's stored column profiles
// into candidate checks. It is the onboarding path the other generation passes
// cannot serve: the natural-language pass starts from an intent and the label pass
// starts from a model-named semantic type, while this one reads what the data
// already looks like. It still only proposes: the result is a set of rule drafts
// that a reviewer decides on, exactly like every other generation pass.
//
// A threshold taken from an observation is a starting point, not a verdict. The
// alternative — the fixed defaults the label drafts carry — is often runnable but
// blind to the table, so this pass prefers an observed baseline with a tolerance and
// states the arithmetic in the rationale. The reviewer sees both the observation and
// the proposed bound, and the accepted rule is an ordinary rule whose later revisions
// are theirs.
//
// Everything here is a pure function of the registry metadata, the profiles, the
// labels and the clock: no model, no target-database connection, no store. The API
// handler resolves the target, reads the evidence, validates every candidate through
// the rule pipeline and reconciles the survivors with the review queue.

// RecommendVersion identifies the deterministic rules in this file. It is stamped on
// every queued draft as the suggestion's prompt version, so a reviewer can tell
// which generation pass and which revision of it produced a proposal.
const RecommendVersion = "dq.recommend.v1"

// The pass caps how much it proposes. A table should get a handful of checks to
// review, not one per column: the cap is what keeps a wide table's queue readable.
const (
	recommendMaxCandidates = 12
	// recommendRowCountFloorFraction is the share of the observed row count a table
	// must keep. Half is a deliberate loose bound: it catches a truncated load or an
	// empty table, not ordinary daily variance.
	recommendRowCountFloorFraction = 0.5
	// recommendNullRateFactor widens the observed null rate. The extra percentage
	// point keeps a zero-ish observation from producing a zero bound that any
	// single new NULL would fail.
	recommendNullRateFactor = 1.5
	recommendNullRateDelta  = 0.1
	recommendMaxNullRate    = 50.0
	// recommendUniqueMinRows is the smallest table a candidate-key uniqueness check is
	// proposed for; below it "every value is distinct" says nothing.
	recommendUniqueMinRows = 100
	// recommendRangeMinRows is the smallest table an observed range is proposed for.
	recommendRangeMinRows = 100
	// recommendRangeSlackFraction widens the observed range on both sides, so an
	// ordinary new value just outside today's extremes does not alert.
	recommendRangeSlackFraction = 0.1
	// recommendStringLengthFactor widens the observed longest value.
	recommendStringLengthFactor = 1.2
	// recommendFreshnessFactor multiplies the observed lag; a table that updates
	// every hour is monitored at two hours, not at fifteen minutes.
	recommendFreshnessFactor = 2
	// recommendFreshnessMinLag is the smallest freshness bound proposed.
	recommendFreshnessMinLag = time.Hour
	// recommendFreshnessMaxLag is the observed lag above which a table looks
	// historical or archived rather than late, and no freshness check is proposed.
	recommendFreshnessMaxLag = 30 * 24 * time.Hour
	// recommendDraftConfidence* rank the strength of each candidate. They are shown to
	// the reviewer; nothing is auto-accepted.
	recommendConfidenceSchema        = 1.0
	recommendConfidenceEnumeration   = 0.7
	recommendConfidenceNullRate      = 0.6
	recommendConfidenceCandidateKey  = 0.6
	recommendConfidenceFreshness     = 0.5
	recommendConfidenceRange         = 0.45
	recommendConfidenceStringLength  = 0.4
	recommendConfidenceRowCount      = 0.4
	recommendConfidenceDefaultFormat = 0.8
)

// Recommendation is one candidate check, ready to be queued as a PENDING rule
// draft. Signature is the proposal identity (RuleSignature); the API handler uses
// it to reconcile the candidate with saved rules and the review queue.
type Recommendation struct {
	RuleType   storepb.QualityRuleType
	Config     *storepb.QualityRuleConfig
	Title      string
	Rationale  string
	Severity   storepb.QualitySeverity
	Confidence float64
	Signature  string
}

// RecommendInput is the evidence the pass is allowed to read. Profiles and Labels
// are keyed by column name; Columns is the analysis scope in display order (empty
// means every column of the target). Metadata supplies foreign keys; a nil value
// simply yields no referential candidates.
type RecommendInput struct {
	Target   ResolvedTarget
	Metadata *storepb.StoredMetadata
	Profiles map[string]*storepb.ColumnProfile
	Labels   map[string]storepb.QualitySemanticType
	Columns  []string
	Language string
	Now      time.Time
}

// recommendColumn is one column of the scope with the facts the pass may use. A
// column without a profile only contributes schema-free candidates (none today), so
// the API refuses such a request before calling in.
type recommendColumn struct {
	info    ColumnInfo
	profile *storepb.ColumnProfile
	label   storepb.QualitySemanticType
	caps    columnCapabilities
}

// recommendSelection is how the pass spends its budget: table-level checks first,
// then referential integrity, then a bounded number of column checks per type. The
// per-type budgets are what stop a wide table from turning into twelve null-rate
// checks and losing the freshness/volume signal.
var recommendSelection = []struct {
	ruleType storepb.QualityRuleType
	limit    int
}{
	{storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, 1},
	{storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS, 1},
	{storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY, 2},
	{storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES, 3},
	{storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, 3},
	{storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, 2},
	{storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX, 2},
	{storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE, 2},
	{storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH, 2},
}

// RecommendationResult is what one recommendation pass produced. Recommendations is
// the bounded, ordered set the caller queues. SkippedColumns names the requested
// columns the pass could derive no check for, so the caller can explain an empty or
// thin result; a column whose candidate lost the per-type budget is not listed,
// because a check for it was derived.
type RecommendationResult struct {
	Recommendations []Recommendation
	SkippedColumns  []string
}

// RecommendChecks derives the candidate checks for one target from the evidence in
// input. The result is deterministic: the same inputs produce the same candidates,
// in the same order, with the same proposal identities.
func RecommendChecks(input RecommendInput) RecommendationResult {
	language := NormalizePromptLanguage(input.Language)
	now := input.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	columns := recommendScope(input)
	requested := recommendRequestedColumns(input)
	if len(columns) == 0 {
		return RecommendationResult{SkippedColumns: requested}
	}
	builder := recommendBuilder{target: input.Target, language: language, now: now}

	builder.addTableChecks(columns)
	// A relation without registry columns has no column-level checks to derive.
	if input.Target.MetaType == storepb.MetaType_TABLE ||
		input.Target.MetaType == storepb.MetaType_EXTERNAL_TABLE {
		builder.addColumnChecks(columns)
		builder.addReferentialChecks(input.Metadata)
	}

	// Skipped columns are the ones no candidate was derived for at all. Coverage is
	// computed from every derived candidate, before selection, so a column whose check
	// lost the per-type budget is not called "skipped".
	derived := make(map[string]struct{}, len(builder.candidates))
	for _, candidate := range builder.candidates {
		if column := ConfigColumn(candidate.Config); column != "" {
			derived[column] = struct{}{}
		}
	}
	var skipped []string
	for _, name := range requested {
		if _, ok := derived[name]; !ok {
			skipped = append(skipped, name)
		}
	}
	return RecommendationResult{Recommendations: builder.selectCandidates(), SkippedColumns: skipped}
}

// recommendRequestedColumns is the analysis scope as the caller named it: trimmed,
// de-duplicated and in request order (or the target's registry order when the request
// named none). It is the set the skipped-column summary is reported against.
func recommendRequestedColumns(input RecommendInput) []string {
	names := input.Columns
	if len(names) == 0 {
		names = input.Target.ColumnNames()
	}
	requested := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		requested = append(requested, name)
	}
	return requested
}

// recommendScope resolves the requested column names against the target, in request
// order, dropping names the target no longer has and de-duplicating repeats. A
// column the pass cannot classify is left out: no rule can run against it, so
// proposing one would only move the failure to the reviewer.
func recommendScope(input RecommendInput) []recommendColumn {
	names := input.Columns
	if len(names) == 0 {
		names = input.Target.ColumnNames()
	}
	columns := make([]recommendColumn, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		info, ok := input.Target.Column(name)
		if !ok || info.Kind == ColumnKindUnknown {
			continue
		}
		columns = append(columns, recommendColumn{
			info:    info,
			profile: input.Profiles[name],
			label:   input.Labels[name],
			caps:    capabilitiesFor(input.Target.Engine, info),
		})
	}
	return columns
}

// recommendCandidate is one candidate before selection. Candidates are appended per
// rule type in scope order, which is the order selection spends each type's budget in.
type recommendCandidate struct {
	Recommendation
}

// recommendBuilder collects candidates and turns them into the bounded, ordered
// result the caller queues.
type recommendBuilder struct {
	target     ResolvedTarget
	language   string
	now        time.Time
	candidates []recommendCandidate
}

// selectCandidates spends the budget by rule type: each type contributes at most its
// configured limit, in scope order, until the total cap is reached. A proposal
// identical to an earlier one (same RuleSignature) is dropped first, so a column
// never produces two drafts for one rule.
func (b *recommendBuilder) selectCandidates() []Recommendation {
	byType := make(map[storepb.QualityRuleType][]recommendCandidate, len(recommendSelection))
	seen := make(map[string]struct{}, len(b.candidates))
	for _, candidate := range b.candidates {
		if _, ok := seen[candidate.Signature]; ok {
			continue
		}
		seen[candidate.Signature] = struct{}{}
		byType[candidate.RuleType] = append(byType[candidate.RuleType], candidate)
	}
	selected := make([]Recommendation, 0, recommendMaxCandidates)
	for _, entry := range recommendSelection {
		taken := 0
		for _, candidate := range byType[entry.ruleType] {
			if taken >= entry.limit || len(selected) >= recommendMaxCandidates {
				break
			}
			selected = append(selected, candidate.Recommendation)
			taken++
		}
		if len(selected) >= recommendMaxCandidates {
			break
		}
	}
	return selected
}

// add records one candidate with the shared title/severity/identity wiring.
func (b *recommendBuilder) add(ruleType storepb.QualityRuleType, config *storepb.QualityRuleConfig, columnLabel string, confidence float64, rationale string) {
	if config == nil {
		return
	}
	b.candidates = append(b.candidates, recommendCandidate{
		Recommendation: Recommendation{
			RuleType:   ruleType,
			Config:     config,
			Title:      recommendTitle(columnLabel, ruleType, b.language),
			Rationale:  rationale,
			Severity:   storepb.QualitySeverity_QUALITY_SEVERITY_WARNING,
			Confidence: confidence,
			Signature:  RuleSignature(ruleType, config),
		},
	})
}

// addTableChecks proposes the checks that describe the relation as a whole: a volume
// floor and, when the data looks like a stream rather than an archive, a freshness
// bound. Both are allowed on views.
func (b *recommendBuilder) addTableChecks(columns []recommendColumn) {
	total, ok := recommendTableRows(columns)
	if !ok || total <= 0 {
		return
	}
	minimum := int64(math.Floor(float64(total) * recommendRowCountFloorFraction))
	if minimum < 1 {
		minimum = 1
	}
	b.add(storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		&storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCount{RowCount: &storepb.RowCountConfig{
			MinRows: proto.Int64(minimum),
		}}},
		recommendTableLabel(b.target),
		recommendConfidenceRowCount,
		recommendRationale(storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, b.language, recommendFacts{
			rows: total, minRows: minimum,
		}))

	freshness := recommendFreshnessCandidate(columns, b.now)
	if freshness == nil {
		return
	}
	bound := recommendFreshnessBound(max(recommendFreshnessFactor*freshness.lag, recommendFreshnessMinLag))
	b.add(storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS,
		&storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Freshness{Freshness: &storepb.FreshnessConfig{
			TimestampColumn: freshness.column,
			MaxLagSeconds:   int32(bound / time.Second),
		}}},
		freshness.column,
		recommendConfidenceFreshness,
		recommendRationale(storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS, b.language, recommendFacts{
			column: freshness.column, newest: freshness.observed.Format(time.RFC3339),
			lag: freshness.lag, bound: bound,
		}))
}

// addColumnChecks proposes the per-column checks the pass can defend from the
// profile. The order of the calls is the tie-break order of selection.
func (b *recommendBuilder) addColumnChecks(columns []recommendColumn) {
	for _, column := range columns {
		b.addAcceptedValues(column)
	}
	for _, column := range columns {
		b.addNullRate(column)
	}
	for _, column := range columns {
		b.addCandidateKey(column)
	}
	for _, column := range columns {
		b.addLabeledFormat(column)
	}
	for _, column := range columns {
		b.addRange(column)
	}
	for _, column := range columns {
		b.addStringLength(column)
	}
}

// addNullRate proposes a null-rate bound for a nullable column the profile observed
// NULLs in. A NOT NULL column cannot hold a NULL, so a check there would never fail;
// a column that is mostly NULL says more about the table than about its quality.
func (b *recommendBuilder) addNullRate(column recommendColumn) {
	profile := column.profile
	if !column.info.Nullable || profile == nil || profile.GetTotalRows() <= 0 || profile.GetNullCount() <= 0 {
		return
	}
	if column.caps.noNullTest || !column.caps.comparison {
		return
	}
	rate := float64(profile.GetNullCount()) / float64(profile.GetTotalRows()) * 100
	if rate > recommendMaxNullRate {
		return
	}
	threshold := rate * recommendNullRateFactor
	if threshold < rate+recommendNullRateDelta {
		threshold = rate + recommendNullRateDelta
	}
	if threshold > recommendMaxNullRate {
		threshold = recommendMaxNullRate
	}
	threshold = math.Ceil(threshold*100) / 100
	b.add(storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		&storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_NullPercent{NullPercent: &storepb.NullPercentConfig{
			Column: column.info.Name, MaxNullPct: proto.Float64(threshold),
		}}},
		column.info.Name,
		recommendConfidenceNullRate,
		recommendRationale(storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, b.language, recommendFacts{
			column: column.info.Name, rows: profile.GetTotalRows(), nullPct: rate, threshold: threshold,
		}))
}

// addAcceptedValues proposes an enumeration for a column whose exact distinct count
// fits the profile's frequent-value list. The exact distinct count is what makes the
// enumeration trustworthy: the profile keeps every distinct value it enumerated only
// when it saw exactly that many, so a mismatch means the list is incomplete and the
// candidate is dropped.
func (b *recommendBuilder) addAcceptedValues(column recommendColumn) {
	profile := column.profile
	if profile == nil || profile.GetDistinctEstimated() || profile.GetTotalRows() <= 0 {
		return
	}
	if column.info.Kind == ColumnKindBoolean || column.info.Kind == ColumnKindTemporal ||
		column.info.Kind == ColumnKindUnknown {
		return
	}
	if !column.caps.comparison || !column.caps.valueComparison {
		return
	}
	distinct := profile.GetDistinctCount()
	if distinct <= 0 || distinct > int64(profileMaxTopValues) {
		return
	}
	if len(profile.GetTopValues()) != int(distinct) {
		// The sample saw a different number of values than the table has, or an empty
		// string / an over-long value was dropped: the set is not the whole story.
		return
	}
	if profile.MaxLength != nil && profile.GetMaxLength() > int64(profileMaxValueBytes) {
		return
	}
	values := make([]string, 0, len(profile.GetTopValues()))
	for _, value := range profile.GetTopValues() {
		if value == nil || strings.TrimSpace(value.GetValue()) == "" {
			return
		}
		values = append(values, value.GetValue())
	}
	if len(values) == 0 {
		return
	}
	b.add(storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES,
		&storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_AcceptedValues{AcceptedValues: &storepb.AcceptedValuesConfig{
			Column:        column.info.Name,
			Values:        values,
			AllowNull:     profile.GetNullCount() > 0,
			FailThreshold: proto.Float64(0),
		}}},
		column.info.Name,
		recommendConfidenceEnumeration,
		recommendRationale(storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES, b.language, recommendFacts{
			column: column.info.Name, rows: profile.GetTotalRows(), distinct: distinct,
		}))
}

// addCandidateKey proposes a uniqueness check on a column the profile proves unique.
// The primary key is skipped on purpose: the engine already enforces it, so a check
// on it could never fail. A column with a label that promises identity is proposed
// too, because duplicates there are a finding, not a false alarm.
func (b *recommendBuilder) addCandidateKey(column recommendColumn) {
	if slices.Contains(b.target.KeyColumns, column.info.Name) {
		return
	}
	// A timestamp may legitimately repeat and an unclassifiable/binary column is not an
	// identifier: only a scalar or text column can be a candidate key.
	if column.info.Kind == ColumnKindBoolean || column.info.Kind == ColumnKindTemporal ||
		column.caps.binary || !column.caps.grouping {
		return
	}
	label := column.label
	labeledIdentity := label == storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_ID ||
		label == storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_BUSINESS_KEY
	profile := column.profile
	if !labeledIdentity {
		if profile == nil || profile.GetDistinctEstimated() ||
			profile.GetTotalRows() < recommendUniqueMinRows ||
			profile.GetNullCount() != 0 || profile.GetDistinctCount() != profile.GetTotalRows() {
			return
		}
	}
	b.add(storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		&storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Unique{Unique: &storepb.UniqueConfig{
			Column: column.info.Name, FailThreshold: proto.Float64(0),
		}}},
		column.info.Name,
		recommendConfidenceCandidateKey,
		recommendRationale(storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, b.language, recommendFacts{
			column: column.info.Name, rows: recommendRows(profile), label: label,
		}))
}

// addLabeledFormat proposes the conservative default pattern when a column carries a
// format-shaped semantic label. It deliberately does not guess a pattern from the
// profile's coarse value shapes: a regex the data cannot satisfy is worse than none.
func (b *recommendBuilder) addLabeledFormat(column recommendColumn) {
	pattern, ok := labelDefaultPatterns[column.label]
	if !ok || !column.caps.regex {
		return
	}
	b.add(storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
		&storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Regex{Regex: &storepb.RegexConfig{
			Column: column.info.Name, Pattern: pattern, FailThreshold: proto.Float64(0),
		}}},
		column.info.Name,
		recommendConfidenceDefaultFormat,
		recommendRationale(storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX, b.language, recommendFacts{
			column: column.info.Name, label: column.label,
		}))
}

// addRange proposes an observed numeric range widened by a tenth of the span. A
// constant column and a column whose values are already distinct (an identifier)
// are skipped: neither has a range worth monitoring.
func (b *recommendBuilder) addRange(column recommendColumn) {
	profile := column.profile
	if column.info.Kind != ColumnKindNumeric || !column.caps.extrema || profile == nil {
		return
	}
	if slices.Contains(b.target.KeyColumns, column.info.Name) ||
		profile.GetTotalRows() < recommendRangeMinRows ||
		profile.GetNullCount() == profile.GetTotalRows() {
		return
	}
	if profile.GetNullCount() == 0 && profile.GetDistinctCount() == profile.GetTotalRows() &&
		!profile.GetDistinctEstimated() {
		return
	}
	low, okLow := recommendParseNumber(profile.GetMinValue())
	high, okHigh := recommendParseNumber(profile.GetMaxValue())
	if !okLow || !okHigh || high <= low {
		return
	}
	slack := (high - low) * recommendRangeSlackFraction
	b.add(storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE,
		&storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Range{Range: &storepb.RangeConfig{
			Column:        column.info.Name,
			Min:           proto.String(recommendFormatNumber(low - slack)),
			Max:           proto.String(recommendFormatNumber(high + slack)),
			Inclusive:     true,
			FailThreshold: proto.Float64(0),
		}}},
		column.info.Name,
		recommendConfidenceRange,
		recommendRationale(storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE, b.language, recommendFacts{
			column: column.info.Name, rows: profile.GetTotalRows(),
			minValue: profile.GetMinValue(), maxValue: profile.GetMaxValue(),
		}))
}

// addStringLength proposes a length bound a fifth longer than the longest observed
// value, for a column the reviewer has labeled free text. An unlabeled short column's
// today-length says nothing about the length the data should have (a status code that
// gains a character is a different check), so the pass does not invent that bound.
func (b *recommendBuilder) addStringLength(column recommendColumn) {
	profile := column.profile
	if column.label != storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_FREE_TEXT {
		return
	}
	if column.info.Kind != ColumnKindString || column.caps.binary || !column.caps.length || profile == nil {
		return
	}
	if profile.MaxLength == nil || profile.GetMaxLength() <= 0 {
		return
	}
	longest := int64(math.Ceil(float64(profile.GetMaxLength()) * recommendStringLengthFactor))
	if longest < 1 {
		longest = 1
	}
	b.add(storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH,
		&storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_StringLength{StringLength: &storepb.StringLengthConfig{
			Column: column.info.Name, MaxLen: proto.Int64(longest), FailThreshold: proto.Float64(0),
		}}},
		column.info.Name,
		recommendConfidenceStringLength,
		recommendRationale(storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH, b.language, recommendFacts{
			column: column.info.Name, maxLength: profile.GetMaxLength(), thresholdLength: longest,
		}))
}

// addReferentialChecks proposes a referential-integrity check for each single-column
// foreign key that can be expressed inside the target's own container. A composite
// key is left alone: its columns are only meaningful together, and one rule per
// column would assert something the schema does not promise. The parent must live in
// the target's schema (or, on the MySQL family, its database), which is the same
// bound the rule compiler enforces.
func (b *recommendBuilder) addReferentialChecks(metadata *storepb.StoredMetadata) {
	if metadata == nil || b.target.MetaType != storepb.MetaType_TABLE {
		return
	}
	table := metadata.GetTableMetadata()
	if table == nil {
		return
	}
	for _, foreignKey := range table.GetForeignKeys() {
		if foreignKey == nil || len(foreignKey.GetColumns()) != 1 || len(foreignKey.GetReferencedColumns()) != 1 {
			continue
		}
		child := strings.TrimSpace(foreignKey.GetColumns()[0])
		parentColumn := strings.TrimSpace(foreignKey.GetReferencedColumns()[0])
		parentTable := strings.TrimSpace(foreignKey.GetReferencedTable())
		if child == "" || parentColumn == "" || parentTable == "" {
			continue
		}
		childInfo, ok := b.target.Column(child)
		if !ok || !capabilitiesFor(b.target.Engine, childInfo).comparison {
			continue
		}
		if !recommendParentInContainer(b.target, foreignKey.GetReferencedSchema()) {
			continue
		}
		b.add(storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY,
			&storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_ReferentialIntegrity{ReferentialIntegrity: &storepb.ReferentialIntegrityConfig{
				ChildColumn:   child,
				ParentTable:   parentTable,
				ParentColumn:  parentColumn,
				AllowNull:     childInfo.Nullable,
				FailThreshold: proto.Float64(0),
			}}},
			child,
			recommendConfidenceSchema,
			recommendRationale(storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY, b.language, recommendFacts{
				column: child, parentTable: parentTable, parentColumn: parentColumn,
			}))
	}
}

// recommendParentInContainer reports whether a foreign key's referenced schema names
// the target's own container. The registry records the referenced schema per engine;
// an empty value means the same container. A cross-schema parent cannot be expressed
// by the rule, so the candidate is dropped rather than saved as an always-error rule.
func recommendParentInContainer(target ResolvedTarget, referencedSchema string) bool {
	referencedSchema = strings.TrimSpace(referencedSchema)
	if isMySQLWireEngine(target.Engine) {
		// The MySQL family has no schema segment; the database is the container. A
		// referenced schema, when recorded, names that database.
		return referencedSchema == "" || strings.EqualFold(referencedSchema, target.Database)
	}
	return referencedSchema == "" || strings.EqualFold(referencedSchema, target.Schema)
}

// recommendTableRows reads the table's row count from any column profile of the
// scope. Every column profile of one profile run carries the same total.
func recommendTableRows(columns []recommendColumn) (int64, bool) {
	for _, column := range columns {
		if column.profile == nil {
			continue
		}
		if total := column.profile.GetTotalRows(); total > 0 {
			return total, true
		}
	}
	return 0, false
}

// recommendRows returns a profile's row count, or zero when there is no profile.
func recommendRows(profile *storepb.ColumnProfile) int64 {
	if profile == nil {
		return 0
	}
	return profile.GetTotalRows()
}

// recommendTableLabel names the relation in a table-level check title.
func recommendTableLabel(target ResolvedTarget) string {
	if table := strings.TrimSpace(target.Table); table != "" {
		return table
	}
	return "table"
}

// freshnessCandidate is the temporal column a freshness check is proposed on,
// with the observation that justifies its bound.
type freshnessCandidate struct {
	column   string
	observed time.Time
	lag      time.Duration
}

// recommendFreshnessCandidate picks at most one temporal column to monitor for
// freshness: a column labeled as a timestamp or date wins, then a conventionally
// named one, then the column with the newest observed value. The chosen column's
// observed newest value must be recent: an old newest value means the relation is an
// archive, and a freshness check would either fail forever or assert nothing.
func recommendFreshnessCandidate(columns []recommendColumn, now time.Time) *freshnessCandidate {
	var best *freshnessCandidate
	bestScore := 0
	for _, column := range columns {
		if column.info.Kind != ColumnKindTemporal || column.profile == nil {
			continue
		}
		observed, ok := recommendParseTime(column.profile.GetMaxValue())
		if !ok {
			continue
		}
		lag := now.Sub(observed)
		if lag < 0 {
			lag = 0
		}
		if lag > recommendFreshnessMaxLag {
			continue
		}
		score := recommendFreshnessScore(column)
		if best != nil && (score < bestScore || (score == bestScore && !observed.After(best.observed))) {
			continue
		}
		best = &freshnessCandidate{column: column.info.Name, observed: observed, lag: lag}
		bestScore = score
	}
	return best
}

// recommendFreshnessScore ranks the reasons to read a column as the relation's event
// time: a semantic label is stronger evidence than a name, and any temporal column is
// better than none.
func recommendFreshnessScore(column recommendColumn) int {
	score := 1
	if recommendTimestampName(column.info.Name) {
		score = 2
	}
	if column.label == storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_TIMESTAMP ||
		column.label == storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_DATE {
		score = 3
	}
	return score
}

// recommendTimestampName reports whether a column name conventionally names an event
// or record time. It is a heuristic used only to rank candidates, never to assert
// that a column is a timestamp.
func recommendTimestampName(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	switch lower {
	case "ts", "time", "date", "datetime", "timestamp":
		return true
	default:
		return strings.HasSuffix(lower, "_at") || strings.HasSuffix(lower, "_time") ||
			strings.HasSuffix(lower, "_date") || strings.HasSuffix(lower, "_ts") ||
			strings.HasPrefix(lower, "created") || strings.HasPrefix(lower, "updated") ||
			strings.HasPrefix(lower, "insert") || strings.HasPrefix(lower, "load")
	}
}

// recommendFreshnessBound rounds a proposed lag up to a readable unit, so the form
// shows "24 hours" rather than "23h 47m".
func recommendFreshnessBound(want time.Duration) time.Duration {
	units := []time.Duration{
		time.Hour, 2 * time.Hour, 6 * time.Hour, 12 * time.Hour, 24 * time.Hour,
		48 * time.Hour, 72 * time.Hour, 7 * 24 * time.Hour, 14 * 24 * time.Hour, 30 * 24 * time.Hour,
	}
	for _, unit := range units {
		if want <= unit {
			return unit
		}
	}
	return recommendFreshnessMaxLag
}

// recommendParseTime reads one profile-rendered timestamp. The profiler renders a
// database time value as RFC 3339; a driver that returned text leaves the engine's
// own spelling, so the common forms are accepted too.
func recommendParseTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	layouts := []string{
		time.RFC3339Nano, time.RFC3339,
		"2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05",
		"2006-01-02T15:04:05.999999999", "2006-01-02",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

// recommendParseNumber reads one profile-rendered number.
func recommendParseNumber(value string) (float64, bool) {
	number, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || math.IsInf(number, 0) || math.IsNaN(number) {
		return 0, false
	}
	return number, true
}

// recommendFormatNumber renders a bound without an exponent, so the form shows the
// value the reviewer expects.
func recommendFormatNumber(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

// recommendFacts carries the observations one rationale may cite.
type recommendFacts struct {
	column          string
	rows            int64
	distinct        int64
	nullPct         float64
	threshold       float64
	minRows         int64
	minValue        string
	maxValue        string
	maxLength       int64
	thresholdLength int64
	newest          string
	lag             time.Duration
	bound           time.Duration
	parentTable     string
	parentColumn    string
	label           storepb.QualitySemanticType
}

// recommendTitle names one draft the way the label drafts name theirs: the column
// (or the table, for a table-level check) followed by the check type.
func recommendTitle(columnLabel string, ruleType storepb.QualityRuleType, language string) string {
	return fmt.Sprintf("%s: %s", columnLabel, recommendRuleTypeName(ruleType, language))
}

// recommendRuleTypeName is the user-facing name of a check type in the draft's
// language. It mirrors the frontend's rule-type vocabulary; the frontend remains the
// single source for the form, this only names what the pass proposes.
func recommendRuleTypeName(ruleType storepb.QualityRuleType, language string) string {
	names := recommendRuleTypeNamesEN
	if language == "zh-CN" {
		names = recommendRuleTypeNamesZH
	}
	if name, ok := names[ruleType]; ok {
		return name
	}
	return "check"
}

var recommendRuleTypeNamesEN = map[storepb.QualityRuleType]string{
	storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT:             "row count",
	storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT:          "null rate",
	storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE:                "uniqueness",
	storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION:    "unique combination",
	storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES:       "accepted values",
	storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE:                 "range",
	storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX:                 "regex",
	storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH:         "string length",
	storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY: "referential integrity",
	storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS:             "freshness",
}

var recommendRuleTypeNamesZH = map[storepb.QualityRuleType]string{
	storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT:             "行数",
	storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT:          "空值率",
	storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE:                "唯一性",
	storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION:    "组合唯一性",
	storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES:       "允许值",
	storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE:                 "范围",
	storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX:                 "正则表达式",
	storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH:         "字符串长度",
	storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY: "引用完整性",
	storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS:             "时效性",
}

// recommendRationale explains a candidate in the draft's language: the observation it
// came from and the arithmetic that produced the bound. The reviewer needs both to
// decide whether the proposal is right.
func recommendRationale(ruleType storepb.QualityRuleType, language string, facts recommendFacts) string {
	if language == "zh-CN" {
		if build, ok := recommendRationaleZH[ruleType]; ok {
			return build(facts)
		}
		return ""
	}
	if build, ok := recommendRationaleEN[ruleType]; ok {
		return build(facts)
	}
	return ""
}

var recommendRationaleEN = map[storepb.QualityRuleType]func(recommendFacts) string{
	storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT: func(facts recommendFacts) string {
		return fmt.Sprintf("Observed %d rows; the proposed floor is half of that (%d).", facts.rows, facts.minRows)
	},
	storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT: func(facts recommendFacts) string {
		return fmt.Sprintf("Observed %.2f%% NULL across %d rows; the proposed bound is %.2f%%, 1.5x the observation.", facts.nullPct, facts.rows, facts.threshold)
	},
	storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE: func(facts recommendFacts) string {
		if facts.label == storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_ID ||
			facts.label == storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_BUSINESS_KEY {
			return fmt.Sprintf("The column is labeled %s; the check reports duplicate identifiers.", labelSemanticTypeShortName(facts.label))
		}
		return fmt.Sprintf("Every one of the %d observed rows has a distinct value; the check reports the first duplicate.", facts.rows)
	},
	storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES: func(facts recommendFacts) string {
		return fmt.Sprintf("Observed exactly %d distinct values across %d rows; the proposed set lists all of them.", facts.distinct, facts.rows)
	},
	storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE: func(facts recommendFacts) string {
		return fmt.Sprintf("Observed range %s to %s across %d rows; the proposed bounds add 10%% of the span on each side.", facts.minValue, facts.maxValue, facts.rows)
	},
	storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX: func(facts recommendFacts) string {
		return fmt.Sprintf("The column is labeled %s; the proposed pattern is the conservative default for that format.", labelSemanticTypeShortName(facts.label))
	},
	storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH: func(facts recommendFacts) string {
		return fmt.Sprintf("The longest observed value is %d characters; the proposed bound is %d, a fifth longer.", facts.maxLength, facts.thresholdLength)
	},
	storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS: func(facts recommendFacts) string {
		return fmt.Sprintf("The newest value is %s (about %s ago); the proposed bound is %s, twice the observed lag.", facts.newest, recommendDurationTextEN(facts.lag), recommendDurationTextEN(facts.bound))
	},
	storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY: func(facts recommendFacts) string {
		return fmt.Sprintf("The foreign key on %s references %s(%s); the check reports child rows with no matching parent.", facts.column, facts.parentTable, facts.parentColumn)
	},
}

var recommendRationaleZH = map[storepb.QualityRuleType]func(recommendFacts) string{
	storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT: func(facts recommendFacts) string {
		return fmt.Sprintf("观测到 %d 行；建议下限取其一半（%d 行）。", facts.rows, facts.minRows)
	},
	storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT: func(facts recommendFacts) string {
		return fmt.Sprintf("观测到 %d 行中空值占 %.2f%%；建议阈值 %.2f%%，为观测值的 1.5 倍。", facts.rows, facts.nullPct, facts.threshold)
	},
	storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE: func(facts recommendFacts) string {
		if facts.label == storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_ID ||
			facts.label == storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_BUSINESS_KEY {
			return fmt.Sprintf("该字段标注为 %s；检查会报告重复的标识值。", labelSemanticTypeShortName(facts.label))
		}
		return fmt.Sprintf("观测到的 %d 行取值全部互不相同；检查会报告第一条重复值。", facts.rows)
	},
	storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES: func(facts recommendFacts) string {
		return fmt.Sprintf("观测到 %d 个不同取值（共 %d 行）；建议集合已完整列出。", facts.distinct, facts.rows)
	},
	storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE: func(facts recommendFacts) string {
		return fmt.Sprintf("观测到取值范围 %s 至 %s（共 %d 行）；建议上下界各放宽区间的 10%%。", facts.minValue, facts.maxValue, facts.rows)
	},
	storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX: func(facts recommendFacts) string {
		return fmt.Sprintf("该字段标注为 %s；建议使用该格式的保守默认正则。", labelSemanticTypeShortName(facts.label))
	},
	storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH: func(facts recommendFacts) string {
		return fmt.Sprintf("观测到最长取值 %d 个字符；建议上限 %d，放宽两成。", facts.maxLength, facts.thresholdLength)
	},
	storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS: func(facts recommendFacts) string {
		return fmt.Sprintf("最新取值为 %s（约 %s 前）；建议上限 %s，为观测延迟的两倍。", facts.newest, recommendDurationTextZH(facts.lag), recommendDurationTextZH(facts.bound))
	},
	storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY: func(facts recommendFacts) string {
		return fmt.Sprintf("字段 %s 的外键引用 %s(%s)；检查会报告找不到父行的子行。", facts.column, facts.parentTable, facts.parentColumn)
	},
}

// recommendDurationTextEN renders a duration the way a reviewer reads it: whole
// hours and days keep one decimal only when it carries information.
func recommendDurationTextEN(value time.Duration) string {
	hours := value.Hours()
	switch {
	case hours < 1:
		return fmt.Sprintf("%d minutes", int(value.Minutes()))
	case hours < 48:
		if math.Mod(hours, 1) == 0 {
			return fmt.Sprintf("%d hours", int(hours))
		}
		return fmt.Sprintf("%.1f hours", hours)
	default:
		days := hours / 24
		if math.Mod(days, 1) == 0 {
			return fmt.Sprintf("%d days", int(days))
		}
		return fmt.Sprintf("%.1f days", days)
	}
}

// recommendDurationTextZH renders a duration for the Chinese rationale.
func recommendDurationTextZH(value time.Duration) string {
	hours := value.Hours()
	switch {
	case hours < 1:
		return fmt.Sprintf("%d 分钟", int(value.Minutes()))
	case hours < 48:
		if math.Mod(hours, 1) == 0 {
			return fmt.Sprintf("%d 小时", int(hours))
		}
		return fmt.Sprintf("%.1f 小时", hours)
	default:
		days := hours / 24
		if math.Mod(days, 1) == 0 {
			return fmt.Sprintf("%d 天", int(days))
		}
		return fmt.Sprintf("%.1f 天", days)
	}
}
