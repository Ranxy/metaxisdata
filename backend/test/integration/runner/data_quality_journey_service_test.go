//go:build integration

package runner

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// This file is the data-quality module's end-to-end journey. The feature-sliced
// scenarios in data_quality_service_test.go each pin one surface against a
// one-row fixture; this test instead drives the whole module the way the product
// is used: a real MySQL source with known defects, a rule suite across every rule
// type, one batch execution, the issue ledger and inbox, the scheduled runner,
// the overview, profiling and labels, a data repair that recovers the problems,
// and the lifecycle edges (missing read-only source, schema change, orphaned
// target, archival).
//
// Everything is asserted through the public ConnectRPC API except two deliberate
// seams: the observer store makes one rule due instead of waiting for its hourly
// slot, and the source database is edited directly to produce and then repair
// data defects.

// journeySeedDDL creates the source tables the journey owns. It runs against the
// per-test MySQL database after the shared fixture reset, so it cannot collide
// with another scenario. The defect counts are exact: two NULL emails, one
// duplicated external_ref, one negative amount, one unaccepted status, one short
// product_code, one orphan customer_id, one shipped_quantity above quantity, and
// one heartbeat table whose rows are all stale.
const journeySeedDDL = `
CREATE TABLE dq_reference_customers (
  id INT PRIMARY KEY,
  name VARCHAR(64) NOT NULL
);
INSERT INTO dq_reference_customers (id, name) VALUES (1, 'a'), (2, 'b'), (3, 'c');

CREATE TABLE dq_orders (
  id INT PRIMARY KEY,
  customer_email VARCHAR(128) NULL,
  external_ref VARCHAR(32) NOT NULL,
  amount DECIMAL(10,2) NOT NULL,
  status VARCHAR(16) NOT NULL,
  product_code VARCHAR(16) NOT NULL,
  customer_id INT NOT NULL,
  quantity INT NOT NULL,
  shipped_quantity INT NOT NULL,
  updated_at DATETIME NOT NULL
);
INSERT INTO dq_orders
  (id, customer_email, external_ref, amount, status, product_code, customer_id, quantity, shipped_quantity, updated_at)
VALUES
  (1,  'alice@example.com', 'R-1',  10.00, 'paid',      'ABC',      1,   1,  1, NOW()),
  (2,  NULL,                'R-2',  20.00, 'pending',   'ABCD',     2,   2,  2, NOW()),
  (3,  NULL,                'R-3',  30.00, 'shipped',   'ABCDE',    1,   3,  3, NOW()),
  (4,  'bob@example.com',   'R-4',  -5.00, 'paid',      'AB',       2,   4,  4, NOW()),
  (5,  'carol@example.com', 'R-5',  50.00, 'cancelled', 'ABCDEF',   3,   5,  5, NOW()),
  (6,  'dave@example.com',  'R-4',  60.00, 'paid',      'ABCDEFG',  1,   6,  7, NOW()),
  (7,  'erin@example.com',  'R-7',  70.00, 'paid',      'ABCDEFGH', 999, 7,  7, NOW()),
  (8,  'frank@example.com', 'R-8',  80.00, 'pending',   'ABC',      2,   8,  8, NOW()),
  (9,  'grace@example.com', 'R-9',  90.00, 'shipped',   'ABC',      3,   9,  9, NOW()),
  (10, 'heidi@example.com', 'R-10', 100.00,'paid',      'ABC',      1,   10, 10, NOW());

CREATE TABLE dq_heartbeat (
  id INT PRIMARY KEY,
  updated_at DATETIME NOT NULL
);
INSERT INTO dq_heartbeat (id, updated_at) VALUES (1, '2020-01-01 00:00:00'), (2, '2020-01-02 00:00:00');

CREATE TABLE dq_empty (
  id INT PRIMARY KEY,
  email VARCHAR(128) NULL
);

CREATE TABLE dq_orphan_target (
  id INT PRIMARY KEY
);
INSERT INTO dq_orphan_target (id) VALUES (1);
`

// journeyRepairSQL repairs every seeded defect in dq_orders and refreshes the
// heartbeat; only dq_empty stays empty. The recovery phase must change the data,
// not the rule definitions.
const journeyRepairSQL = `
UPDATE dq_orders SET customer_email = 'repaired@example.com' WHERE id IN (2, 3);
UPDATE dq_orders SET external_ref = 'R-6' WHERE id = 6;
UPDATE dq_orders SET amount = 55.00 WHERE id = 4;
UPDATE dq_orders SET status = 'paid' WHERE id = 5;
UPDATE dq_orders SET product_code = 'ABC' WHERE id = 4;
UPDATE dq_orders SET customer_id = 1 WHERE id = 7;
UPDATE dq_orders SET shipped_quantity = 6 WHERE id = 6;
INSERT INTO dq_heartbeat (id, updated_at) VALUES (3, NOW());
`

// journeySource holds the registry GUIDs of the journey's tables.
type journeySource struct {
	orders    string
	heartbeat string
	empty     string
	orphan    string
}

// journeyRuleSpec is one rule in the suite. label keys the rule in the test's
// maps; firstRun and afterRepair are the exact verdicts the seeded data must
// produce.
type journeyRuleSpec struct {
	label       string
	ruleType    v1pb.QualityRuleType
	dimension   v1pb.QualityDimension
	severity    v1pb.QualitySeverity
	targetGUID  string
	config      *v1pb.QualityRuleConfig
	firstRun    v1pb.QualityStatus
	afterRepair v1pb.QualityStatus
}

