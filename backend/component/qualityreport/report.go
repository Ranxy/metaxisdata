package qualityreport

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// The six quality verdicts, as the enum keys the renderer's catalog is keyed by.
const (
	StatusPass         = "PASS"
	StatusWarn         = "WARN"
	StatusFail         = "FAIL"
	StatusError        = "ERROR"
	StatusSkipped      = "SKIPPED"
	StatusNotEvaluated = "NOT_EVALUATED"
)

// The issue and rule severities, as catalog keys.
const (
	SeverityWarning  = "WARNING"
	SeverityError    = "ERROR"
	SeverityCritical = "CRITICAL"
)

// The observed-value units, as catalog keys.
const (
	ObservedKindCount     = "COUNT"
	ObservedKindRatio     = "RATIO"
	ObservedKindSeconds   = "SECONDS"
	ObservedKindTimestamp = "TIMESTAMP"
	ObservedKindNumber    = "NUMBER"
)

const (
	// MaxDocumentBytes caps the rendered document. A report is meant to be
	// forwarded as a file, so it must not grow without a bound even when a table
	// has very wide failed rows.
	MaxDocumentBytes = 2 << 20
	// MaxFailedChecks is how many failed checks a report lists at most. The
	// handler reads at most this many executions, so the model never carries
	// sample evidence the renderer cannot show.
	MaxFailedChecks = 20
	// MaxSampleRows is how many sampled rows one failed check lists at most. The
	// handler keeps one extra row in the model so the document can say the list
	// was cut.
	MaxSampleRows = 5
	// maxTrimIterations bounds the size-trimming loop. Every retry halves at
	// least one budget, and the documented caps converge well inside this bound;
	// the limit exists so a pathological input fails instead of spinning.
	maxTrimIterations = 40
	// maxSampleCellRunes is the starting per-cell text budget. The store already
	// truncates a sample value; this keeps a wide table from filling the document
	// with escaped data.
	maxSampleCellRunes = 256
	// maxTitleRunes is the starting title budget. Rule titles have no stored length
	// bound, so the report always shortens a pathological one.
	maxTitleRunes = 200
	// maxOrphanReasonRunes bounds the resolution error the orphan banner carries.
	maxOrphanReasonRunes = 400
	// maxErrorMessageRunes bounds the stored execution error text.
	maxErrorMessageRunes = 400

	chartWidth  = 720
	chartHeight = 200
	chartLeft   = 44
	chartRight  = 12
	chartTop    = 14
	chartBottom = 32
)

// The embedded template is parsed once. Any edit to it is a compile-time
// failure, which keeps a broken report from shipping as a runtime error.
//
//go:embed report.html.tmpl
var reportTemplateSource string

// reportTemplate is parsed once, at startup. A template that does not parse is a
// build-level defect, so Must is the honest failure here.
var reportTemplate = template.Must(template.New("quality-report").Parse(reportTemplateSource))

// Report is the data the renderer turns into one self-contained HTML document.
// It carries display-neutral values: statuses, severities and rule types are
// enum keys such as "FAIL" or "ROW_COUNT", never sentences, so the renderer
// alone decides the language.
type Report struct {
	// Target identifies the reported object.
	Target Target
	// WindowDays and WindowStart describe the execution window the counts and
	// the trend cover.
	WindowDays  int
	WindowStart time.Time
	// GeneratedAt and GeneratedBy attribute the document to its caller.
	GeneratedAt time.Time
	GeneratedBy string

	// Counts is the six-state tally of the completed checks in the window.
	Counts Counts
	// RuleCount and EnabledRuleCount describe the target's current rules.
	RuleCount        int32
	EnabledRuleCount int32
	// WorstStatusKey is the worst latest result across the rules; empty when no
	// rule has run.
	WorstStatusKey string

	Rules      []Rule
	OpenIssues []Issue

	// Trend is one point per day, oldest first, with quiet days as zero points.
	Trend []TrendDay

	// FailedChecks are the recent failed checks, newest first. FailedCheckTotal is
	// how many failed checks the window holds, so a capped list can say what it
	// omitted.
	FailedChecks     []FailedCheck
	FailedCheckTotal int32

	// Orphaned reports that the target no longer resolves; the document is then
	// rendered from the snapshots saved with the rules.
	Orphaned       bool
	OrphanedReason string

	Basis Basis
}

