package metrics

import (
	"database/sql"
	"net/http"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// descPattern extracts the fully qualified name and the variable label names
// from prometheus.Desc.String(). The Desc type exposes no accessor for either,
// and gathering cannot see a vec that has no children yet.
var descPattern = regexp.MustCompile(`fqName: "([^"]+)", help: ".*", constLabels: \{[^}]*\}, variableLabels: \{([^}]*)\}`)

// describe returns one collector's metric name and label names. A collector that
// describes more than one metric (there is none today) fails the test instead of
// silently returning the last one.
func describe(t *testing.T, collector prometheus.Collector) (string, []string) {
	t.Helper()
	descriptions := make(chan *prometheus.Desc, 8)
	go func() {
		collector.Describe(descriptions)
		close(descriptions)
	}()
	name := ""
	var labels []string
	for desc := range descriptions {
		match := descPattern.FindStringSubmatch(desc.String())
		require.NotNil(t, match, "unparsable descriptor %q", desc.String())
		if name != "" {
			require.Equal(t, name, match[1], "one collector described two metric names")
		}
		name = match[1]
		labels = nil
		for _, raw := range strings.Split(match[2], ",") {
			raw = strings.TrimSpace(strings.Trim(raw, `"`))
			if raw != "" {
				labels = append(labels, raw)
			}
		}
	}
	return name, labels
}

// TestRegisteredInstruments pins the public metric names and their label sets.
// A new metric or label without updating this table fails here, which is the
// guard against an accidental unbounded label reaching the endpoint.
func TestRegisteredInstruments(t *testing.T) {
	t.Parallel()

	got := make(map[string][]string)
	for _, collector := range New().collectors() {
		name, labels := describe(t, collector)
		require.NotEmpty(t, name)
		got[name] = labels
	}

	want := map[string][]string{
		// HTTP / RPC.
		"metaxisdata_http_requests_total":           {"method", "route", "status"},
		"metaxisdata_http_request_duration_seconds": {"method", "route"},
		"metaxisdata_rpc_requests_total":            {"service", "method", "code"},
		"metaxisdata_rpc_request_duration_seconds":  {"service", "method"},
		// Runner lifecycle.
		"metaxisdata_runner_up":           {"runner"},
		"metaxisdata_runner_panics_total": {"runner"},
		// Schema sync.
		"metaxisdata_schemasync_scans_total":                      {"outcome"},
		"metaxisdata_schemasync_scan_duration_seconds":            nil,
		"metaxisdata_schemasync_instance_syncs_total":             {"engine", "outcome"},
		"metaxisdata_schemasync_instance_sync_duration_seconds":   {"engine"},
		"metaxisdata_schemasync_database_syncs_total":             {"engine", "outcome"},
		"metaxisdata_schemasync_database_sync_duration_seconds":   {"engine"},
		"metaxisdata_schemasync_failures_total":                   {"stage", "reason"},
		"metaxisdata_schemasync_driver_call_duration_seconds":     {"engine", "call"},
		"metaxisdata_schemasync_meta_resources_written_total":     {"object_type"},
		"metaxisdata_schemasync_meta_resources_deleted_total":     {"object_type"},
		"metaxisdata_schemasync_retries_total":                    {"attempt"},
		"metaxisdata_schemasync_retries_exhausted_total":          {"engine"},
		"metaxisdata_schemasync_connection_limit_deferrals_total": {"kind"},
		// Lineage analysis.
		"metaxisdata_lineage_analysis_objects_total":           {"meta_type", "engine", "result"},
		"metaxisdata_lineage_analyze_duration_seconds":         {"engine", "meta_type"},
		"metaxisdata_lineage_analysis_in_flight":               nil,
		"metaxisdata_lineage_analysis_retries_exhausted_total": {"meta_type"},
		"metaxisdata_lineage_edges_written_total":              {"meta_type", "kind"},
		// OpenLineage ingestion.
		"metaxisdata_openlineage_ingest_failures_total":           {"failure_kind", "route"},
		"metaxisdata_openlineage_ingest_events_total":             {"route", "result", "reason"},
		"metaxisdata_openlineage_ingest_process_duration_seconds": {"route"},
		"metaxisdata_openlineage_edges_total":                     {"kind"},
		// Data quality.
		"metaxisdata_quality_due_scan_duration_seconds":       nil,
		"metaxisdata_quality_due_rules":                       nil,
		"metaxisdata_quality_runs_created_total":              {"trigger"},
		"metaxisdata_quality_runs_completed_total":            {"outcome"},
		"metaxisdata_quality_run_admissions_total":            {"trigger", "result"},
		"metaxisdata_quality_runs_deferred_total":             {"reason"},
		"metaxisdata_quality_run_abandoned_total":             {"reason"},
		"metaxisdata_quality_runs_in_progress":                nil,
		"metaxisdata_quality_manual_enqueue_rejected_total":   nil,
		"metaxisdata_quality_rule_executions_total":           {"rule_type", "status"},
		"metaxisdata_quality_rule_execution_duration_seconds": {"rule_type"},
		"metaxisdata_quality_rule_errors_total":               {"rule_type", "error_code"},
		// Target read-only queries.
		"metaxisdata_query_readonly_total":            {"purpose", "outcome"},
		"metaxisdata_query_readonly_duration_seconds": {"purpose"},
		// LLM.
		"metaxisdata_llm_calls_total":            {"feature", "provider", "outcome"},
		"metaxisdata_llm_call_duration_seconds":  {"feature", "provider"},
		"metaxisdata_llm_tokens_total":           {"feature", "provider", "kind"},
		"metaxisdata_llm_usage_missing_total":    {"feature"},
		"metaxisdata_llm_parse_failures_total":   {"feature"},
		"metaxisdata_llm_quota_rejections_total": {"kind"},
		// Maintenance.
		"metaxisdata_maintenance_passes_total":          {"outcome"},
		"metaxisdata_maintenance_pass_duration_seconds": nil,
		"metaxisdata_maintenance_deleted_rows_total":    {"item"},
		"metaxisdata_maintenance_item_failures_total":   {"item"},
		// Lineage revalidation runner.
		"metaxisdata_lineagevalidation_triggers_total":        {"result"},
		"metaxisdata_lineagevalidation_passes_total":          {"outcome"},
		"metaxisdata_lineagevalidation_pass_duration_seconds": nil,
		"metaxisdata_lineagevalidation_edges_total":           {"result"},
		// Instance connection governor.
		"metaxisdata_instance_connection_try_acquire_rejected_total": nil,
	}

	names := make([]string, 0, len(got))
	for name := range got {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		labels, ok := want[name]
		require.True(t, ok, "metric %q is not in the golden table", name)
		require.Equal(t, labels, got[name], "label set of %q", name)
	}
	for name := range want {
		_, ok := got[name]
		require.True(t, ok, "golden metric %q is not registered", name)
	}
}

// TestScrapeCollectors pins the metrics computed at scrape time.
func TestScrapeCollectors(t *testing.T) {
	t.Parallel()

	registry := prometheus.NewRegistry()
	registry.MustRegister(
		NewLineageAnalysisQueueDepth(func() (int, int) { return 3, 4 }),
		NewInstanceConnectionInUse(func() int { return 2 }),
		NewInstanceConnectionWaiters(func() map[string]int {
			return map[string]int{"background": 1, "manual": 2, "interactive": 3}
		}),
		NewQualityManualQueueDepth(func() int { return 5 }),
	)

	families, err := registry.Gather()
	require.NoError(t, err)
	got := make(map[string]bool, len(families))
	for _, family := range families {
		got[family.GetName()] = true
	}
	require.Equal(t, map[string]bool{
		"metaxisdata_lineage_analysis_queue_depth": true,
		"metaxisdata_instance_connection_in_use":   true,
		"metaxisdata_instance_connection_waiters":  true,
		"metaxisdata_quality_manual_queue_depth":   true,
	}, got)

	// The scrape-time gauges must report the snapshot they were given, with the
	// bounded label values the catalogue names.
	require.Equal(t, 3.0, sampleValue(t, families, "metaxisdata_lineage_analysis_queue_depth", map[string]string{"queue": "event"}))
	require.Equal(t, 4.0, sampleValue(t, families, "metaxisdata_lineage_analysis_queue_depth", map[string]string{"queue": "scan"}))
	require.Equal(t, 2.0, sampleValue(t, families, "metaxisdata_instance_connection_in_use", nil))
	require.Equal(t, 1.0, sampleValue(t, families, "metaxisdata_instance_connection_waiters", map[string]string{"priority": "background"}))
	require.Equal(t, 2.0, sampleValue(t, families, "metaxisdata_instance_connection_waiters", map[string]string{"priority": "manual"}))
	require.Equal(t, 3.0, sampleValue(t, families, "metaxisdata_instance_connection_waiters", map[string]string{"priority": "interactive"}))
	require.Equal(t, 5.0, sampleValue(t, families, "metaxisdata_quality_manual_queue_depth", nil))
}

// TestNilMetricsIsNoop pins that a nil Metrics disables instrumentation instead
// of panicking, which is what lets tests and constructors omit it.
func TestNilMetricsIsNoop(t *testing.T) {
	t.Parallel()

	var m *Metrics
	m.ObserveHTTPRequest(http.MethodGet, "/x", 200, time.Second)
	m.ObserveRPCUnary("InstanceService", "Get", "ok", time.Second)
	m.ObserveRPCStream("ExplainSQLService", "ExplainSQL", "ok")
	m.RunnerStarted("schemasync")
	m.RunnerStopped("schemasync")
	m.RunnerPanicked("schemasync")
	m.ObserveSchemaSyncScan(true, time.Second)
	m.ObserveInstanceSync(storepb.Engine_MYSQL, true, time.Second)
	m.ObserveDatabaseSync(storepb.Engine_MYSQL, false, time.Second)
	m.CountSchemaSyncFailure(SyncStageDatabase, FailureReasonOther)
	m.ObserveDriverCall(storepb.Engine_MYSQL, DriverCallSyncDBSchema, time.Second)
	m.AddMetaResourcesWritten(storepb.MetaType_TABLE, 1)
	m.AddMetaResourcesDeleted(storepb.MetaType_TABLE, 1)
	m.CountSchemaSyncRetry(1)
	m.CountSchemaSyncRetriesExhausted(storepb.Engine_MYSQL)
	m.CountConnectionLimitDeferral(DeferralDatabase)
	m.ObserveLineageAnalysis(storepb.MetaType_VIEW, storepb.Engine_MYSQL, AnalysisStored)
	m.ObserveLineageAnalyzeDuration(storepb.Engine_MYSQL, storepb.MetaType_VIEW, time.Second)
	m.IncLineageAnalysisInFlight()
	m.DecLineageAnalysisInFlight()
	m.CountLineageAnalysisRetriesExhausted(storepb.MetaType_VIEW)
	m.AddLineageEdges(storepb.MetaType_VIEW, EdgeKindColumn, 1)
	m.CountOpenLineageIngestFailure("INVALID_EVENT", IngestRouteEvent)
	m.AddOpenLineageIngestEvents(IngestRouteEvent, IngestResultAccepted, IngestReasonParse, 1)
	m.ObserveOpenLineageProcessDuration(IngestRouteEvent, time.Second)
	m.AddOpenLineageEdges(EdgeKindTable, 1)
	m.ObserveQualityDueScan(time.Second)
	m.ObserveQualityDueRules(1)
	m.CountQualityRunCreated(QualityTriggerManual)
	m.CountQualityRunCompleted(storepb.QualityStatus_QUALITY_STATUS_PASS)
	m.CountQualityRunAdmission(QualityTriggerManual, true)
	m.CountQualityRunsDeferred(QualityDeferralAdmissionTimeout)
	m.CountQualityRunAbandoned(QualityAbandonedQueueFull)
	m.IncQualityRunsInProgress()
	m.DecQualityRunsInProgress()
	m.CountQualityManualEnqueueRejected()
	m.ObserveQualityRuleExecution(storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, storepb.QualityStatus_QUALITY_STATUS_FAIL, time.Second)
	m.CountQualityRuleError(storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, "CONNECTION")
	m.ObserveReadOnlyQuery(QueryPurposeRuleAggregate, true, time.Second)
	m.ObserveLLMCall("explain_sql", "openai", LLMOutcomeSuccess, time.Second, 1, 2)
	m.CountLLMUsageMissing("explain_sql")
	m.CountLLMParseFailure("dq_rule_draft")
	m.CountLLMQuotaRejection(LLMQuotaAnalysis)
	m.ObserveMaintenancePass(true, time.Second, map[string]int64{"explain_sql_cache": 1}, nil)
	m.CountLineageValidationTrigger(ValidationTriggerQueued)
	m.ObserveLineageValidationPass(ValidationPassOK, time.Second, 1, 2)
	m.CountInstanceConnectionTryAcquireRejected()
}

// TestLabelMappings pins the enum-to-label mapping. The values are part of the
// public contract once scraped, so they are asserted rather than left to the
// compiler.
func TestLabelMappings(t *testing.T) {
	t.Parallel()

	require.Equal(t, "mysql", engineLabel(storepb.Engine_MYSQL))
	require.Equal(t, "postgres", engineLabel(storepb.Engine_POSTGRES))
	require.Equal(t, "mssql", engineLabel(storepb.Engine_MSSQL))
	require.Equal(t, "unknown", engineLabel(storepb.Engine_ENGINE_UNSPECIFIED))

	require.Equal(t, "materialized_view", analysisMetaTypeLabel(storepb.MetaType_MATERIALIZED_VIEW))
	require.Equal(t, "other", analysisMetaTypeLabel(storepb.MetaType_TABLE))
	require.Equal(t, "external_table", objectTypeLabel(storepb.MetaType_EXTERNAL_TABLE))
	require.Equal(t, "other", objectTypeLabel(storepb.MetaType_OPENLINEAGE))

	require.Equal(t, "row_count", qualityRuleTypeLabel(storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT))
	require.Equal(t, "cross_table", qualityRuleTypeLabel(storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE))
	require.Equal(t, "unspecified", qualityRuleTypeLabel(storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED))
	require.Equal(t, "not_evaluated", qualityStatusLabel(storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED))
	require.Equal(t, "none", qualityErrorCodeLabel(""))
	require.Equal(t, "sql_syntax", qualityErrorCodeLabel("SQL_SYNTAX"))
	require.Equal(t, "other", qualityErrorCodeLabel("A_CODE_FROM_THE_FUTURE"))

	require.Equal(t, "get", httpMethodLabel(http.MethodGet))
	require.Equal(t, "other", httpMethodLabel("FROBNICATE"))
	require.Equal(t, "other", httpMethodLabel(""))

	require.Equal(t, "success", outcomeLabel(true))
	require.Equal(t, "failure", outcomeLabel(false))
}

// sampleValue returns the value of the sample in the named family whose labels
// match every pair given, and fails when there is no such sample.
func sampleValue(t *testing.T, families []*dto.MetricFamily, name string, labels map[string]string) float64 {
	t.Helper()
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			matches := true
			for key, want := range labels {
				found := false
				for _, pair := range metric.GetLabel() {
					if pair.GetName() == key && pair.GetValue() == want {
						found = true
						break
					}
				}
				if !found {
					matches = false
					break
				}
			}
			if !matches {
				continue
			}
			if metric.GetCounter() != nil {
				return metric.GetCounter().GetValue()
			}
			if metric.GetGauge() != nil {
				return metric.GetGauge().GetValue()
			}
			if metric.GetHistogram() != nil {
				return float64(metric.GetHistogram().GetSampleCount())
			}
		}
	}
	t.Fatalf("no sample %q with labels %v", name, labels)
	return 0
}