// TestDataQualityJourneyMySQLRealServerIntegration is the module's end-to-end
// journey: it seeds a known-bad MySQL source, runs every rule type as one batch
// through the real runner, and follows the resulting problems through the issue
// ledger, the inbox, the scheduled runner, the overview, profiling and labels
// until a data repair recovers them. It then pins the lifecycle edges and
// archival behavior.
func TestDataQualityJourneyMySQLRealServerIntegration(t *testing.T) {
	t.Parallel()

	fixture := setupQualityMySQLFixture(t, true)
	source := seedJourneySource(t, fixture)
	rules := createJourneyRules(t, source)
	ruleNames := make(map[string]string, len(rules))
	for _, rule := range rules {
		ruleNames[rule.label] = fixture.createJourneyRule(t, rule).GetName()
	}
	require.Len(t, ruleNames, 19, "the suite covers every rule type plus the empty-data path")

	// P2: one manual batch, one execution per rule, every verdict exactly as the
	// seeded data dictates.
	firstRun := runJourneyRules(t, fixture, rules, ruleNames)
	assertJourneyOutcomes(t, rules, ruleNames, firstRun, func(spec journeyRuleSpec) v1pb.QualityStatus { return spec.firstRun })
	assertJourneyEvidence(t, firstRun, ruleNames)
	assertJourneySamples(t, firstRun, ruleNames)
	// The directory row and the target workspace expose the same score and recent
	// pass rate: dq_orders carries 17 enabled rules, all judged once, of which 10
	// pass, 1 warns and 6 fail.
	assertJourneyTargetHealth(t, fixture, source.orders, 17, 10.0/17.0)

	// P3: each FAIL opens exactly one issue; the passing, warning and unevaluated
	// verdicts open none; each opened issue produces one inbox message.
	issues := waitForJourneyIssues(t, fixture, rules, ruleNames)
	for _, rule := range rules {
		ruleName := ruleNames[rule.label]
		if rule.firstRun != v1pb.QualityStatus_QUALITY_STATUS_FAIL {
			require.Equal(t, 0, journeyNotificationCount(t, fixture,
				v1pb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_OPENED, ruleName),
				"%s must not send an issue-opened message", rule.label)
			continue
		}
		fixture.waitForNotification(t, v1pb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_OPENED, ruleName)
		require.Equal(t, 1, journeyNotificationCount(t, fixture,
			v1pb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_OPENED, ruleName),
			"%s should send exactly one issue-opened message", rule.label)
	}

	// P4: the scheduler plans and executes a due rule without an API call. A
	// second FAIL only touches the issue: occurrence_count moves and no duplicate
	// message is written.
	scheduled := runJourneyScheduledExecution(t, fixture, ruleNames["unique"])
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_FAIL, scheduled.GetOutcome())
	require.Equal(t, v1pb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_SCHEDULED, scheduled.GetTrigger())
	require.Empty(t, scheduled.GetInitiator(), "a scheduled execution has no API initiator")
	waitForJourneyIssueOccurrence(t, fixture, issues[ruleNames["unique"]], 2)
	require.Equal(t, 1, journeyNotificationCount(t, fixture,
		v1pb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_OPENED, ruleNames["unique"]),
		"a repeated FAIL touches the issue instead of sending another message")
	assertJourneyRuleScheduledAhead(t, fixture, ruleNames["unique"])

	// P5: the overview aggregates the same workspace state. Parallel scenarios
	// share this workspace, so only lower bounds and the response's own structural
	// invariants are exact.
	assertJourneyOverview(t, fixture, len(rules), len(rules), 7)

	// P6: profiling and a manual label over the same journey table.
	assertJourneyProfileAndLabel(t, fixture, source.orders)

	// P7: repair the data (not the rules) and rerun the suite. Every rule that
	// could not pass now passes, the empty target still has no verdict, and all
	// seven issues recover automatically.
	repairJourneyData(t, fixture)
	secondRun := runJourneyRules(t, fixture, rules, ruleNames)
	assertJourneyOutcomes(t, rules, ruleNames, secondRun, func(spec journeyRuleSpec) v1pb.QualityStatus { return spec.afterRepair })
	for _, rule := range rules {
		if rule.firstRun != v1pb.QualityStatus_QUALITY_STATUS_FAIL {
			continue
		}
		recovered := waitForJourneyIssueStatus(t, fixture, ruleNames[rule.label], v1pb.QualityIssueStatus_QUALITY_ISSUE_STATUS_RESOLVED)
		require.Equal(t, v1pb.QualityResolutionReason_QUALITY_RESOLUTION_REASON_AUTO_RECOVERED, recovered.GetResolutionReason())
		fixture.waitForNotification(t, v1pb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_RECOVERED, ruleNames[rule.label])
		require.Equal(t, 1, journeyNotificationCount(t, fixture,
			v1pb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_RECOVERED, ruleNames[rule.label]),
			"%s should send exactly one recovery message", rule.label)
	}
	assertJourneyOverview(t, fixture, len(rules), 2*len(rules), 0)

	// P8a: a runtime-unusable read-only source is an ERROR, not a FAIL, and the
	// operational failure is announced to the administrators.
	assertJourneyMissingReadOnlySource(t, fixture, ruleNames["row_count"])

	// P8b: a DDL change underneath a saved rule is caught by the metadata rule.
	assertJourneySchemaDriftAfterDDLChange(t, fixture, ruleNames["schema_drift"])

	// P8c: a dropped target is SKIPPED(ORPHANED) at runtime and explained on the
	// rule read path.
	orphanRuleName := assertJourneyOrphanedTarget(t, fixture, source.orphan)

	// P8d: a soft-deleted instance keeps its registry rows, but its objects are no
	// longer coverage, so its rules' targets must leave the numerator as well.
	assertJourneyDeletedInstanceCoverage(t, fixture, source.orders)

	// P9: archiving keeps rule history readable and removes the rules from the
	// default list.
	assertJourneyArchive(t, fixture, append(journeyRuleNameList(ruleNames), orphanRuleName))
}

// journeyRuleNameList returns the rule names in suite creation order.
func journeyRuleNameList(names map[string]string) []string {
	labels := []string{
		"row_count", "row_count_change", "null_percent", "unique", "unique_combination",
		"accepted_values", "range", "regex", "string_length", "referential_integrity",
		"cross_column", "freshness", "custom_sql", "cel_expression", "schema_drift",
		"group_check", "aggregate", "cross_table", "empty_null_percent",
	}
	ruleNames := make([]string, 0, len(labels))
	for _, label := range labels {
		ruleNames = append(ruleNames, names[label])
	}
	return ruleNames
}

// seedJourneySource seeds the journey tables in the fixture's own database and
// syncs the database so the registry knows them.
func seedJourneySource(t *testing.T, fixture *qualityFixture) journeySource {
	t.Helper()

	require.NoError(t, fixture.env.ExecMySQL(fixture.ctx,
		"USE "+quoteMySQLIdentifier(fixture.sourceDatabase)+";"+journeySeedDDL), "seed the journey source")
	fixture.env.SyncDatabase(fixture.ctx, t, fixture.databaseName)

	prefix := fmt.Sprintf("%s;%s", fixture.instanceID, fixture.sourceDatabase)
	return journeySource{
		orders:    waitForMetaGUIDByName(fixture.ctx, t, fixture.env, prefix, storepb.MetaType_TABLE, "dq_orders"),
		heartbeat: waitForMetaGUIDByName(fixture.ctx, t, fixture.env, prefix, storepb.MetaType_TABLE, "dq_heartbeat"),
		empty:     waitForMetaGUIDByName(fixture.ctx, t, fixture.env, prefix, storepb.MetaType_TABLE, "dq_empty"),
		orphan:    waitForMetaGUIDByName(fixture.ctx, t, fixture.env, prefix, storepb.MetaType_TABLE, "dq_orphan_target"),
	}
}