// Target identifies the reported object.
type Target struct {
	GUID string
	// TypeKey is a metadata object type enum key, such as "TABLE".
	TypeKey string
	// Path is the display path snapshot: instance/database/schema/object.
	Path string
	// Instance is the instance resource id, and Engine its engine name.
	Instance string
	Engine   string
}

// Counts is a six-state tally.
type Counts struct {
	Total        int32
	Pass         int32
	Warn         int32
	Fail         int32
	Error        int32
	Skipped      int32
	NotEvaluated int32
}

// TrendDay is one day of the trend.
type TrendDay struct {
	// Date is the UTC day, formatted YYYY-MM-DD.
	Date   string
	Counts Counts
}

// Rule is one rule in the report's rule list.
type Rule struct {
	Title        string
	TypeKey      string
	DimensionKey string
	SeverityKey  string
	StateKey     string
	Column       string
	Owner        string
	// LastStatusKey is empty when the rule never completed a check.
	LastStatusKey   string
	ObservedValue   *float64
	ObservedKindKey string
	Thresholds      *storepb.QualityThresholds
	LastRunTime     time.Time
	Revision        int32
}

// Issue is one unresolved problem.
type Issue struct {
	RuleTitle   string
	SeverityKey string
	StatusKey   string
	FirstSeen   time.Time
	LastSeen    time.Time
	Occurrences int32
}

// FailedCheck is one failed execution with its sampled rows.
type FailedCheck struct {
	RuleTitle  string
	Column     string
	FinishedAt time.Time
	FailedRows int64
	TotalRows  int64
	Sampled    bool
	// SampleTruncated is the execution's own truncation flag: more sampled rows
	// existed when the check was recorded than were stored.
	SampleTruncated bool
	ErrorCode       string
	ErrorMessage    string
	Samples         []FailureSample
}

// FailureSample is one sampled failed row.
type FailureSample struct {
	Cells []SampleCell
}

// SampleCell is one column of a sampled failed row.
type SampleCell struct {
	Column string
	Value  string
	IsNull bool
}

// Basis is the monitoring basis the report records: what the target state, the
// schedule and the rule revisions were when the document was produced.
type Basis struct {
	ScheduleInterval time.Duration
	NextRunTime      *time.Time
	// HasRevisionRange is false when the target has no rule at all.
	HasRevisionRange  bool
	RuleRevisionMin   int32
	RuleRevisionMax   int32
	EngineVersions    []string
	TargetColumnCount int32
}

// Render turns a report into the complete HTML document. A report the caller
// built within the caps renders as it is; one that is still larger than
// MaxDocumentBytes is shortened (long texts truncated, sample rows and checks
// dropped) until it fits, and the document says so.
func Render(language string, report *Report) (string, error) {
	if report == nil {
		return "", errors.New("a quality report is required")
	}
	limits := newRenderLimits(report)
	for iteration := 0; ; iteration++ {
		document, err := renderDocument(language, report, limits)
		if err != nil {
			return "", err
		}
		if len(document) <= MaxDocumentBytes {
			return document, nil
		}
		if iteration+1 >= maxTrimIterations {
			return "", errors.Errorf("the quality report exceeds %d bytes after %d trimming rounds", MaxDocumentBytes, maxTrimIterations)
		}
		if !limits.shrink() {
			return "", errors.Errorf("the quality report exceeds %d bytes even after trimming every section", MaxDocumentBytes)
		}
	}
}

// renderLimits is the size-trimming state of one Render call. The first attempt
// applies the hard caps (20 checks, 5 rows, 256-byte cells); every retry reduces
// one budget so the document converges on MaxDocumentBytes.
type renderLimits struct {
	checks     int
	rows       int
	cellRunes  int
	titleRunes int
	rules      int
	issues     int
	trimmed    bool
}

// newRenderLimits starts one render at the documented caps.
func newRenderLimits(report *Report) *renderLimits {
	return &renderLimits{
		checks:     min(len(report.FailedChecks), MaxFailedChecks),
		rows:       MaxSampleRows,
		cellRunes:  maxSampleCellRunes,
		titleRunes: maxTitleRunes,
		rules:      len(report.Rules),
		issues:     len(report.OpenIssues),
	}
}

