package qualityreport

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// reportFixture is a complete report: rules with a verdict, an open issue, a
// few failed checks with samples and a three-day trend.
func reportFixture() *Report {
	observed := 12.5
	warn := 5.0
	fail := 10.0
	qualityScore := 85.0
	nextRun := time.Date(2026, time.October, 31, 12, 0, 0, 0, time.UTC)
	return &Report{
		Target: Target{
			GUID:     "instance-1;shop;;orders",
			TypeKey:  "TABLE",
			Path:     "instance-1/shop/orders",
			Instance: "instance-1",
			Engine:   "MYSQL",
		},
		WindowDays:  3,
		WindowStart: time.Date(2026, time.October, 29, 0, 0, 0, 0, time.UTC),
		GeneratedAt: time.Date(2026, time.October, 31, 9, 30, 0, 0, time.UTC),
		GeneratedBy: "admin@example.com",
		Counts: Counts{
			Total: 10, Pass: 6, Warn: 1, Fail: 2, Error: 1,
		},
		RuleCount:         2,
		EnabledRuleCount:  1,
		QualityScore:      &qualityScore,
		ScoredRuleCount:   1,
		UnscoredRuleCount: 0,
		WorstStatusKey:    StatusError,
		Rules: []Rule{
			{
				Title:           "orders row count",
				TypeKey:         "ROW_COUNT",
				DimensionKey:    "COMPLETENESS",
				SeverityKey:     SeverityWarning,
				StateKey:        "ENABLED",
				Owner:           "users/1",
				LastStatusKey:   StatusPass,
				ObservedValue:   &observed,
				ObservedKindKey: ObservedKindCount,
				Thresholds: &storepb.QualityThresholds{
					Comparison: storepb.QualityComparison_QUALITY_COMPARISON_GREATER_THAN,
					WarnValue:  &warn,
					FailValue:  &fail,
				},
				LastRunTime: time.Date(2026, time.October, 31, 8, 0, 0, 0, time.UTC),
				Revision:    3,
			},
			{
				Title:        "orders email not null",
				TypeKey:      "NULL_PERCENT",
				DimensionKey: "COMPLETENESS",
				SeverityKey:  SeverityError,
				StateKey:     "PAUSED",
				Column:       "email",
				Revision:     1,
			},
		},
		OpenIssues: []Issue{
			{
				RuleTitle:   "orders email not null",
				SeverityKey: SeverityError,
				StatusKey:   "OPEN",
				FirstSeen:   time.Date(2026, time.October, 30, 7, 0, 0, 0, time.UTC),
				LastSeen:    time.Date(2026, time.October, 31, 7, 0, 0, 0, time.UTC),
				Occurrences: 4,
			},
		},
		Trend: []TrendDay{
			{Date: "2026-10-29", Counts: Counts{Total: 4, Pass: 3, Fail: 1}},
			{Date: "2026-10-30", Counts: Counts{Total: 0}},
			{Date: "2026-10-31", Counts: Counts{Total: 6, Pass: 3, Warn: 1, Fail: 1, Error: 1}},
		},
		FailedChecks: []FailedCheck{
			{
				RuleTitle:  "orders email not null",
				Column:     "email",
				FinishedAt: time.Date(2026, time.October, 31, 7, 0, 0, 0, time.UTC),
				FailedRows: 2,
				TotalRows:  10,
				Samples: []FailureSample{
					{Cells: []SampleCell{{Column: "id", Value: "2"}, {Column: "email", Value: "<script>alert(\"x\")</script>"}}},
					{Cells: []SampleCell{{Column: "id", Value: "3"}, {Column: "email", Value: "a & b"}}},
				},
			},
			{
				RuleTitle:  "orders amount range",
				Column:     "amount",
				FinishedAt: time.Date(2026, time.October, 31, 6, 0, 0, 0, time.UTC),
				FailedRows: 1,
				TotalRows:  10,
				Sampled:    true,
			},
		},
		FailedCheckTotal: 7,
		Basis: Basis{
			ScheduleInterval:  2 * time.Hour,
			NextRunTime:       &nextRun,
			HasRevisionRange:  true,
			RuleRevisionMin:   1,
			RuleRevisionMax:   3,
			EngineVersions:    []string{"dq.v1"},
			TargetColumnCount: 4,
		},
	}
}