// createJourneyRules builds the rule suite. Ten rules pass, one warns, seven fail
// (kept below the ten-failure storm threshold so per-rule notifications are
// deterministic), and one has no verdict; the empty target is that unevaluated
// rule, while every evaluated rule recovers after the repair.
func createJourneyRules(t *testing.T, source journeySource) []journeyRuleSpec {
	t.Helper()

	rowCountMin, rowCountMax := int64(5), int64(100)
	rowCountChangeIncrease, rowCountChangeDecrease := 100.0, 100.0
	nullEmailMax, nullEmptyMax := int64(1), int64(0)
	acceptedFail, acceptedWarn := 2.0, 0.0
	rangeMin, rangeMax := "0", "1000"
	lengthMin, lengthMax := int64(3), int64(8)
	groupMax := int64(7)
	aggregateMin, aggregateMax := 0.0, 10000.0
	crossTableTolerance := 10.0

	return []journeyRuleSpec{
		{
			label: "row_count", ruleType: v1pb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
			dimension:  v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS,
			severity:   v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
			targetGUID: source.orders,
			config: &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_RowCount{RowCount: &v1pb.RowCountConfig{
				MinRows: &rowCountMin, MaxRows: &rowCountMax,
			}}},
			firstRun:    v1pb.QualityStatus_QUALITY_STATUS_PASS,
			afterRepair: v1pb.QualityStatus_QUALITY_STATUS_PASS,
		},
		{
			label: "row_count_change", ruleType: v1pb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE,
			dimension:  v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS,
			severity:   v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
			targetGUID: source.orders,
			config: &v1pb.QualityRuleConfig{
				Config: &v1pb.QualityRuleConfig_RowCountChange{RowCountChange: &v1pb.RowCountChangeConfig{
					MaxIncreasePct: &rowCountChangeIncrease, MaxDecreasePct: &rowCountChangeDecrease,
				}},
				// Without a baseline the verdict follows on_empty; PASS lets the
				// first observation become the baseline the next run compares to.
				OnEmpty: v1pb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_PASS,
			},
			firstRun:    v1pb.QualityStatus_QUALITY_STATUS_PASS,
			afterRepair: v1pb.QualityStatus_QUALITY_STATUS_PASS,
		},
		{
			label: "null_percent", ruleType: v1pb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
			dimension:  v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS,
			severity:   v1pb.QualitySeverity_QUALITY_SEVERITY_ERROR,
			targetGUID: source.orders,
			config: &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_NullPercent{NullPercent: &v1pb.NullPercentConfig{
				Column: "customer_email", MaxNullCount: &nullEmailMax,
			}}},
			firstRun:    v1pb.QualityStatus_QUALITY_STATUS_FAIL,
			afterRepair: v1pb.QualityStatus_QUALITY_STATUS_PASS,
		},
		{
			label: "unique", ruleType: v1pb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
			dimension:  v1pb.QualityDimension_QUALITY_DIMENSION_UNIQUENESS,
			severity:   v1pb.QualitySeverity_QUALITY_SEVERITY_ERROR,
			targetGUID: source.orders,
			config: &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_Unique{Unique: &v1pb.UniqueConfig{
				Column: "external_ref",
			}}},
			firstRun:    v1pb.QualityStatus_QUALITY_STATUS_FAIL,
			afterRepair: v1pb.QualityStatus_QUALITY_STATUS_PASS,
		},
		{
			label: "unique_combination", ruleType: v1pb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION,
			dimension:  v1pb.QualityDimension_QUALITY_DIMENSION_UNIQUENESS,
			severity:   v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
			targetGUID: source.orders,
			config: &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_UniqueCombination{UniqueCombination: &v1pb.UniqueCombinationConfig{
				Columns: []string{"id", "external_ref"},
			}}},
			firstRun:    v1pb.QualityStatus_QUALITY_STATUS_PASS,
			afterRepair: v1pb.QualityStatus_QUALITY_STATUS_PASS,
		},
		{
			label: "accepted_values", ruleType: v1pb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES,
			dimension:  v1pb.QualityDimension_QUALITY_DIMENSION_VALIDITY,
			severity:   v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
			targetGUID: source.orders,
			config: &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_AcceptedValues{AcceptedValues: &v1pb.AcceptedValuesConfig{
				Column: "status", Values: []string{"paid", "pending", "shipped"},
				FailThreshold: &acceptedFail, WarnThreshold: &acceptedWarn,
			}}},
			firstRun:    v1pb.QualityStatus_QUALITY_STATUS_WARN,
			afterRepair: v1pb.QualityStatus_QUALITY_STATUS_PASS,
		},
		{
			label: "range", ruleType: v1pb.QualityRuleType_QUALITY_RULE_TYPE_RANGE,
			dimension:  v1pb.QualityDimension_QUALITY_DIMENSION_VALIDITY,
			severity:   v1pb.QualitySeverity_QUALITY_SEVERITY_ERROR,
			targetGUID: source.orders,
			config: &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_Range{Range: &v1pb.RangeConfig{
				Column: "amount", Min: &rangeMin, Max: &rangeMax,
			}}},
			firstRun:    v1pb.QualityStatus_QUALITY_STATUS_FAIL,
			afterRepair: v1pb.QualityStatus_QUALITY_STATUS_PASS,
		},
		{
			label: "regex", ruleType: v1pb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
			dimension:  v1pb.QualityDimension_QUALITY_DIMENSION_VALIDITY,
			severity:   v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
			targetGUID: source.orders,
			config: &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_Regex{Regex: &v1pb.RegexConfig{
				Column: "external_ref", Pattern: `^R-[0-9]+$`,
			}}},
			firstRun:    v1pb.QualityStatus_QUALITY_STATUS_PASS,
			afterRepair: v1pb.QualityStatus_QUALITY_STATUS_PASS,
		},
		{
			label: "string_length", ruleType: v1pb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH,
			dimension:  v1pb.QualityDimension_QUALITY_DIMENSION_VALIDITY,
			severity:   v1pb.QualitySeverity_QUALITY_SEVERITY_ERROR,
			targetGUID: source.orders,
			config: &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_StringLength{StringLength: &v1pb.StringLengthConfig{
				Column: "product_code", MinLen: &lengthMin, MaxLen: &lengthMax,
			}}},
			firstRun:    v1pb.QualityStatus_QUALITY_STATUS_FAIL,
			afterRepair: v1pb.QualityStatus_QUALITY_STATUS_PASS,
		},
		{
			label: "referential_integrity", ruleType: v1pb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY,
			dimension:  v1pb.QualityDimension_QUALITY_DIMENSION_CONSISTENCY,
			severity:   v1pb.QualitySeverity_QUALITY_SEVERITY_ERROR,
			targetGUID: source.orders,
			config: &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_ReferentialIntegrity{ReferentialIntegrity: &v1pb.ReferentialIntegrityConfig{
				ChildColumn: "customer_id", ParentTable: "dq_reference_customers", ParentColumn: "id",
			}}},
			firstRun:    v1pb.QualityStatus_QUALITY_STATUS_FAIL,
			afterRepair: v1pb.QualityStatus_QUALITY_STATUS_PASS,
		},
		{
			label: "cross_column", ruleType: v1pb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_COLUMN,
			dimension:  v1pb.QualityDimension_QUALITY_DIMENSION_CONSISTENCY,
			severity:   v1pb.QualitySeverity_QUALITY_SEVERITY_ERROR,
			targetGUID: source.orders,
			config: &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_CrossColumn{CrossColumn: &v1pb.CrossColumnConfig{
				LeftColumn: "shipped_quantity", RightColumn: "quantity",
				Op: v1pb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_LTE,
			}}},
			firstRun:    v1pb.QualityStatus_QUALITY_STATUS_FAIL,
			afterRepair: v1pb.QualityStatus_QUALITY_STATUS_PASS,
		},
		{
			label: "freshness", ruleType: v1pb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS,
			dimension:  v1pb.QualityDimension_QUALITY_DIMENSION_TIMELINESS,
			severity:   v1pb.QualitySeverity_QUALITY_SEVERITY_ERROR,
			targetGUID: source.heartbeat,
			config: &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_Freshness{Freshness: &v1pb.FreshnessConfig{
				TimestampColumn: "updated_at", MaxLagSeconds: 3600,
			}}},
			firstRun:    v1pb.QualityStatus_QUALITY_STATUS_FAIL,
			afterRepair: v1pb.QualityStatus_QUALITY_STATUS_PASS,
		},
		{
			label: "custom_sql", ruleType: v1pb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL,
			dimension:  v1pb.QualityDimension_QUALITY_DIMENSION_ACCURACY,
			severity:   v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
			targetGUID: source.orders,
			config: &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_CustomSql{CustomSql: &v1pb.CustomSQLConfig{
				Sql: "SELECT id FROM dq_orders WHERE amount > 10000",
			}}},
			firstRun:    v1pb.QualityStatus_QUALITY_STATUS_PASS,
			afterRepair: v1pb.QualityStatus_QUALITY_STATUS_PASS,
		},
		{
			label: "cel_expression", ruleType: v1pb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION,
			dimension:  v1pb.QualityDimension_QUALITY_DIMENSION_ACCURACY,
			severity:   v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
			targetGUID: source.orders,
			config: &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_CelExpression{CelExpression: &v1pb.CelExpressionConfig{
				Expression: "id > 0", Columns: []string{"id"},
			}}},
			firstRun:    v1pb.QualityStatus_QUALITY_STATUS_PASS,
			afterRepair: v1pb.QualityStatus_QUALITY_STATUS_PASS,
		},
		{
			label: "schema_drift", ruleType: v1pb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT,
			dimension:   v1pb.QualityDimension_QUALITY_DIMENSION_CONSISTENCY,
			severity:    v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
			targetGUID:  source.orders,
			config:      &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_SchemaDrift{SchemaDrift: &v1pb.SchemaDriftConfig{}}},
			firstRun:    v1pb.QualityStatus_QUALITY_STATUS_PASS,
			afterRepair: v1pb.QualityStatus_QUALITY_STATUS_PASS,
		},
		{
			label: "group_check", ruleType: v1pb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK,
			dimension:  v1pb.QualityDimension_QUALITY_DIMENSION_CONSISTENCY,
			severity:   v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
			targetGUID: source.orders,
			config: &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_GroupCheck{GroupCheck: &v1pb.GroupCheckConfig{
				Columns: []string{"status"}, MaxRowsPerGroup: &groupMax,
			}}},
			firstRun:    v1pb.QualityStatus_QUALITY_STATUS_PASS,
			afterRepair: v1pb.QualityStatus_QUALITY_STATUS_PASS,
		},
		{
			label: "aggregate", ruleType: v1pb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE,
			dimension:  v1pb.QualityDimension_QUALITY_DIMENSION_ACCURACY,
			severity:   v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
			targetGUID: source.orders,
			config: &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_Aggregate{Aggregate: &v1pb.AggregateConfig{
				Column: "amount", Function: v1pb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM,
				Min: &aggregateMin, Max: &aggregateMax,
			}}},
			firstRun:    v1pb.QualityStatus_QUALITY_STATUS_PASS,
			afterRepair: v1pb.QualityStatus_QUALITY_STATUS_PASS,
		},
		{
			label: "cross_table", ruleType: v1pb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE,
			dimension:  v1pb.QualityDimension_QUALITY_DIMENSION_CONSISTENCY,
			severity:   v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
			targetGUID: source.orders,
			config: &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_CrossTable{CrossTable: &v1pb.CrossTableConfig{
				RightTable:    "dq_reference_customers",
				LeftFunction:  v1pb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT,
				RightFunction: v1pb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT,
				Tolerance:     &crossTableTolerance,
			}}},
			firstRun:    v1pb.QualityStatus_QUALITY_STATUS_PASS,
			afterRepair: v1pb.QualityStatus_QUALITY_STATUS_PASS,
		},
		{
			label: "empty_null_percent", ruleType: v1pb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
			dimension:  v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS,
			severity:   v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
			targetGUID: source.empty,
			config: &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_NullPercent{NullPercent: &v1pb.NullPercentConfig{
				Column: "email", MaxNullCount: &nullEmptyMax,
			}}},
			firstRun:    v1pb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED,
			afterRepair: v1pb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED,
		},
	}
}