// shrink reduces one budget by half and reports whether anything was left to
// reduce. Values go first because a wide row dominates the document; whole
// sections go last because dropping a rule from the list changes what the report
// says. Halving keeps the loop short even for a pathological report: the caps
// above converge in far fewer than maxTrimIterations full renders.
func (l *renderLimits) shrink() bool {
	l.trimmed = true
	switch {
	case l.cellRunes > 8:
		l.cellRunes = max(8, l.cellRunes/2)
	case l.rows > 1:
		l.rows = max(1, l.rows/2)
	case l.checks > 0:
		l.checks /= 2
	case l.titleRunes > 25:
		l.titleRunes = max(25, l.titleRunes/2)
	case l.rules > 0:
		l.rules /= 2
	case l.issues > 0:
		l.issues /= 2
	default:
		return false
	}
	return true
}

// renderDocument builds the view and executes the template once.
func renderDocument(language string, report *Report, limits *renderLimits) (string, error) {
	view := newReportView(language, report, limits)
	var buffer bytes.Buffer
	if err := reportTemplate.Execute(&buffer, view); err != nil {
		return "", errors.Wrap(err, "failed to render the quality report template")
	}
	return buffer.String(), nil
}

// reportView is the template's view model: every field is already display text.
type reportView struct {
	HTMLTitle   string
	ReportTitle string
	Labels      labels

	Orphaned     bool
	OrphanReason string
	Trimmed      bool

	Scope   scopeView
	Summary summaryView
	Trend   trendView
	Rules   []ruleView
	// RulesEmpty is true when no rule is shown. RulesTrimmed distinguishes "the
	// size cap dropped them" from "the target has none", so the section never
	// claims there are no rules when some exist.
	RulesEmpty    bool
	RulesTrimmed  bool
	Issues        []issueView
	IssuesEmpty   bool
	IssuesTrimmed bool
	Samples       samplesView
	Basis         basisView
}

type scopeView struct {
	Path        string
	GUID        string
	Type        string
	Instance    string
	Engine      string
	Window      string
	GeneratedAt string
	GeneratedBy string
}

type summaryView struct {
	TotalChecks      string
	PassRate         string
	RulePassRate     string
	RuleCount        string
	EnabledRuleCount string
	OpenIssueCount   string
	WorstResult      string
	WorstClass       string
	Counts           []countView
	SeverityCounts   []countView
}

type countView struct {
	Label string
	Count string
	Class string
}

type trendView struct {
	Empty   bool
	Caption string
	Width   int
	Height  int
	AxisX1  int
	AxisX2  int
	AxisY   int
	MaxText string
	// MaxTextY is the y of the scale's top value label, above the chart area.
	MaxTextY int
	Grid     []gridLineView
	Bars     []barView
	XLabels  []xLabelView
	Legend   []legendView
}

type gridLineView struct {
	X1 int
	X2 int
	Y  int
}

type barView struct {
	Title    string
	Segments []segmentView
}

type segmentView struct {
	X      int
	Y      int
	Width  int
	Height int
	Class  string
}

type xLabelView struct {
	X    int
	Y    int
	Text string
}

type legendView struct {
	Label string
	Class string
}

type ruleView struct {
	Title           string
	Type            string
	Column          string
	Dimension       string
	Severity        string
	SeverityClass   string
	State           string
	StateClass      string
	Owner           string
	LastResult      string
	LastResultClass string
	Observed        string
	Thresholds      string
	LastRun         string
	Revision        string
}

type issueView struct {
	Rule          string
	Severity      string
	SeverityClass string
	Status        string
	StatusClass   string
	FirstSeen     string
	LastSeen      string
	Occurrences   string
}

type samplesView struct {
	Empty bool
	// Trimmed reports that the size cap dropped failed checks, so an empty
	// section must not read as "nothing failed".
	Trimmed bool
	Caption string
	Showing string
	Checks  []checkView
}

type checkView struct {
	Rule         string
	Column       string
	Time         string
	Rows         string
	Sampled      string
	ErrorCode    string
	ErrorMessage string
	Note         string
	HasTable     bool
	Table        sampleTableView
	NoSamples    bool
}

type sampleTableView struct {
	Columns []string
	Rows    [][]sampleCellView
}

type sampleCellView struct {
	Value  string
	IsNull bool
}

type basisView struct {
	Path     string
	Schedule string
	NextRun  string
	Revision string
	Engines  string
	Columns  string
}