// A complete report renders the documented sections in the requested language.
func TestRenderEnglishReport(t *testing.T) {
	t.Parallel()

	document, err := Render(LanguageEnglish, reportFixture())
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(document, "<!doctype html>"))
	require.Contains(t, document, `<html lang="en">`)
	for _, heading := range []string{
		"Report scope", "Result summary", "Daily trend", "Rules", "Open issues", "Failed samples", "Monitoring basis",
	} {
		require.Contains(t, document, heading)
	}
	require.Contains(t, document, "instance-1/shop/orders")
	require.Contains(t, document, "orders row count")
	require.Contains(t, document, "admin@example.com")
	require.Contains(t, document, "60.0%", "the pass rate is the pass share of the window's checks")
	require.Contains(t, document, "Rule pass rate")
	require.Contains(t, document, "100.0%", "only the rule with a known result counts toward the rule pass rate")
	require.Contains(t, document, "Quality score")
	require.Contains(t, document, "85.0", "the quality score is the weighted target score")
	require.Contains(t, document, "Judged checks")
	require.Contains(t, document, "<svg")
	require.Contains(t, document, "@media print")
	require.Contains(t, document, `class="seg-fail"`)
	require.Contains(t, document, "Showing 2 of 7 failed checks in the window.")
	require.Contains(t, document, "dq.v1")
	require.Contains(t, document, "forwarding the file forwards those rows",
		"the footer must warn that the report carries real sample values")
	require.Contains(t, document, `</html>`)
}

// A target whose checks were never judged has no score: the card carries the
// not-available mark rather than a misleading zero or one hundred.
func TestRenderQualityScoreUnset(t *testing.T) {
	t.Parallel()

	report := reportFixture()
	report.QualityScore = nil
	report.ScoredRuleCount = 0
	report.UnscoredRuleCount = 1
	view := newReportView(LanguageEnglish, report, newRenderLimits(report))
	require.Equal(t, englishLabels.LabelNotAvailable, view.Summary.QualityScore)
	require.Equal(t, "0", view.Summary.ScoredRuleCount)
	require.Equal(t, "1", view.Summary.UnscoredRuleCount)
}

// The Chinese report keeps the same structure and switches the vocabulary.
func TestRenderChineseReport(t *testing.T) {
	t.Parallel()

	document, err := Render(LanguageChinese, reportFixture())
	require.NoError(t, err)
	require.Contains(t, document, `<html lang="zh-CN">`)
	for _, heading := range []string{"报告范围", "结果汇总", "每日趋势", "规则", "未解决问题", "失败样本", "监控依据"} {
		require.Contains(t, document, heading)
	}
	require.Contains(t, document, "通过率")
	require.Contains(t, document, "质量分")
	require.Contains(t, document, "已判定检查")
	require.Contains(t, document, "行数")
	require.Contains(t, document, "转发本文件即转发这些数据",
		"页脚必须提示报告含真实样本值")
	require.NotContains(t, document, "Report scope")
}

// A sample value is data, never markup: html/template escapes it, so a stored
// `<script>` cannot become a script in the exported file.
func TestRenderEscapesSampleValues(t *testing.T) {
	t.Parallel()

	document, err := Render(LanguageEnglish, reportFixture())
	require.NoError(t, err)
	require.NotContains(t, document, "<script>alert")
	require.NotContains(t, document, "javascript:")
	require.Contains(t, document, "&lt;script&gt;alert")
	require.Contains(t, document, "a &amp; b")

	// A rule title is data too: an injected tag is escaped in the table as well.
	report := reportFixture()
	report.Rules[0].Title = "<img src=x onerror=alert(1)>"
	injected, err := Render(LanguageEnglish, report)
	require.NoError(t, err)
	require.NotContains(t, injected, "<img")
	require.Contains(t, injected, "&lt;img")
}

// A report with no failed check still renders every section and says the window
// held no failure, instead of dropping the section silently.
func TestRenderWithoutSamples(t *testing.T) {
	t.Parallel()

	report := reportFixture()
	report.FailedChecks = nil
	report.FailedCheckTotal = 0
	report.OpenIssues = nil
	document, err := Render(LanguageEnglish, report)
	require.NoError(t, err)
	require.Contains(t, document, "No check failed in the window.")
	require.Contains(t, document, "No open issues.")
}