// createJourneyRule creates one rule through the public API.
func (f *qualityFixture) createJourneyRule(t *testing.T, rule journeyRuleSpec) *v1pb.QualityRule {
	t.Helper()

	created, err := f.dataQuality.CreateQualityRule(f.ctx, withToken(f.env.AdminToken(), &v1pb.CreateQualityRuleRequest{
		Parent: "workspaces/-",
		Rule: &v1pb.QualityRule{
			Title:     "journey " + rule.label,
			Dimension: rule.dimension,
			RuleType:  rule.ruleType,
			Target:    &v1pb.QualityTarget{MetaGuid: rule.targetGUID, MetaType: v1pb.MetaType_TABLE},
			Config:    rule.config,
			Severity:  rule.severity,
		},
	}))
	require.NoErrorf(t, err, "create the %s rule", rule.label)
	return created.Msg
}

// runJourneyRules runs every rule as one manual batch and waits for each planned
// execution, keyed by the rule it judges.
func runJourneyRules(t *testing.T, fixture *qualityFixture, rules []journeyRuleSpec, names map[string]string) map[string]*v1pb.QualityExecution {
	t.Helper()

	selected := make([]string, 0, len(rules))
	for _, rule := range rules {
		selected = append(selected, names[rule.label])
	}
	executions := fixture.runRules(t, selected...)
	require.Len(t, executions, len(rules), "one execution per selected rule")
	byRule := make(map[string]*v1pb.QualityExecution, len(executions))
	for _, planned := range executions {
		require.Equal(t, v1pb.QualityExecutionState_QUALITY_EXECUTION_STATE_PENDING, planned.GetState())
		require.Equal(t, v1pb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_MANUAL, planned.GetTrigger())
		finished := fixture.waitForCompletedExecution(t, planned.GetName())
		require.Equal(t, v1pb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED, finished.GetState())
		byRule[finished.GetRule()] = finished
	}
	return byRule
}

// assertJourneyOutcomes checks each rule's verdict and the metadata every
// execution of the suite shares.
func assertJourneyOutcomes(t *testing.T, rules []journeyRuleSpec, names map[string]string, byRule map[string]*v1pb.QualityExecution, want func(journeyRuleSpec) v1pb.QualityStatus) {
	t.Helper()

	for _, rule := range rules {
		execution := byRule[names[rule.label]]
		require.NotNilf(t, execution, "%s planned no execution", rule.label)
		require.Equalf(t, want(rule), execution.GetOutcome(), "%s verdict (error: %s)", rule.label, execution.GetErrorMessage())
		require.Equal(t, int32(1), execution.GetRuleRevision(), "%s executed its first revision", rule.label)
		require.NotNil(t, execution.GetFinishTime())
		require.False(t, execution.GetTruncated())
		if rule.ruleType == v1pb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT {
			require.Empty(t, execution.GetExecutedSql(), "a metadata rule runs no SQL")
			continue
		}
		require.NotEmptyf(t, execution.GetExecutedSql(), "%s records the SQL it ran", rule.label)
	}
}

// assertJourneyEvidence checks the exact observation the seeded defects produce:
// the row counts, the failed-row counts, and the kinds of measurement that are not
// row counts.
func assertJourneyEvidence(t *testing.T, byRule map[string]*v1pb.QualityExecution, names map[string]string) {
	t.Helper()

	for label, want := range map[string]struct{ failed, total int64 }{
		"row_count":             {0, 10},
		"null_percent":          {2, 10},
		"unique":                {1, 10},
		"unique_combination":    {0, 10},
		"accepted_values":       {1, 10},
		"range":                 {1, 10},
		"regex":                 {0, 10},
		"string_length":         {1, 10},
		"referential_integrity": {1, 10},
		"cross_column":          {1, 10},
		"group_check":           {0, 4},
	} {
		execution := byRule[names[label]]
		require.NotNilf(t, execution, "%s planned no execution", label)
		require.Equalf(t, want.failed, execution.GetFailedRows(), "%s failed rows", label)
		require.Equalf(t, want.total, execution.GetTotalRows(), "%s total rows", label)
	}

	rowCount := byRule[names["row_count"]]
	require.Equal(t, float64(10), rowCount.GetObservedValue())
	require.Equal(t, v1pb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT, rowCount.GetObservedKind())

	freshness := byRule[names["freshness"]]
	require.Equal(t, v1pb.QualityObservedKind_QUALITY_OBSERVED_KIND_SECONDS, freshness.GetObservedKind())
	require.Empty(t, freshness.GetSamples(), "a freshness failure has no violating row")

	customSQL := byRule[names["custom_sql"]]
	require.Equal(t, int64(0), customSQL.GetFailedRows(), "the custom statement returns no failing rows")

	empty := byRule[names["empty_null_percent"]]
	require.Equal(t, int64(0), empty.GetTotalRows())
}