// newReportView turns one report into the template view under the given limits.
func newReportView(language string, report *Report, limits *renderLimits) *reportView {
	lang := NormalizeLanguage(language)
	text := catalogFor(lang)
	view := &reportView{
		HTMLTitle:    text.HTMLTitle,
		ReportTitle:  text.ReportTitle,
		Labels:       text,
		Orphaned:     report.Orphaned,
		OrphanReason: truncateRunes(report.OrphanedReason, maxOrphanReasonRunes),
		Trimmed:      limits.trimmed,
	}
	view.Scope = scopeView{
		Path:        valueOr(report.Target.Path, text.LabelNotAvailable),
		GUID:        valueOr(report.Target.GUID, text.LabelNotAvailable),
		Type:        valueOr(objectTypeLabel(lang, report.Target.TypeKey), text.LabelNotAvailable),
		Instance:    valueOr(report.Target.Instance, text.LabelNotAvailable),
		Engine:      valueOr(report.Target.Engine, text.LabelNotAvailable),
		Window:      formatWindow(lang, report.WindowDays, report.WindowStart),
		GeneratedAt: formatTime(report.GeneratedAt, text.LabelNotAvailable),
		GeneratedBy: valueOr(report.GeneratedBy, text.LabelNotAvailable),
	}
	view.Summary = newSummaryView(lang, text, report)
	view.Trend = newTrendView(lang, text, report.Trend)
	view.Rules, view.RulesEmpty, view.RulesTrimmed = newRuleViews(lang, text, report.Rules, limits)
	view.Issues, view.IssuesEmpty, view.IssuesTrimmed = newIssueViews(lang, text, report.OpenIssues, limits)
	view.Samples = newSamplesView(text, report, limits)
	view.Basis = basisView{
		Path:     valueOr(report.Target.Path, text.LabelNotAvailable),
		Schedule: scheduleText(lang, text, report.Basis),
		NextRun:  formatTimePtr(report.Basis.NextRunTime, text.LabelNotAvailable),
		Revision: revisionText(text, report.Basis),
		Engines:  valueOr(strings.Join(report.Basis.EngineVersions, ", "), text.LabelNotAvailable),
		Columns:  columnCountText(text, report.Basis.TargetColumnCount),
	}
	return view
}

// newSummaryView builds the result summary: the six-state tally, the pass rate
// and the rule/issue headlines.
func newSummaryView(language string, text labels, report *Report) summaryView {
	view := summaryView{
		TotalChecks:      strconv.FormatInt(int64(report.Counts.Total), 10),
		PassRate:         passRateText(text, report.Counts),
		RulePassRate:     rulePassRateText(text, report.Rules),
		RuleCount:        strconv.Itoa(int(report.RuleCount)),
		EnabledRuleCount: strconv.Itoa(int(report.EnabledRuleCount)),
		OpenIssueCount:   strconv.Itoa(len(report.OpenIssues)),
		WorstResult:      text.LabelNeverRun,
	}
	if report.WorstStatusKey != "" {
		view.WorstResult = statusLabel(language, report.WorstStatusKey)
		view.WorstClass = "status-" + cssKey(report.WorstStatusKey)
	}
	for _, entry := range []struct {
		key   string
		count int32
	}{
		{StatusPass, report.Counts.Pass},
		{StatusWarn, report.Counts.Warn},
		{StatusFail, report.Counts.Fail},
		{StatusError, report.Counts.Error},
		{StatusSkipped, report.Counts.Skipped},
		{StatusNotEvaluated, report.Counts.NotEvaluated},
	} {
		view.Counts = append(view.Counts, countView{
			Label: statusLabel(language, entry.key),
			Count: strconv.FormatInt(int64(entry.count), 10),
			Class: "status-" + cssKey(entry.key),
		})
	}
	severityCounts := map[string]int32{}
	for _, issue := range report.OpenIssues {
		severityCounts[issue.SeverityKey]++
	}
	for _, key := range []string{SeverityWarning, SeverityError, SeverityCritical} {
		if severityCounts[key] == 0 {
			continue
		}
		view.SeverityCounts = append(view.SeverityCounts, countView{
			Label: severityLabel(language, key),
			Count: strconv.FormatInt(int64(severityCounts[key]), 10),
			Class: "severity-" + cssKey(key),
		})
	}
	return view
}