// The report never loads anything: no remote script, stylesheet, font, image or
// CSS import may appear in the document.
func TestRenderHasNoExternalReferences(t *testing.T) {
	t.Parallel()

	document, err := Render(LanguageEnglish, reportFixture())
	require.NoError(t, err)
	for _, marker := range []string{
		"http://", "https://", "<script", "<link", "<iframe", "@import", "@font-face",
		"url(", "src=", "xlink:href", ".woff", ".ttf", "fonts.",
	} {
		require.NotContains(t, document, marker)
	}
}

// The document is a pure function of the report: rendering the same report twice
// produces byte-identical HTML, which is what makes a golden test meaningful.
func TestRenderIsDeterministic(t *testing.T) {
	t.Parallel()

	first, err := Render(LanguageEnglish, reportFixture())
	require.NoError(t, err)
	second, err := Render(LanguageEnglish, reportFixture())
	require.NoError(t, err)
	require.Equal(t, first, second)
}

// The documented caps are hard: a report with more failed checks and sample rows
// than allowed lists at most 20 checks with at most 5 rows each, and it says what
// it omitted.
func TestRenderCapsFailedChecksAndSampleRows(t *testing.T) {
	t.Parallel()

	report := reportFixture()
	report.FailedChecks = nil
	report.FailedCheckTotal = 25
	for index := 0; index < 25; index++ {
		samples := make([]FailureSample, 0, 8)
		for row := 0; row < 8; row++ {
			samples = append(samples, FailureSample{Cells: []SampleCell{
				{Column: "id", Value: "1"},
				{Column: "check", Value: "failed check " + strings.Repeat("x", row+1)},
			}})
		}
		report.FailedChecks = append(report.FailedChecks, FailedCheck{
			RuleTitle:  "check " + strconv.Itoa(index),
			FailedRows: 8,
			TotalRows:  10,
			Samples:    samples,
		})
	}
	document, err := Render(LanguageEnglish, report)
	require.NoError(t, err)
	require.Contains(t, document, "Showing 20 of 25 failed checks in the window.")
	require.Contains(t, document, "Only the first 5 sample rows are shown.")
	require.Contains(t, document, "check 19")
	require.NotContains(t, document, "check 20")
}

// A report larger than 2 MiB is shortened until it fits and carries the explicit
// truncation note; the document is still one complete HTML file.
func TestRenderOversizedReportIsTrimmed(t *testing.T) {
	t.Parallel()

	report := reportFixture()
	report.FailedChecks = nil
	report.FailedCheckTotal = 20
	wide := make([]SampleCell, 0, 120)
	for index := 0; index < 120; index++ {
		column := "very_long_column_name_" + strconv.Itoa(index)
		wide = append(wide, SampleCell{Column: column, Value: strings.Repeat("value", 80)})
	}
	for check := 0; check < 20; check++ {
		samples := make([]FailureSample, 0, 5)
		for row := 0; row < 5; row++ {
			samples = append(samples, FailureSample{Cells: wide})
		}
		report.FailedChecks = append(report.FailedChecks, FailedCheck{
			RuleTitle:  "wide check",
			FailedRows: 5,
			TotalRows:  10,
			Samples:    samples,
		})
	}
	document, err := Render(LanguageEnglish, report)
	require.NoError(t, err)
	require.LessOrEqual(t, len(document), MaxDocumentBytes)
	require.Contains(t, document, "The report exceeded the 2 MiB size cap and was shortened")
	require.Contains(t, document, "</html>")
}

// When the size cap drops whole rules, issues or checks, the section must say
// they were omitted, not that the target has none of them.
func TestRenderTrimmedSectionsDoNotClaimEmpty(t *testing.T) {
	t.Parallel()

	report := reportFixture()
	document, err := renderDocument(LanguageEnglish, report, &renderLimits{
		checks: 0, rows: MaxSampleRows, cellRunes: maxSampleCellRunes,
		titleRunes: maxTitleRunes, rules: 0, issues: 0, trimmed: true,
	})
	require.NoError(t, err)
	require.Contains(t, document, englishLabels.RulesTrimmed)
	require.Contains(t, document, englishLabels.IssuesTrimmed)
	require.Contains(t, document, englishLabels.SamplesTrimmed)
	require.NotContains(t, document, englishLabels.RulesEmpty)
	require.NotContains(t, document, englishLabels.IssuesEmpty)
	require.NotContains(t, document, englishLabels.SamplesEmpty)
	require.Contains(t, document, englishLabels.TrimNote, "the document still explains why")
}