// assertJourneySamples checks the failed-row evidence itself: the NULL marker on a
// NULL sample, the primary key that identifies the row, the duplicate key, and the
// out-of-range value.
func assertJourneySamples(t *testing.T, byRule map[string]*v1pb.QualityExecution, names map[string]string) {
	t.Helper()

	nulls := byRule[names["null_percent"]]
	require.Len(t, nulls.GetSamples(), 2, "both NULL emails are kept as evidence")
	ids := make([]string, 0, len(nulls.GetSamples()))
	for _, sample := range nulls.GetSamples() {
		email := journeySampleColumn(sample, "customer_email")
		require.NotNil(t, email)
		require.NotNil(t, email.IsNull, "a NULL sample carries the is_null marker")
		require.True(t, email.GetIsNull())
		require.Empty(t, email.GetValue())
		id := journeySampleColumn(sample, "id")
		require.NotNil(t, id, "the primary key identifies the sampled row")
		ids = append(ids, id.GetValue())
	}
	require.ElementsMatch(t, []string{"2", "3"}, ids)

	uniqueExecution := byRule[names["unique"]]
	require.NotEmpty(t, uniqueExecution.GetSamples())
	require.True(t, journeyHasSampleValue(uniqueExecution, "external_ref", "R-4"),
		"the duplicated key is the evidence: %v", uniqueExecution.GetSamples())

	rangeExecution := byRule[names["range"]]
	require.Len(t, rangeExecution.GetSamples(), 1)
	amount := journeySampleColumn(rangeExecution.GetSamples()[0], "amount")
	require.NotNil(t, amount)
	observed, err := strconv.ParseFloat(amount.GetValue(), 64)
	require.NoErrorf(t, err, "parse the sampled amount %q", amount.GetValue())
	require.Equal(t, -5.0, observed)
}

// journeySampleColumn returns one column of a failed-row sample, or nil.
func journeySampleColumn(sample *v1pb.FailureSample, column string) *v1pb.ColumnValue {
	for _, cell := range sample.GetColumns() {
		if cell.GetColumn() == column {
			return cell
		}
	}
	return nil
}

// journeyHasSampleValue reports whether any sample carries a value for a column.
func journeyHasSampleValue(execution *v1pb.QualityExecution, column, value string) bool {
	for _, sample := range execution.GetSamples() {
		if cell := journeySampleColumn(sample, column); cell != nil && cell.GetValue() == value {
			return true
		}
	}
	return false
}

// waitForJourneyIssues waits for one open issue per failing rule and proves the
// passing and warning rules opened none.
func waitForJourneyIssues(t *testing.T, fixture *qualityFixture, rules []journeyRuleSpec, names map[string]string) map[string]*v1pb.QualityIssue {
	t.Helper()

	issues := make(map[string]*v1pb.QualityIssue, len(rules))
	for _, rule := range rules {
		ruleName := names[rule.label]
		if rule.firstRun != v1pb.QualityStatus_QUALITY_STATUS_FAIL {
			all, err := fixture.listIssues(t, ruleName, v1pb.QualityIssueStatus_QUALITY_ISSUE_STATUS_UNSPECIFIED)
			require.NoError(t, err)
			require.Emptyf(t, all, "%s must not open an issue", rule.label)
			continue
		}
		issue := fixture.waitForIssue(t, ruleName, v1pb.QualityIssueStatus_QUALITY_ISSUE_STATUS_OPEN)
		require.Equal(t, ruleName, issue.GetRule())
		require.Equal(t, rule.severity, issue.GetSeverity())
		require.Equal(t, int32(1), issue.GetOccurrenceCount())
		issues[ruleName] = issue
	}
	require.Len(t, issues, 7)
	return issues
}

// waitForJourneyIssueOccurrence waits until a second FAIL has moved the issue's
// occurrence count.
func waitForJourneyIssueOccurrence(t *testing.T, fixture *qualityFixture, issue *v1pb.QualityIssue, want int32) {
	t.Helper()

	require.Eventually(t, func() bool {
		resp, err := fixture.dataQuality.GetQualityIssue(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.GetQualityIssueRequest{Name: issue.GetName()}))
		if err != nil {
			return false
		}
		return resp.Msg.GetOccurrenceCount() == want
	}, 30*time.Second, 200*time.Millisecond, "the issue's occurrence count never reached %d: %s", want, fixture.env.ServerLogs())
}

// waitForJourneyIssueStatus waits for one rule's issue to reach a state.
func waitForJourneyIssueStatus(t *testing.T, fixture *qualityFixture, ruleName string, status v1pb.QualityIssueStatus) *v1pb.QualityIssue {
	t.Helper()

	var issue *v1pb.QualityIssue
	require.Eventually(t, func() bool {
		issues, err := fixture.listIssues(t, ruleName, status)
		if err != nil || len(issues) == 0 {
			return false
		}
		issue = issues[0]
		return true
	}, 30*time.Second, 200*time.Millisecond, "the rule's %s issue never appeared: %s", status.String(), fixture.env.ServerLogs())
	return issue
}

// runJourneyScheduledExecution makes one rule's target due and waits for the
// scheduler to plan and finish its execution. The observer store is the only way
// to move a target's next run; the API can only schedule it at least 15 minutes
// out.
func runJourneyScheduledExecution(t *testing.T, fixture *qualityFixture, ruleName string) *v1pb.QualityExecution {
	t.Helper()

	resourceID := journeyRuleResourceID(ruleName)
	rule, err := fixture.env.Store.GetQualityRule(fixture.ctx, &store.FindQualityRuleMessage{ResourceID: &resourceID})
	require.NoError(t, err)
	require.NotNil(t, rule)
	past := time.Now().Add(-time.Minute)
	require.NoError(t, fixture.env.Store.AdvanceQualityTargetNextRun(fixture.ctx, rule.TargetGUID, rule.TargetType, past),
		"make the target due")

	var planned *v1pb.QualityExecution
	require.Eventually(t, func() bool {
		resp, err := fixture.dataQuality.ListQualityExecutions(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.ListQualityExecutionsRequest{
			Parent:   "workspaces/-",
			PageSize: 20,
			Rule:     ruleName,
			Trigger:  v1pb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_SCHEDULED,
		}))
		if err != nil || len(resp.Msg.GetQualityExecutions()) == 0 {
			return false
		}
		planned = resp.Msg.GetQualityExecutions()[0]
		return true
	}, 60*time.Second, 250*time.Millisecond, "the scheduler never planned the due rule: %s", fixture.env.ServerLogs())
	return fixture.waitForCompletedExecution(t, planned.GetName())
}

// assertJourneyRuleScheduledAhead checks that the scheduled pass advanced the
// target's next slot instead of leaving it due for every tick. The advance happens
// when the batch settles, just after the execution finishes, so this polls.
func assertJourneyRuleScheduledAhead(t *testing.T, fixture *qualityFixture, ruleName string) {
	t.Helper()

	resourceID := journeyRuleResourceID(ruleName)
	rule, err := fixture.env.Store.GetQualityRule(fixture.ctx, &store.FindQualityRuleMessage{ResourceID: &resourceID})
	require.NoError(t, err)
	require.NotNil(t, rule)
	require.Eventually(t, func() bool {
		schedule, err := fixture.env.Store.GetQualityTargetSchedule(fixture.ctx, rule.TargetGUID, rule.TargetType)
		if err != nil || schedule == nil || schedule.NextRunAt == nil {
			return false
		}
		return schedule.NextRunAt.After(time.Now())
	}, 10*time.Second, 200*time.Millisecond, "the target's next slot never moved past the run")
}