// newTrendView precomputes the SVG bar chart: one stacked bar per day, with the
// six states in a fixed order so the same state always has the same color.
func newTrendView(language string, text labels, days []TrendDay) trendView {
	view := trendView{
		Caption: text.TrendCaption,
		Width:   chartWidth,
		Height:  chartHeight,
	}
	if len(days) == 0 {
		view.Empty = true
		return view
	}
	maxTotal := 1
	for _, day := range days {
		maxTotal = max(maxTotal, int(day.Counts.Total))
	}
	plotWidth := chartWidth - chartLeft - chartRight
	plotHeight := chartHeight - chartTop - chartBottom
	axisY := chartTop + plotHeight
	view.AxisX1, view.AxisX2, view.AxisY = chartLeft, chartWidth-chartRight, axisY
	view.MaxText = strconv.Itoa(maxTotal)
	view.MaxTextY = chartTop + 4
	for _, fraction := range []float64{0, 0.25, 0.5, 0.75, 1} {
		view.Grid = append(view.Grid, gridLineView{
			X1: chartLeft,
			X2: chartWidth - chartRight,
			Y:  axisY - int(math.Round(fraction*float64(plotHeight))),
		})
	}
	slot := float64(plotWidth) / float64(len(days))
	barWidth := max(2, min(int(math.Round(slot))-1, 28))
	if barWidth <= 0 {
		barWidth = 2
	}
	for index, day := range days {
		x := chartLeft + int(math.Round(float64(index)*slot+(slot-float64(barWidth))/2))
		bar := barView{Title: day.Date + ": " + strconv.Itoa(int(day.Counts.Total))}
		// The segments are stacked from the axis up. Tracking the cumulative
		// height as a float and deriving every edge from it keeps the stack inside
		// the plot area: rounding each height independently can overflow it.
		cumulative := 0.0
		bottom := axisY
		for _, entry := range []struct {
			key   string
			count int32
		}{
			{StatusPass, day.Counts.Pass},
			{StatusWarn, day.Counts.Warn},
			{StatusFail, day.Counts.Fail},
			{StatusError, day.Counts.Error},
			{StatusSkipped, day.Counts.Skipped},
			{StatusNotEvaluated, day.Counts.NotEvaluated},
		} {
			if entry.count <= 0 {
				continue
			}
			cumulative += float64(entry.count) / float64(maxTotal)
			top := axisY - int(math.Round(cumulative*float64(plotHeight)))
			top = max(chartTop, top)
			height := bottom - top
			if height <= 0 {
				height = 1
				top = bottom - 1
			}
			bar.Segments = append(bar.Segments, segmentView{
				X: x, Y: top, Width: barWidth, Height: height, Class: cssKey(entry.key),
			})
			bottom = top
		}
		view.Bars = append(view.Bars, bar)
	}
	step := max(1, len(days)/8)
	labeled := map[int]bool{}
	for index := 0; index < len(days); index += step {
		labeled[index] = true
	}
	labeled[len(days)-1] = true
	for index := range labeled {
		view.XLabels = append(view.XLabels, xLabelView{
			X:    chartLeft + int(math.Round(float64(index)*slot+slot/2)),
			Y:    axisY + 16,
			Text: shortDate(days[index].Date),
		})
	}
	// The index map above is unordered; sort the labels by position so the
	// document is deterministic.
	slices.SortFunc(view.XLabels, func(a, b xLabelView) int { return a.X - b.X })
	for _, key := range []string{StatusPass, StatusWarn, StatusFail, StatusError, StatusSkipped, StatusNotEvaluated} {
		view.Legend = append(view.Legend, legendView{
			Label: statusLabel(language, key),
			Class: "status-" + cssKey(key),
		})
	}
	return view
}