// TestRecordersEmitExpectedLabels pins that the recorders reach the instruments
// with the mapped label values; it is the end-to-end counter part of the golden
// table above.
func TestRecordersEmitExpectedLabels(t *testing.T) {
	t.Parallel()

	m := New()
	registry := prometheus.NewRegistry()
	require.NoError(t, m.Register(registry))

	m.ObserveHTTPRequest(http.MethodGet, "/api/version", http.StatusOK, 100*time.Millisecond)
	m.ObserveRPCUnary("InstanceService", "Get", "ok", time.Second)
	m.ObserveInstanceSync(storepb.Engine_MYSQL, true, time.Second)
	m.CountSchemaSyncFailure(SyncStageDatabase, FailureReasonTimeout)
	m.ObserveQualityRuleExecution(storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, storepb.QualityStatus_QUALITY_STATUS_FAIL, time.Second)
	m.CountQualityRuleError(storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, "CONNECTION")
	m.ObserveLLMCall("explain_sql", "openai", LLMOutcomeSuccess, time.Second, 10, 5)
	m.CountLLMQuotaRejection(LLMQuotaAnalysis)
	m.CountInstanceConnectionTryAcquireRejected()
	m.RunnerPanicked("schemasync")
	// net/http accepts arbitrary method tokens; the recorder must not turn one
	// into a series.
	m.ObserveHTTPRequest("FROBNICATE", "/api/version", http.StatusMethodNotAllowed, 100*time.Millisecond)

	families, err := registry.Gather()
	require.NoError(t, err)

	require.Equal(t, 1.0, sampleValue(t, families, "metaxisdata_http_requests_total", map[string]string{
		"method": "get", "route": "/api/version", "status": "200",
	}))
	require.Equal(t, 1.0, sampleValue(t, families, "metaxisdata_http_requests_total", map[string]string{
		"method": "other", "route": "/api/version", "status": "405",
	}))
	require.Equal(t, 1.0, sampleValue(t, families, "metaxisdata_rpc_requests_total", map[string]string{
		"service": "InstanceService", "method": "Get", "code": "ok",
	}))
	require.Equal(t, 1.0, sampleValue(t, families, "metaxisdata_schemasync_instance_syncs_total", map[string]string{
		"engine": "mysql", "outcome": "success",
	}))
	require.Equal(t, 1.0, sampleValue(t, families, "metaxisdata_schemasync_failures_total", map[string]string{
		"stage": SyncStageDatabase, "reason": FailureReasonTimeout,
	}))
	require.Equal(t, 1.0, sampleValue(t, families, "metaxisdata_quality_rule_executions_total", map[string]string{
		"rule_type": "row_count", "status": "fail",
	}))
	require.Equal(t, 1.0, sampleValue(t, families, "metaxisdata_quality_rule_errors_total", map[string]string{
		"rule_type": "row_count", "error_code": "connection",
	}))
	require.Equal(t, 1.0, sampleValue(t, families, "metaxisdata_llm_calls_total", map[string]string{
		"feature": "explain_sql", "provider": "openai", "outcome": LLMOutcomeSuccess,
	}))
	require.Equal(t, 10.0, sampleValue(t, families, "metaxisdata_llm_tokens_total", map[string]string{
		"feature": "explain_sql", "provider": "openai", "kind": LLMTokenPrompt,
	}))
	require.Equal(t, 1.0, sampleValue(t, families, "metaxisdata_llm_quota_rejections_total", map[string]string{
		"kind": LLMQuotaAnalysis,
	}))
	require.Equal(t, 1.0, sampleValue(t, families, "metaxisdata_instance_connection_try_acquire_rejected_total", nil))
	require.Equal(t, 1.0, sampleValue(t, families, "metaxisdata_runner_panics_total", map[string]string{
		"runner": "schemasync",
	}))
}