// The omitted count and the list come from different observations, so the
// rendered denominator must be at least the number of rows shown.
func TestRenderShowsTheLargerFailedCheckTotal(t *testing.T) {
	t.Parallel()

	report := reportFixture()
	report.FailedChecks = nil
	for index := 0; index < MaxFailedChecks; index++ {
		report.FailedChecks = append(report.FailedChecks, FailedCheck{RuleTitle: "failed check"})
	}
	report.FailedCheckTotal = MaxFailedChecks + 1
	document, err := Render(LanguageEnglish, report)
	require.NoError(t, err)
	require.Contains(t, document, "Showing 20 of 21 failed checks in the window.")
}

// The trimming loop is bounded: every step halves a budget and the budgets hit
// their floors in far fewer steps than the loop cap, so a pathological report
// fails fast instead of re-rendering forever.
func TestRenderLimitsShrinkConverges(t *testing.T) {
	t.Parallel()

	limits := newRenderLimits(reportFixture())
	steps := 0
	for limits.shrink() {
		steps++
	}
	require.Less(t, steps, maxTrimIterations)
	require.True(t, limits.trimmed)
	require.Zero(t, limits.checks)
	require.Zero(t, limits.rules)
	require.Zero(t, limits.issues)
	require.Equal(t, 1, limits.rows)
	require.Equal(t, 8, limits.cellRunes)
	require.Equal(t, 25, limits.titleRunes)
}

// An orphaned target still renders from the snapshots, and the document states
// why the live target could not be read.
func TestRenderOrphanedTarget(t *testing.T) {
	t.Parallel()

	report := reportFixture()
	report.Orphaned = true
	report.OrphanedReason = "the target table was dropped"
	document, err := Render(LanguageEnglish, report)
	require.NoError(t, err)
	require.Contains(t, document, "This target no longer resolves")
	require.Contains(t, document, "the target table was dropped")
	require.Contains(t, document, "instance-1/shop/orders", "the path snapshot still renders")
}

// Render refuses a nil report instead of emitting a blank document.
func TestRenderRejectsNilReport(t *testing.T) {
	t.Parallel()

	_, err := Render(LanguageEnglish, nil)
	require.Error(t, err)
}

// Language normalization accepts the supported tags and their variants.
func TestNormalizeLanguage(t *testing.T) {
	t.Parallel()

	require.Equal(t, LanguageChinese, NormalizeLanguage("zh-CN"))
	require.Equal(t, LanguageChinese, NormalizeLanguage("zh"))
	require.Equal(t, LanguageChinese, NormalizeLanguage("ZH-TW"))
	require.Equal(t, LanguageChinese, NormalizeLanguage("  zh-hans  "))
	require.Equal(t, LanguageEnglish, NormalizeLanguage("en-US"))
	require.Equal(t, LanguageEnglish, NormalizeLanguage("en-GB"))
	require.Equal(t, LanguageEnglish, NormalizeLanguage(""))
	require.Equal(t, LanguageEnglish, NormalizeLanguage("fr-FR"))
}

// Threshold and interval text is bilingual and skips an unset level.
func TestFormatThresholdsAndInterval(t *testing.T) {
	t.Parallel()

	warn := 5.0
	fail := 10.0
	thresholds := &storepb.QualityThresholds{
		Comparison: storepb.QualityComparison_QUALITY_COMPARISON_GREATER_THAN,
		WarnValue:  &warn,
		FailValue:  &fail,
	}
	require.Equal(t, "warn > 5 · fail > 10", formatThresholds(LanguageEnglish, thresholds))
	require.Equal(t, "警告 > 5 · 失败 > 10", formatThresholds(LanguageChinese, thresholds))
	require.Empty(t, formatThresholds(LanguageEnglish, nil))
	require.Equal(t, "2h", formatInterval(LanguageEnglish, 2*time.Hour))
	require.Equal(t, "2小时", formatInterval(LanguageChinese, 2*time.Hour))
	require.Equal(t, "15m", formatInterval(LanguageEnglish, 15*time.Minute))
}