// newRuleViews builds the rule rows under the render limits. The second result
// says the list is empty, the third whether size trimming emptied or shortened
// it, so an omitted row is never reported as a missing rule.
func newRuleViews(language string, text labels, rules []Rule, limits *renderLimits) ([]ruleView, bool, bool) {
	shown := rules
	if limits.rules < len(shown) {
		shown = shown[:limits.rules]
	}
	views := make([]ruleView, 0, len(shown))
	for _, rule := range shown {
		view := ruleView{
			Title:      truncateRunes(rule.Title, limits.titleRunes),
			Type:       ruleTypeLabel(language, rule.TypeKey),
			Column:     valueOr(rule.Column, text.LabelNotAvailable),
			Dimension:  dimensionLabel(language, rule.DimensionKey),
			Severity:   severityLabel(language, rule.SeverityKey),
			State:      ruleStateLabel(language, rule.StateKey),
			Owner:      valueOr(rule.Owner, text.LabelUnassigned),
			LastResult: text.LabelNeverRun,
			Observed:   formatObserved(language, rule.ObservedValue, rule.ObservedKindKey),
			Thresholds: valueOr(formatThresholds(language, rule.Thresholds), text.LabelNotAvailable),
			LastRun:    formatTime(rule.LastRunTime, text.LabelNotAvailable),
			Revision:   strconv.Itoa(int(rule.Revision)),
		}
		view.SeverityClass = badgeClass("severity", rule.SeverityKey)
		view.StateClass = badgeClass("state", rule.StateKey)
		if rule.LastStatusKey != "" {
			view.LastResult = statusLabel(language, rule.LastStatusKey)
			view.LastResultClass = "status-" + cssKey(rule.LastStatusKey)
		}
		views = append(views, view)
	}
	return views, len(views) == 0, limits.rules < len(rules)
}

// newIssueViews builds the open-issue rows under the render limits.
func newIssueViews(language string, text labels, issues []Issue, limits *renderLimits) ([]issueView, bool, bool) {
	shown := issues
	if limits.issues < len(shown) {
		shown = shown[:limits.issues]
	}
	views := make([]issueView, 0, len(shown))
	for _, issue := range shown {
		views = append(views, issueView{
			Rule:          truncateRunes(issue.RuleTitle, limits.titleRunes),
			Severity:      severityLabel(language, issue.SeverityKey),
			SeverityClass: "severity-" + cssKey(issue.SeverityKey),
			Status:        issueStatusLabel(language, issue.StatusKey),
			StatusClass:   "state-" + cssKey(issue.StatusKey),
			FirstSeen:     formatTime(issue.FirstSeen, text.LabelNotAvailable),
			LastSeen:      formatTime(issue.LastSeen, text.LabelNotAvailable),
			Occurrences:   strconv.Itoa(int(issue.Occurrences)),
		})
	}
	return views, len(views) == 0, limits.issues < len(issues)
}

// newSamplesView builds the capped failed-sample section.
func newSamplesView(text labels, report *Report, limits *renderLimits) samplesView {
	view := samplesView{Caption: text.SampleCaption}
	total := report.FailedCheckTotal
	if total == 0 {
		total = int32(len(report.FailedChecks))
	}
	shown := report.FailedChecks
	if limits.checks < len(shown) {
		shown = shown[:limits.checks]
	}
	if len(shown) > 0 {
		view.Showing = fmt.Sprintf(text.SampleShown, len(shown), int(total))
	}
	for _, check := range shown {
		rowViews := checkView{
			Rule:         truncateRunes(check.RuleTitle, limits.titleRunes),
			Column:       valueOr(check.Column, text.LabelNotAvailable),
			Time:         formatTime(check.FinishedAt, text.LabelNotAvailable),
			Rows:         strconv.FormatInt(check.FailedRows, 10) + " / " + strconv.FormatInt(check.TotalRows, 10),
			Sampled:      yesNo(text, check.Sampled),
			ErrorCode:    check.ErrorCode,
			ErrorMessage: truncateRunes(check.ErrorMessage, maxErrorMessageRunes),
		}
		var notes []string
		rows := check.Samples
		if len(rows) > limits.rows {
			rows = rows[:limits.rows]
			notes = append(notes, fmt.Sprintf(text.CheckRowLimit, limits.rows))
		}
		if check.SampleTruncated {
			notes = append(notes, text.CheckRecorded)
		}
		rowViews.Note = strings.Join(notes, " ")
		rowViews.Table = newSampleTableView(rows, limits.cellRunes, text.LabelNullValue)
		rowViews.HasTable = len(rowViews.Table.Columns) > 0
		rowViews.NoSamples = !rowViews.HasTable
		view.Checks = append(view.Checks, rowViews)
	}
	view.Empty = len(view.Checks) == 0
	view.Trimmed = limits.checks < len(report.FailedChecks)
	return view
}