// TestEveryInstrumentIsRegistered mirrors the instruments New builds against the
// collectors Register consumes. Adding an instrument to a domain struct and New
// but forgetting the domain's collectors() would silently never expose it; this
// walk makes that a test failure instead.
func TestEveryInstrumentIsRegistered(t *testing.T) {
	t.Parallel()

	m := New()
	registered := make(map[prometheus.Collector]bool)
	for _, collector := range m.collectors() {
		registered[collector] = true
	}

	var missing []string
	var walk func(prefix string, value reflect.Value)
	walk = func(prefix string, value reflect.Value) {
		valueType := value.Type()
		for i := 0; i < value.NumField(); i++ {
			field := value.Field(i)
			name := prefix + valueType.Field(i).Name
			if field.Kind() == reflect.Struct {
				walk(name+".", field)
				continue
			}
			if field.Kind() == reflect.Interface && field.IsNil() {
				continue
			}
			if field.Kind() == reflect.Pointer && field.IsNil() {
				continue
			}
			// The instrument fields are unexported, so reflect needs the
			// addressable value to hand back an interface.
			addressable := reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()
			collector, ok := addressable.Interface().(prometheus.Collector)
			if !ok {
				continue
			}
			if !registered[collector] {
				missing = append(missing, name)
			}
		}
	}
	walk("Metrics.", reflect.ValueOf(m).Elem())

	require.Empty(t, missing, "instruments built but never registered")
}