// journeyRuleResourceID returns the last path segment of a rule resource name.
func journeyRuleResourceID(ruleName string) string {
	return ruleName[strings.LastIndex(ruleName, "/")+1:]
}

// journeyNotificationCount counts the inbox messages of one kind about one rule.
// The inbox is workspace-wide and shared with the other scenarios, so the count
// pages through it and filters by the rule. Offset pagination can repeat messages
// when a parallel scenario inserts at the top between pages, so messages are
// deduplicated by resource name.
func journeyNotificationCount(t *testing.T, fixture *qualityFixture, kind v1pb.DataQualityNotificationKind, ruleName string) int {
	t.Helper()

	seen := make(map[string]struct{})
	pageToken := ""
	for range 10 {
		resp, err := fixture.notifications.ListNotifications(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.ListNotificationsRequest{
			Parent:    "workspaces/-",
			PageSize:  1000,
			PageToken: pageToken,
		}))
		require.NoError(t, err)
		for _, message := range resp.Msg.GetNotifications() {
			detail := message.GetDataQuality()
			if message.GetType() == v1pb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY &&
				detail.GetKind() == kind && detail.GetRule() == ruleName {
				seen[message.GetName()] = struct{}{}
			}
		}
		pageToken = resp.Msg.GetNextPageToken()
		if pageToken == "" {
			return len(seen)
		}
	}
	t.Fatalf("the inbox did not finish paging while counting %s messages for %s", kind.String(), ruleName)
	return 0
}

// assertJourneyOverview checks the overview against the journey's own state. The
// overview aggregates the whole workspace and other scenarios run in parallel, so
// only lower bounds and the response's structural invariants can be exact.
func assertJourneyOverview(t *testing.T, fixture *qualityFixture, minRules, minExecutions, minOpenErrors int) {
	t.Helper()

	resp, err := fixture.dataQuality.GetQualityOverview(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.GetQualityOverviewRequest{Parent: "workspaces/-"}))
	require.NoError(t, err)
	overview := resp.Msg

	require.GreaterOrEqual(t, int(overview.GetRuleCount()), minRules)
	require.GreaterOrEqual(t, int(overview.GetEnabledRuleCount()), minRules)
	require.GreaterOrEqual(t, int(overview.GetMonitoredTableCount()), 3, "the journey watches dq_orders, dq_heartbeat and dq_empty")
	require.GreaterOrEqual(t, int(overview.GetFailingRuleCount()), 7, "the seven failing rules ran within the last day")
	require.GreaterOrEqual(t, int(overview.GetExecutionCount()), minExecutions)
	if minOpenErrors > 0 {
		require.GreaterOrEqual(t, int(overview.GetOpenErrorCount()), minOpenErrors, "the seven ERROR-severity issues are open")
	}

	require.Equal(t, int32(7), overview.GetTrendDays())
	require.Len(t, overview.GetTrend(), 7)
	today := time.Now().UTC()
	for index, point := range overview.GetTrend() {
		wantDate := today.AddDate(0, 0, index-len(overview.GetTrend())+1).Format("2006-01-02")
		require.Equalf(t, wantDate, point.GetDate(), "the trend covers consecutive UTC days ending today")
		require.Equal(t, point.GetTotal(), point.GetPass()+point.GetWarn()+point.GetFail()+point.GetError()+point.GetSkipped()+point.GetNotEvaluated(),
			"a trend point's statuses add up to its total")
	}
	require.GreaterOrEqual(t, int(overview.GetTrend()[len(overview.GetTrend())-1].GetFail()), 7, "today's failing executions are in the trend")

	// Coverage: the denominator is the registry, so every journey table is
	// countable and the numerator can never exceed it.
	require.GreaterOrEqual(t, int(overview.GetTotalTableCount()), 4, "the journey's four tables are registered")
	require.GreaterOrEqual(t, int(overview.GetTotalColumnCount()), int(overview.GetCoveredColumnCount()))
	require.GreaterOrEqual(t, int(overview.GetCoveredColumnCount()), 1, "the column rules cover some columns")
	require.GreaterOrEqual(t, int(overview.GetJudgedTableCount()), 2, "dq_orders and dq_heartbeat are judged; dq_empty is not evaluated")
	require.NotNil(t, overview.QualityScore, "the journey judged rules, so the workspace has a score")
	require.GreaterOrEqual(t, overview.GetQualityScore(), 0.0)
	require.LessOrEqual(t, overview.GetQualityScore(), 100.0)
	require.GreaterOrEqual(t, int(overview.GetScoredRuleCount()), 18, "every journey rule except the empty-data one is judged")

	// The per-instance row must agree with the workspace totals and never claim
	// more coverage than the registry holds.
	var instance *v1pb.QualityCoverageBreakdown
	for _, breakdown := range overview.GetCoverageByInstance() {
		if breakdown.GetKey() == fixture.instanceID {
			instance = breakdown
			break
		}
	}
	require.NotNil(t, instance, "the journey instance has a coverage row: %s", fixture.env.ServerLogs())
	require.GreaterOrEqual(t, int(instance.GetTotalTableCount()), 4)
	require.GreaterOrEqual(t, int(instance.GetMonitoredTableCount()), 3)
	require.LessOrEqual(t, int(instance.GetMonitoredTableCount()), int(instance.GetTotalTableCount()))
	require.GreaterOrEqual(t, int(instance.GetCoveredColumnCount()), 1)
	require.LessOrEqual(t, int(instance.GetCoveredColumnCount()), int(instance.GetTotalColumnCount()))

	require.NotEmpty(t, overview.GetCoverageByEnvironment(), "the journey instance contributes an environment row")
	for _, breakdown := range overview.GetCoverageByEnvironment() {
		require.LessOrEqual(t, int(breakdown.GetMonitoredTableCount()), int(breakdown.GetTotalTableCount()))
		require.LessOrEqual(t, int(breakdown.GetCoveredColumnCount()), int(breakdown.GetTotalColumnCount()))
	}
	require.NotEmpty(t, overview.GetScoreByDimension())
	for _, dimension := range overview.GetScoreByDimension() {
		require.LessOrEqual(t, dimension.GetScoredRuleCount(), dimension.GetRuleCount())
		require.LessOrEqual(t, dimension.GetFailingRuleCount(), dimension.GetScoredRuleCount())
		if dimension.QualityScore != nil {
			require.GreaterOrEqual(t, dimension.GetQualityScore(), 0.0)
			require.LessOrEqual(t, dimension.GetQualityScore(), 100.0)
		}
	}
	require.NotEmpty(t, overview.GetScoreByOwner(), "the journey's unowned rules form the empty-owner row")
	for _, owner := range overview.GetScoreByOwner() {
		require.LessOrEqual(t, owner.GetScoredRuleCount(), owner.GetRuleCount())
		require.LessOrEqual(t, owner.GetFailingRuleCount(), owner.GetScoredRuleCount())
		if owner.QualityScore != nil {
			require.GreaterOrEqual(t, owner.GetQualityScore(), 0.0)
			require.LessOrEqual(t, owner.GetQualityScore(), 100.0)
		}
	}
}