// newSampleTableView aligns the sampled rows on the union of their columns, so
// two rows of the same check always line up even when one omits a column. Long
// values are truncated and NULLs render as a labeled, grayed cell.
func newSampleTableView(samples []FailureSample, cellRunes int, nullLabel string) sampleTableView {
	table := sampleTableView{}
	index := map[string]int{}
	for _, sample := range samples {
		for _, cell := range sample.Cells {
			if _, ok := index[cell.Column]; ok {
				continue
			}
			index[cell.Column] = len(table.Columns)
			table.Columns = append(table.Columns, cell.Column)
		}
	}
	for _, sample := range samples {
		row := make([]sampleCellView, len(table.Columns))
		for _, cell := range sample.Cells {
			value := truncateRunes(cell.Value, cellRunes)
			if cell.IsNull {
				value = nullLabel
			}
			row[index[cell.Column]] = sampleCellView{Value: value, IsNull: cell.IsNull}
		}
		table.Rows = append(table.Rows, row)
	}
	return table
}

// basisText helpers.

func scheduleText(language string, text labels, basis Basis) string {
	if basis.ScheduleInterval <= 0 {
		return text.ScheduleNone
	}
	return fmt.Sprintf(text.ScheduleEvery, formatInterval(language, basis.ScheduleInterval))
}

func revisionText(text labels, basis Basis) string {
	if !basis.HasRevisionRange {
		return text.LabelNotAvailable
	}
	return fmt.Sprintf(text.RevisionRange, basis.RuleRevisionMin, basis.RuleRevisionMax)
}

func columnCountText(text labels, count int32) string {
	if count <= 0 {
		return text.LabelNotAvailable
	}
	return fmt.Sprintf(text.ColumnCount, count)
}

// rulePassRateText renders the pass share of the rules whose latest result is
// known: a rule that never ran is not a failing rule and does not dilute the
// rate.
func rulePassRateText(text labels, rules []Rule) string {
	total, passing := 0, 0
	for _, rule := range rules {
		if rule.LastStatusKey == "" {
			continue
		}
		total++
		if rule.LastStatusKey == StatusPass {
			passing++
		}
	}
	if total == 0 {
		return text.LabelNotAvailable
	}
	return strconv.FormatFloat(float64(passing)/float64(total)*100, 'f', 1, 64) + "%"
}

// passRateText renders the pass share of the completed checks in the window.
func passRateText(text labels, counts Counts) string {
	if counts.Total <= 0 {
		return text.LabelNotAvailable
	}
	return strconv.FormatFloat(float64(counts.Pass)/float64(counts.Total)*100, 'f', 1, 64) + "%"
}

// windowFormat renders the covered window in the language's sentence shape.
func formatWindow(language string, days int, start time.Time) string {
	start = start.UTC()
	end := start.AddDate(0, 0, max(days, 1)-1)
	if language == LanguageChinese {
		return fmt.Sprintf("最近 %d 天（%s 至 %s）", max(days, 1), start.Format("2006-01-02"), end.Format("2006-01-02"))
	}
	return fmt.Sprintf("Last %d days (%s – %s)", max(days, 1), start.Format("2006-01-02"), end.Format("2006-01-02"))
}

// formatTime renders a moment in UTC; the zero time is the "not available" mark
// the callers pass in.
func formatTime(value time.Time, notAvailable string) string {
	if value.IsZero() {
		return notAvailable
	}
	return value.UTC().Format("2006-01-02 15:04:05 UTC")
}

// formatTimePtr renders an optional moment.
func formatTimePtr(value *time.Time, notAvailable string) string {
	if value == nil {
		return notAvailable
	}
	return formatTime(*value, notAvailable)
}

// shortDate trims a YYYY-MM-DD date to MM-DD for the chart axis.
func shortDate(date string) string {
	if len(date) == len("2006-01-02") {
		return date[5:]
	}
	return date
}

// yesNo renders a boolean with the catalog's terms.
func yesNo(text labels, value bool) string {
	if value {
		return text.LabelYes
	}
	return text.LabelNo
}

// badgeClass is the CSS class of a severity or state badge; an unknown key has
// no badge at all.
func badgeClass(prefix, key string) string {
	if key == "" {
		return ""
	}
	return prefix + "-" + cssKey(key)
}

// valueOr returns value, or the fallback when it is empty.
func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// truncateRunes cuts s to limit runes and appends an ellipsis, so truncation
// never splits a UTF-8 sequence.
func truncateRunes(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "…"
}