// TestDBPoolCollector pins the pool collector's names and the state split; the
// values come from a snapshot, so the test drives database/sql.DBStats directly.
func TestDBPoolCollector(t *testing.T) {
	t.Parallel()

	registry := prometheus.NewRegistry()
	registry.MustRegister(NewDBPoolCollector(func() sql.DBStats {
		return sql.DBStats{
			MaxOpenConnections: 50,
			InUse:              3,
			Idle:               2,
			WaitCount:          7,
			WaitDuration:       time.Second,
		}
	}))
	families, err := registry.Gather()
	require.NoError(t, err)

	require.Equal(t, 50.0, sampleValue(t, families, "metaxisdata_db_pool_max_open_connections", nil))
	require.Equal(t, 3.0, sampleValue(t, families, "metaxisdata_db_pool_connections", map[string]string{"state": "in_use"}))
	require.Equal(t, 2.0, sampleValue(t, families, "metaxisdata_db_pool_connections", map[string]string{"state": "idle"}))
	require.Equal(t, 7.0, sampleValue(t, families, "metaxisdata_db_pool_wait_total", nil))
	require.Equal(t, 1.0, sampleValue(t, families, "metaxisdata_db_pool_wait_duration_seconds_total", nil))
}

// TestBuildInfoCollector pins the deployment-identity series.
func TestBuildInfoCollector(t *testing.T) {
	t.Parallel()

	registry := prometheus.NewRegistry()
	registry.MustRegister(NewBuildInfoCollector("v1.2.3", "abc123"))
	families, err := registry.Gather()
	require.NoError(t, err)

	require.Equal(t, 1.0, sampleValue(t, families, "metaxisdata_build_info", map[string]string{
		"version": "v1.2.3", "git_commit": "abc123",
	}))
}