// assertJourneyTargetHealth pins the target summary's score and recent pass rate
// on one target after its first batch. The directory row and the workspace header
// are two reads of the same state, so they must agree.
func assertJourneyTargetHealth(t *testing.T, fixture *qualityFixture, targetGUID string, wantScored int32, wantPassRate float64) {
	t.Helper()

	listed, err := fixture.dataQuality.ListQualityTargets(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.ListQualityTargetsRequest{
		Parent:   "workspaces/-",
		PageSize: 1000,
		Instance: fixture.instanceID,
	}))
	require.NoError(t, err)
	var summary *v1pb.QualityTargetSummary
	for _, candidate := range listed.Msg.GetQualityTargets() {
		if candidate.GetTarget().GetMetaGuid() == targetGUID {
			summary = candidate
			break
		}
	}
	require.NotNil(t, summary, "the target directory lists the journey table: %s", fixture.env.ServerLogs())
	require.NotNil(t, summary.QualityScore, "the target's judged rules produce a score")
	require.GreaterOrEqual(t, summary.GetQualityScore(), 0.0)
	require.LessOrEqual(t, summary.GetQualityScore(), 100.0)
	require.Equal(t, wantScored, summary.GetScoredRuleCount())
	require.NotNil(t, summary.RecentPassRate, "the first batch judged every enabled rule")
	require.InDelta(t, wantPassRate, summary.GetRecentPassRate(), 1e-9)
	require.Equal(t, wantScored, summary.GetRecentExecutionCount(), "the first batch completed one execution per rule")

	target, err := fixture.dataQuality.GetQualityTarget(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.GetQualityTargetRequest{
		Parent:     "workspaces/-",
		TargetGuid: targetGUID,
	}))
	require.NoError(t, err)
	require.NotNil(t, target.Msg.GetSummary().QualityScore)
	require.InDelta(t, summary.GetQualityScore(), target.Msg.GetSummary().GetQualityScore(), 1e-9)
	require.Equal(t, summary.GetScoredRuleCount(), target.Msg.GetSummary().GetScoredRuleCount())
	require.InDelta(t, summary.GetRecentPassRate(), target.Msg.GetSummary().GetRecentPassRate(), 1e-9)
	require.Equal(t, summary.GetRecentExecutionCount(), target.Msg.GetSummary().GetRecentExecutionCount())
}

// assertJourneyProfileAndLabel profiles the journey table and writes a label by
// hand, the two deterministic facts the AI-assisted paths build on.
func assertJourneyProfileAndLabel(t *testing.T, fixture *qualityFixture, tableGUID string) {
	t.Helper()

	profiled, err := fixture.dataQuality.ProfileColumns(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.ProfileColumnsRequest{
		Parent:    "workspaces/-",
		TableGuid: tableGUID,
		MetaType:  v1pb.MetaType_TABLE,
	}))
	require.NoError(t, err)
	require.NotEmpty(t, profiled.Msg.GetColumnProfiles())

	var email *v1pb.ColumnProfile
	for _, profile := range profiled.Msg.GetColumnProfiles() {
		require.Equal(t, tableGUID, profile.GetTableGuid())
		require.Equal(t, int64(10), profile.GetProfile().GetTotalRows())
		if profile.GetColumn() == "customer_email" {
			email = profile
		}
	}
	require.NotNil(t, email, "the email column is profiled")
	require.Equal(t, int64(2), email.GetProfile().GetNullCount())
	require.Equal(t, int64(8), email.GetProfile().GetDistinctCount())

	accepted, err := fixture.dataQuality.UpdateColumnLabel(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.UpdateColumnLabelRequest{
		Parent:    "workspaces/-",
		TableGuid: tableGUID,
		MetaType:  v1pb.MetaType_TABLE,
		Column:    "customer_email",
		Label:     v1pb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_EMAIL,
		Action:    v1pb.ColumnLabelAction_COLUMN_LABEL_ACTION_ACCEPT,
	}))
	require.NoError(t, err)
	require.Equal(t, v1pb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_EMAIL, accepted.Msg.GetColumnLabel().GetLabel())
	require.Equal(t, v1pb.QualityLabelSource_QUALITY_LABEL_SOURCE_MANUAL, accepted.Msg.GetColumnLabel().GetSource())

	listed, err := fixture.dataQuality.ListColumnLabels(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.ListColumnLabelsRequest{
		Parent:    "workspaces/-",
		TableGuid: tableGUID,
		MetaType:  v1pb.MetaType_TABLE,
	}))
	require.NoError(t, err)
	require.Len(t, listed.Msg.GetColumnLabels(), 1)
	require.Equal(t, "customer_email", listed.Msg.GetColumnLabels()[0].GetColumn())
}

// repairJourneyData fixes the seeded defects directly in the source database.
func repairJourneyData(t *testing.T, fixture *qualityFixture) {
	t.Helper()

	require.NoError(t, fixture.env.ExecMySQL(fixture.ctx,
		"USE "+quoteMySQLIdentifier(fixture.sourceDatabase)+";"+journeyRepairSQL), "repair the journey source")
}

// assertJourneyMissingReadOnlySource removes the instance's READ_ONLY source after
// the rule was saved: the execution is ERROR, not FAIL, and the operational failure
// is announced to the administrators.
func assertJourneyMissingReadOnlySource(t *testing.T, fixture *qualityFixture, ruleName string) {
	t.Helper()

	require.NoError(t, fixture.env.DeleteDataSource(fixture.ctx, common.FormatDataSource(fixture.instanceID, "readonly")),
		"remove the read-only source the rule needs")
	execution := fixture.runToCompletion(t, ruleName)
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_ERROR, execution.GetOutcome(), "a missing source is an execution error")
	require.Equal(t, dataquality.ErrCodeNoReadOnly, execution.GetErrorCode())
	require.NotEmpty(t, execution.GetErrorMessage())
	require.Empty(t, execution.GetSamples())

	message := fixture.waitForNotification(t, v1pb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_EXECUTION_FAILED, ruleName)
	require.Equal(t, execution.GetName(), message.GetDataQuality().GetExecution())
	require.Equal(t, dataquality.ErrCodeNoReadOnly, message.GetDataQuality().GetErrorCode())

	// Restore a read-only source for the remaining phases. A new id avoids
	// depending on what deleting and recreating the same resource allows.
	_, err := fixture.env.CreateDataSource(fixture.ctx, common.FormatInstance(fixture.instanceID), &v1pb.DataSource{
		Type:     v1pb.DataSourceType_READ_ONLY,
		Username: "root",
		Password: "root",
		Host:     fixture.env.MySQLHost,
		Port:     fixture.env.MySQLPort,
	}, "readonly2", false)
	require.NoError(t, err, "restore a read-only source")
}

// assertJourneySchemaDriftAfterDDLChange changes the table's schema after the rule
// was saved and expects the metadata rule to report the drift. MySQL does not
// implement db.ObjectDefinitionReader, so this engine stores no DDL hash and
// target_schema_changed is always false; the registry column snapshot the drift
// rule compares is what makes the change visible here.
func assertJourneySchemaDriftAfterDDLChange(t *testing.T, fixture *qualityFixture, driftRuleName string) {
	t.Helper()

	require.NoError(t, fixture.env.ExecMySQL(fixture.ctx, fmt.Sprintf(
		"USE %s; ALTER TABLE dq_orders ADD COLUMN nickname VARCHAR(32) NULL;",
		quoteMySQLIdentifier(fixture.sourceDatabase))), "change the target schema")
	fixture.env.SyncDatabase(fixture.ctx, t, fixture.databaseName)

	drifted := fixture.runToCompletion(t, driftRuleName)
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_FAIL, drifted.GetOutcome(), "the added column is drift")
	require.GreaterOrEqual(t, drifted.GetFailedRows(), int64(1))
	require.NotEmpty(t, drifted.GetSamples(), "the drift items are the evidence")
	require.Empty(t, drifted.GetExecutedSql(), "a metadata rule runs no SQL")
}

// assertJourneyOrphanedTarget drops the table a rule watches: the execution is
// SKIPPED(ORPHANED), it opens no issue and sends no message, and the rule read
// path explains what happened.
func assertJourneyOrphanedTarget(t *testing.T, fixture *qualityFixture, orphanTableGUID string) string {
	t.Helper()

	minRows := int64(1)
	rule := fixture.createJourneyRule(t, journeyRuleSpec{
		label:      "orphan_target",
		ruleType:   v1pb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		dimension:  v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS,
		severity:   v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		targetGUID: orphanTableGUID,
		config: &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_RowCount{RowCount: &v1pb.RowCountConfig{
			MinRows: &minRows,
		}}},
	})
	before := fixture.runToCompletion(t, rule.GetName())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_PASS, before.GetOutcome())

	require.NoError(t, fixture.env.ExecMySQL(fixture.ctx, fmt.Sprintf(
		"USE %s; DROP TABLE dq_orphan_target;", quoteMySQLIdentifier(fixture.sourceDatabase))), "drop the watched table")
	fixture.env.SyncDatabase(fixture.ctx, t, fixture.databaseName)

	skipped := fixture.runToCompletion(t, rule.GetName())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_SKIPPED, skipped.GetOutcome())
	require.Equal(t, dataquality.ErrCodeOrphaned, skipped.GetErrorCode())
	require.Empty(t, skipped.GetSamples())

	openIssues, err := fixture.listIssues(t, rule.GetName(), v1pb.QualityIssueStatus_QUALITY_ISSUE_STATUS_OPEN)
	require.NoError(t, err)
	require.Empty(t, openIssues, "a rule that watches nothing is not a data problem")
	for _, kind := range []v1pb.DataQualityNotificationKind{
		v1pb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_OPENED,
		v1pb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_EXECUTION_FAILED,
	} {
		require.Equal(t, 0, journeyNotificationCount(t, fixture, kind, rule.GetName()))
	}

	// The dropped table removed its coverage denominator row. The still-enabled
	// rule must not sit in the numerator, or coverage would exceed 100%.
	overview, err := fixture.dataQuality.GetQualityOverview(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.GetQualityOverviewRequest{Parent: "workspaces/-"}))
	require.NoError(t, err)
	require.LessOrEqual(t, int(overview.Msg.GetMonitoredTableCount()), int(overview.Msg.GetTotalTableCount()),
		"an enabled rule that outlived its target cannot push coverage past 100%%")
	require.LessOrEqual(t, int(overview.Msg.GetJudgedTableCount()), int(overview.Msg.GetMonitoredTableCount()))
	var instance *v1pb.QualityCoverageBreakdown
	for _, breakdown := range overview.Msg.GetCoverageByInstance() {
		if breakdown.GetKey() == fixture.instanceID {
			instance = breakdown
			break
		}
	}
	require.NotNil(t, instance, "the journey instance still has a coverage row: %s", fixture.env.ServerLogs())
	require.Equal(t, int32(3), instance.GetMonitoredTableCount(),
		"dq_orders, dq_heartbeat and dq_empty remain the monitored targets; the dropped table is not counted")
	require.LessOrEqual(t, int(instance.GetMonitoredTableCount()), int(instance.GetTotalTableCount()))
	require.LessOrEqual(t, int(instance.GetCoveredColumnCount()), int(instance.GetTotalColumnCount()))

	read, err := fixture.dataQuality.GetQualityRule(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.GetQualityRuleRequest{Name: rule.GetName()}))
	require.NoError(t, err)
	require.NotEmpty(t, read.Msg.GetOrphanedReason(), "the rule read path explains the orphaned target")

	// The list path fills the same projection.
	listed, err := fixture.dataQuality.ListQualityRules(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.ListQualityRulesRequest{
		Parent: "workspaces/-", PageSize: 100, TargetGuid: orphanTableGUID,
	}))
	require.NoError(t, err)
	require.Len(t, listed.Msg.GetQualityRules(), 1)
	require.NotEmpty(t, listed.Msg.GetQualityRules()[0].GetOrphanedReason())
	return rule.GetName()
}

// assertJourneyDeletedInstanceCoverage pins the coverage numerator guard against
// a soft-deleted instance. DeleteInstance keeps the registry rows, so a rule's
// target still exists by GUID, while the coverage denominator excludes objects
// under a deleted instance; the existence probe must exclude them too or coverage
// would count a denominator-less target. The observer store is read directly
// because the API's workspace totals are shared with the parallel scenarios.
func assertJourneyDeletedInstanceCoverage(t *testing.T, fixture *qualityFixture, targetGUID string) {
	t.Helper()

	_, err := fixture.env.DeleteInstance(fixture.ctx, common.FormatInstance(fixture.instanceID))
	require.NoError(t, err, "soft-delete the journey instance")
	existing, err := fixture.env.Store.ListExistingMetaRegistryTargets(fixture.ctx, []store.MetaGUIDKey{
		{GUID: targetGUID, ObjectType: storepb.MetaType_TABLE},
	})
	require.NoError(t, err)
	require.Empty(t, existing, "a soft-deleted instance's target is not a coverage denominator row")
	coverage, err := fixture.env.Store.ListQualityCoverage(fixture.ctx)
	require.NoError(t, err)
	for _, row := range coverage {
		require.NotEqual(t, fixture.instanceID, row.InstanceResourceID, "a soft-deleted instance has no coverage row")
	}
}

// assertJourneyArchive archives every rule and proves the history stays readable.
func assertJourneyArchive(t *testing.T, fixture *qualityFixture, ruleNames []string) {
	t.Helper()

	token := fixture.env.AdminToken()
	for _, ruleName := range ruleNames {
		_, err := fixture.dataQuality.DeleteQualityRule(fixture.ctx, withToken(token, &v1pb.DeleteQualityRuleRequest{Name: ruleName}))
		require.NoErrorf(t, err, "archive %s", ruleName)
	}

	defaultList, err := fixture.dataQuality.ListQualityRules(fixture.ctx, withToken(token, &v1pb.ListQualityRulesRequest{
		Parent: "workspaces/-", PageSize: 1000,
	}))
	require.NoError(t, err)
	live := make(map[string]struct{}, len(defaultList.Msg.GetQualityRules()))
	for _, rule := range defaultList.Msg.GetQualityRules() {
		live[rule.GetName()] = struct{}{}
	}
	for _, ruleName := range ruleNames {
		require.NotContains(t, live, ruleName, "an archived rule leaves the default list")
	}

	archivedList, err := fixture.dataQuality.ListQualityRules(fixture.ctx, withToken(token, &v1pb.ListQualityRulesRequest{
		Parent: "workspaces/-", PageSize: 1000, State: v1pb.QualityRuleState_QUALITY_RULE_STATE_ARCHIVED,
	}))
	require.NoError(t, err)
	archived := make(map[string]struct{}, len(archivedList.Msg.GetQualityRules()))
	for _, rule := range archivedList.Msg.GetQualityRules() {
		archived[rule.GetName()] = struct{}{}
	}
	for _, ruleName := range ruleNames {
		require.Contains(t, archived, ruleName, "an archived rule is discoverable through the ARCHIVED filter")
	}
	require.NotEmpty(t, fixture.executionsForRule(t, ruleNames[0]), "archiving keeps the rule's executions")
}
