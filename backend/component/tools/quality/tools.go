package quality

// The read-only quality tool bodies: the trust signal, the target directory and
// one target's workspace, the issue ledger, executions and their evidence, the
// lineage impact, and consumption coverage. MCP runs these; the built-in agent's
// own quality tools are product-local (migration deferred), and the transport
// keeps its schema, permission and audit envelope.

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/component/tools/metadata"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

// defaultPageSize is what a listing returns when the caller does not say.
const defaultPageSize = 25

// maxPageSize caps one listing.
const maxPageSize = 200

// Reader is the slice of the data-quality service the tools use.
type Reader interface {
	ListQualityIssues(context.Context, *connect.Request[v1pb.ListQualityIssuesRequest]) (*connect.Response[v1pb.ListQualityIssuesResponse], error)
	GetQualityTarget(context.Context, *connect.Request[v1pb.GetQualityTargetRequest]) (*connect.Response[v1pb.GetQualityTargetResponse], error)
	ListQualityExecutions(context.Context, *connect.Request[v1pb.ListQualityExecutionsRequest]) (*connect.Response[v1pb.ListQualityExecutionsResponse], error)
	GetQualityExecution(context.Context, *connect.Request[v1pb.GetQualityExecutionRequest]) (*connect.Response[v1pb.QualityExecution], error)
	ListQualityTargets(context.Context, *connect.Request[v1pb.ListQualityTargetsRequest]) (*connect.Response[v1pb.ListQualityTargetsResponse], error)
	GetQualityTargetImpact(context.Context, *connect.Request[v1pb.GetQualityTargetImpactRequest]) (*connect.Response[v1pb.GetQualityTargetImpactResponse], error)
	GetQualityConsumptionCoverage(context.Context, *connect.Request[v1pb.GetQualityConsumptionCoverageRequest]) (*connect.Response[v1pb.GetQualityConsumptionCoverageResponse], error)
}

// Tools runs the quality read tools.
type Tools struct {
	quality  Reader
	resolver *metadata.Resolver
}

// NewTools builds the quality tool set over the quality service and the shared
// name resolver.
func NewTools(quality Reader, resolver *metadata.Resolver) *Tools {
	return &Tools{quality: quality, resolver: resolver}
}

// normalizePageSize applies the listing budget.
func normalizePageSize(requested int32) (int32, error) {
	switch {
	case requested < 0:
		return 0, metadata.InvalidArgument("page_size must not be negative")
	case requested == 0:
		return defaultPageSize, nil
	case requested > maxPageSize:
		return maxPageSize, nil
	default:
		return requested, nil
	}
}

// parseMetaType reads a meta type name. An empty value stays unspecified, which
// lets the server infer the type from a GUID.
func parseMetaType(value string) (v1pb.MetaType, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return v1pb.MetaType_UNSPECIFIED, nil
	}
	number, ok := v1pb.MetaType_value[strings.ToUpper(trimmed)]
	if !ok {
		return v1pb.MetaType_UNSPECIFIED, metadata.NewError(metadata.CodeInvalidArgument,
			fmt.Sprintf("unknown meta type %q", value),
			"valid types include TABLE, VIEW, MATERIALIZED_VIEW, COLUMN, SCHEMA, MANUAL_SQL")
	}
	return v1pb.MetaType(number), nil
}

// --- get_quality_signal ---

type GetQualitySignalArgs struct {
	Object   metadata.ObjectRef `json:"object"`
	MetaType string             `json:"meta_type"`
}

// qualitySignalResult is the answer for a monitored object: every trust signal
// the metadata browser shows, in one object.
type qualitySignalResult struct {
	GUID      string `json:"guid"`
	MetaType  string `json:"metaType"`
	Name      string `json:"name,omitempty"`
	Monitored bool   `json:"monitored"`
	// AllPaused says every check on the object is paused: the checks exist but
	// nothing is being verified. The counters still describe the target, while the
	// verdict fields are omitted — a paused check's last result is history, not a
	// current health claim.
	AllPaused            bool     `json:"allPaused"`
	WorstStatus          string   `json:"worstStatus,omitempty"`
	LastRunTime          string   `json:"lastRunTime,omitempty"`
	QualityScore         *float64 `json:"qualityScore,omitempty"`
	RecentPassRate       *float64 `json:"recentPassRate,omitempty"`
	RecentExecutionCount int32    `json:"recentExecutionCount"`
	CoveredColumnCount   int32    `json:"coveredColumnCount"`
	TotalColumnCount     int32    `json:"totalColumnCount"`
	OpenIssueCount       int32    `json:"openIssueCount"`
	EnabledRuleCount     int32    `json:"enabledRuleCount"`
	SchemaChanged        bool     `json:"schemaChanged"`
	Orphaned             bool     `json:"orphaned"`
}

// qualitySignalUnmonitoredResult is the answer for an object with no live rule.
// It is deliberately not an error: the object exists and simply has no checks, so
// its signal is empty rather than unknown.
type qualitySignalUnmonitoredResult struct {
	GUID      string `json:"guid"`
	MetaType  string `json:"metaType"`
	Name      string `json:"name,omitempty"`
	Monitored bool   `json:"monitored"`
}

// runGetQualitySignal reads one object's trust signal. The quality API takes an
// exact-target selection for this: a selection is unpaged, keeps the request
// order, and omits a target with no live rule, which is how "not monitored" is
// told apart from "no such object".
func (t *Tools) GetQualitySignal(ctx context.Context, raw json.RawMessage) (any, error) {
	var args GetQualitySignalArgs
	if err := metadata.DecodeArguments(raw, &args); err != nil {
		return nil, err
	}
	metaType, err := parseMetaType(args.MetaType)
	if err != nil {
		return nil, err
	}
	if metaType == v1pb.MetaType_UNSPECIFIED && args.Object.GUID != "" {
		return nil, metadata.NewError(metadata.CodeInvalidArgument, "meta_type is required when the reference is a bare guid",
			"list_metadata and search_metadata return metaType with every object, or address the object by name instead")
	}
	object, err := t.resolver.ResolveObject(ctx, args.Object, metaType)
	if err != nil {
		return nil, err
	}
	if !qualitySignalMetaTypes[object.MetaType] {
		// A rule can only watch a table or view. Any other object therefore has no
		// signal to read, and the quality RPC would refuse the key: "nothing watches
		// this" is the truthful answer.
		return qualitySignalNotMonitored(object), nil
	}
	response, err := t.quality.ListQualityTargets(ctx, connect.NewRequest(&v1pb.ListQualityTargetsRequest{
		// "workspaces/-" is the current workspace, the way every quality RPC names it.
		Parent: "workspaces/-",
		Targets: []*v1pb.QualityTargetKey{{
			TargetGuid: object.GUID,
			MetaType:   object.MetaType,
		}},
	}))
	if err != nil {
		return nil, metadata.FromRPC(err)
	}
	summary := qualityTargetSummaryOf(response.Msg.GetQualityTargets(), object.GUID, object.MetaType)
	if summary == nil {
		// A selection omits a key with no live rule. The object exists; nothing
		// watches it, which is an answer rather than a failure.
		return qualitySignalNotMonitored(object), nil
	}
	// The aggregate counts paused checks too, so an all-paused target still carries
	// its paused checks' last verdict. That is history, not a current health claim:
	// report the state and omit the verdict rather than let a model call a paused
	// table healthy.
	allPaused := summary.GetEnabledRuleCount() <= 0
	result := qualitySignalResult{
		GUID:                 object.GUID,
		MetaType:             object.MetaType.String(),
		Name:                 qualitySignalName(object.Name, summary),
		Monitored:            true,
		AllPaused:            allPaused,
		LastRunTime:          qualitySignalTime(summary.GetLastRunTime()),
		QualityScore:         summary.QualityScore,
		RecentPassRate:       summary.RecentPassRate,
		RecentExecutionCount: summary.GetRecentExecutionCount(),
		CoveredColumnCount:   summary.GetCoveredColumnCount(),
		TotalColumnCount:     summary.GetTotalColumnCount(),
		OpenIssueCount:       summary.GetOpenIssueCount(),
		EnabledRuleCount:     summary.GetEnabledRuleCount(),
		SchemaChanged:        summary.GetSchemaChanged(),
		Orphaned:             summary.GetOrphaned(),
	}
	if !allPaused {
		result.WorstStatus = qualitySignalStatus(summary.GetWorstStatus())
	}
	return result, nil
}

// qualitySignalMetaTypes are the object types a quality rule can watch; any other
// object has no signal to read.
var qualitySignalMetaTypes = map[v1pb.MetaType]bool{
	v1pb.MetaType_TABLE:             true,
	v1pb.MetaType_EXTERNAL_TABLE:    true,
	v1pb.MetaType_VIEW:              true,
	v1pb.MetaType_MATERIALIZED_VIEW: true,
}

// qualitySignalNotMonitored answers that nothing watches the object.
func qualitySignalNotMonitored(object metadata.Resolved) qualitySignalUnmonitoredResult {
	return qualitySignalUnmonitoredResult{
		GUID:      object.GUID,
		MetaType:  object.MetaType.String(),
		Name:      object.Name,
		Monitored: false,
	}
}

// qualityTargetSummaryOf finds the one summary a single-key selection answered
// with. A selection omits the keys it has no live rule for, so an empty answer is
// the only legitimate miss; anything else is not this object's signal and is
// treated as such rather than reported as a different table's health.
func qualityTargetSummaryOf(summaries []*v1pb.QualityTargetSummary, guid string, metaType v1pb.MetaType) *v1pb.QualityTargetSummary {
	for _, summary := range summaries {
		if summary.GetTarget().GetMetaGuid() == guid && summary.GetTarget().GetMetaType() == metaType {
			return summary
		}
	}
	return nil
}

// qualitySignalName prefers the name the caller's reference resolved to, and
// falls back to the path snapshot stored with the target, which is what answers
// when the caller addressed the object by GUID.
func qualitySignalName(resolved string, summary *v1pb.QualityTargetSummary) string {
	if resolved != "" {
		return resolved
	}
	return summary.GetTarget().GetPath().GetObject()
}

// qualitySignalStatus renders the worst latest result. UNSPECIFIED means no check
// of the target has run yet, and it is omitted rather than reported as a status a
// model would have to learn to ignore.
func qualitySignalStatus(status v1pb.QualityStatus) string {
	if status == v1pb.QualityStatus_QUALITY_STATUS_UNSPECIFIED {
		return ""
	}
	return status.String()
}

// qualitySignalTime renders a timestamp the way the rest of this surface does; an
// unset one is omitted.
func qualitySignalTime(timestamp *timestamppb.Timestamp) string {
	if timestamp == nil || timestamp.AsTime().Unix() <= 0 {
		return ""
	}
	return timestamp.AsTime().UTC().Format(time.RFC3339)
}

// --- quality read models ---

// qualityListParent names the current workspace, the way every quality RPC does.
const qualityListParent = "workspaces/-"

// qualityTargetKey identifies one target: a GUID alone does not identify a
// registry row across object types.
type qualityTargetKey struct {
	guid     string
	metaType v1pb.MetaType
}

// qualityTargetRef is the target identity a quality row carries. Path is the
// target's stored path snapshot, absent when the target no longer has a live
// check.
type qualityTargetRef struct {
	GUID     string          `json:"guid"`
	MetaType string          `json:"metaType,omitempty"`
	Path     json.RawMessage `json:"path,omitempty"`
}

// qualityTargetRefOf builds a row's target from the API's target message and a
// path the caller rendered.
func qualityTargetRefOf(target *v1pb.QualityTarget, path json.RawMessage) qualityTargetRef {
	ref := qualityTargetRef{Path: path}
	if target == nil {
		return ref
	}
	ref.GUID = target.GetMetaGuid()
	ref.MetaType = qualityEnumName(target.GetMetaType())
	return ref
}

// qualityTargetPath renders the path snapshot stored with a resource; an unset
// one stays absent.
func qualityTargetPath(target *v1pb.QualityTarget) (json.RawMessage, error) {
	path := target.GetPath()
	if path == nil {
		return nil, nil
	}
	return metadata.ProtoJSON(path)
}

// qualityEnumName renders an enum for the wire and omits the unspecified value: a
// model should not have to learn to ignore it.
func qualityEnumName(value interface{ String() string }) string {
	name := value.String()
	if name == "" || name == "UNSPECIFIED" || strings.HasSuffix(name, "_UNSPECIFIED") {
		return ""
	}
	return name
}

// qualityEnum reads an enum argument, accepting both the short name a model
// writes (OPEN, FAIL, SCHEDULED) and the protobuf name an earlier result
// rendered (QUALITY_ISSUE_STATUS_OPEN).
func qualityEnum[E ~int32](value, prefix string, names map[string]int32) (E, error) {
	trimmed := strings.ToUpper(strings.TrimSpace(value))
	if trimmed == "" {
		return 0, nil
	}
	number, ok := names[trimmed]
	if !ok {
		number, ok = names[prefix+trimmed]
	}
	if !ok {
		return 0, metadata.InvalidArgument("unknown value %q; accepted values are %s", value,
			strings.Join(qualityEnumValues(prefix, names), ", "))
	}
	return E(number), nil
}

// qualityEnumValues lists an enum's short names for an error message, without the
// unspecified value nobody should ask for.
func qualityEnumValues(prefix string, names map[string]int32) []string {
	values := make([]string, 0, len(names))
	for name := range names {
		if short := strings.TrimPrefix(name, prefix); short != name && short != "UNSPECIFIED" {
			values = append(values, short)
		}
	}
	slices.Sort(values)
	return values
}

// objectRefIsZero reports that a listing tool was given no object filter.
func objectRefIsZero(ref metadata.ObjectRef) bool {
	return ref == metadata.ObjectRef{}
}

// requireQualityListTarget refuses a listing object that no check can watch:
// issues and executions belong to check targets, and an empty page would read as
// "this object has none". A bare guid is allowed, because it leaves the type
// unknown and the RPC filters by GUID only.
func requireQualityListTarget(object metadata.Resolved, subject string) error {
	if object.MetaType == v1pb.MetaType_UNSPECIFIED || qualitySignalMetaTypes[object.MetaType] {
		return nil
	}
	return metadata.NewError(metadata.CodeInvalidArgument,
		subject+" belong to quality check targets: a table, external table, view or materialized view",
		"address one of those, or pass the target's guid")
}

// --- list_quality_issues ---

type ListQualityIssuesArgs struct {
	Object    metadata.ObjectRef `json:"object"`
	Rule      string             `json:"rule"`
	Status    string             `json:"status"`
	Severity  string             `json:"severity"`
	PageSize  int32              `json:"page_size"`
	PageToken string             `json:"page_token"`
}

// qualityIssueRow is one problem-ledger row. It deliberately carries no rule
// detail: get_quality_target and the execution tools answer that.
type qualityIssueRow struct {
	Name               string           `json:"name"`
	Rule               string           `json:"rule"`
	Target             qualityTargetRef `json:"target"`
	Status             string           `json:"status,omitempty"`
	Severity           string           `json:"severity,omitempty"`
	FirstSeenTime      string           `json:"firstSeenTime,omitempty"`
	LastSeenTime       string           `json:"lastSeenTime,omitempty"`
	FirstSeenExecution string           `json:"firstSeenExecution,omitempty"`
	LastSeenExecution  string           `json:"lastSeenExecution,omitempty"`
	OccurrenceCount    int32            `json:"occurrenceCount"`
	AcknowledgedBy     string           `json:"acknowledgedBy,omitempty"`
	AcknowledgedTime   string           `json:"acknowledgedTime,omitempty"`
	ResolvedTime       string           `json:"resolvedTime,omitempty"`
	ResolutionReason   string           `json:"resolutionReason,omitempty"`
	ResolutionNote     string           `json:"resolutionNote,omitempty"`
	MutedUntil         string           `json:"mutedUntil,omitempty"`
}

type listQualityIssuesResult struct {
	Issues        []qualityIssueRow `json:"issues"`
	NextPageToken string            `json:"nextPageToken,omitempty"`
}

func (t *Tools) ListQualityIssues(ctx context.Context, raw json.RawMessage) (any, error) {
	var args ListQualityIssuesArgs
	if err := metadata.DecodeArguments(raw, &args); err != nil {
		return nil, err
	}
	status, err := qualityEnum[v1pb.QualityIssueStatus](args.Status, "QUALITY_ISSUE_STATUS_", v1pb.QualityIssueStatus_value)
	if err != nil {
		return nil, err
	}
	severity, err := qualityEnum[v1pb.QualitySeverity](args.Severity, "QUALITY_SEVERITY_", v1pb.QualitySeverity_value)
	if err != nil {
		return nil, err
	}
	pageSize, err := normalizePageSize(args.PageSize)
	if err != nil {
		return nil, err
	}
	request := &v1pb.ListQualityIssuesRequest{
		Parent:    qualityListParent,
		PageSize:  pageSize,
		PageToken: args.PageToken,
		Status:    status,
		Severity:  severity,
		Rule:      strings.TrimSpace(args.Rule),
	}
	if !objectRefIsZero(args.Object) {
		object, err := t.resolver.ResolveObject(ctx, args.Object, v1pb.MetaType_UNSPECIFIED)
		if err != nil {
			return nil, err
		}
		if err := requireQualityListTarget(object, "quality issues"); err != nil {
			return nil, err
		}
		request.TargetGuid = object.GUID
	}
	response, err := t.quality.ListQualityIssues(ctx, connect.NewRequest(request))
	if err != nil {
		return nil, metadata.FromRPC(err)
	}
	issues := response.Msg.GetQualityIssues()
	paths := t.qualityIssueTargetPaths(ctx, issues)
	rows := qualityIssueRows(issues, func(target *v1pb.QualityTarget) json.RawMessage {
		return paths[qualityTargetKey{guid: target.GetMetaGuid(), metaType: target.GetMetaType()}]
	})
	return listQualityIssuesResult{Issues: rows, NextPageToken: response.Msg.GetNextPageToken()}, nil
}

// qualityIssueRows projects a page of issues. pathOf supplies each row's target
// path, which the API does not carry on an issue: it is enriched from the target
// directory, or taken from the target being opened.
func qualityIssueRows(issues []*v1pb.QualityIssue, pathOf func(*v1pb.QualityTarget) json.RawMessage) []qualityIssueRow {
	rows := make([]qualityIssueRow, 0, len(issues))
	for _, issue := range issues {
		if issue == nil {
			continue
		}
		target := issue.GetTarget()
		rows = append(rows, qualityIssueRow{
			Name:               issue.GetName(),
			Rule:               issue.GetRule(),
			Target:             qualityTargetRefOf(target, pathOf(target)),
			Status:             qualityEnumName(issue.GetStatus()),
			Severity:           qualityEnumName(issue.GetSeverity()),
			FirstSeenTime:      qualitySignalTime(issue.GetFirstSeenTime()),
			LastSeenTime:       qualitySignalTime(issue.GetLastSeenTime()),
			FirstSeenExecution: issue.GetFirstSeenExecution(),
			LastSeenExecution:  issue.GetLastSeenExecution(),
			OccurrenceCount:    issue.GetOccurrenceCount(),
			AcknowledgedBy:     issue.GetAcknowledgedBy(),
			AcknowledgedTime:   qualitySignalTime(issue.GetAcknowledgedTime()),
			ResolvedTime:       qualitySignalTime(issue.GetResolvedTime()),
			ResolutionReason:   qualityEnumName(issue.GetResolutionReason()),
			ResolutionNote:     issue.GetResolutionNote(),
			MutedUntil:         qualitySignalTime(issue.GetMutedUntil()),
		})
	}
	return rows
}

// qualityIssueTargetPaths reads the stored path snapshot of every target a page of
// issues names, in one exact-selection call. A page holds at most 200 issues, and
// a selection accepts at most 200 keys. It is best effort: an enrichment failure
// leaves the paths out rather than losing the listing.
func (t *Tools) qualityIssueTargetPaths(ctx context.Context, issues []*v1pb.QualityIssue) map[qualityTargetKey]json.RawMessage {
	paths := map[qualityTargetKey]json.RawMessage{}
	keys := qualityIssueTargetKeys(issues)
	if len(keys) == 0 {
		return paths
	}
	response, err := t.quality.ListQualityTargets(ctx, connect.NewRequest(&v1pb.ListQualityTargetsRequest{
		Parent:  qualityListParent,
		Targets: keys,
	}))
	if err != nil {
		return paths
	}
	for _, summary := range response.Msg.GetQualityTargets() {
		path := summary.GetTarget().GetPath()
		if path == nil {
			continue
		}
		encoded, err := metadata.ProtoJSON(path)
		if err != nil {
			continue
		}
		paths[qualityTargetKey{
			guid:     summary.GetTarget().GetMetaGuid(),
			metaType: summary.GetTarget().GetMetaType(),
		}] = encoded
	}
	return paths
}

// qualityIssueTargetKeys is the distinct set of targets a page of issues names,
// limited to the types a check can watch because a selection rejects any other.
func qualityIssueTargetKeys(issues []*v1pb.QualityIssue) []*v1pb.QualityTargetKey {
	keys := make([]*v1pb.QualityTargetKey, 0, len(issues))
	seen := make(map[qualityTargetKey]bool, len(issues))
	for _, issue := range issues {
		target := issue.GetTarget()
		if target == nil || target.GetMetaGuid() == "" || !qualitySignalMetaTypes[target.GetMetaType()] {
			continue
		}
		key := qualityTargetKey{guid: target.GetMetaGuid(), metaType: target.GetMetaType()}
		if seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, &v1pb.QualityTargetKey{TargetGuid: key.guid, MetaType: key.metaType})
	}
	return keys
}

// --- get_quality_target ---

type GetQualityTargetArgs struct {
	Object   metadata.ObjectRef `json:"object"`
	MetaType string             `json:"meta_type"`
}

// qualityTargetInfo names the target a workspace read was opened on.
type qualityTargetInfo struct {
	GUID     string `json:"guid"`
	MetaType string `json:"metaType"`
	Name     string `json:"name,omitempty"`
}

// getQualityTargetResult is one target's workspace: the header, every live check
// and the unresolved problems. Rule history and the target's column list stay out:
// the execution tools and get_metadata answer those.
type getQualityTargetResult struct {
	Monitored bool `json:"monitored"`
	// AllPaused says every check on the target is paused: the checks exist but
	// nothing is being verified, so the summary's verdict is history rather than
	// a current health claim.
	AllPaused      bool              `json:"allPaused"`
	Target         qualityTargetInfo `json:"target"`
	Summary        json.RawMessage   `json:"summary,omitempty"`
	Rules          []qualityRuleRow  `json:"rules"`
	OpenIssues     []qualityIssueRow `json:"openIssues"`
	OrphanedReason string            `json:"orphanedReason,omitempty"`
}

// qualityRuleRow is one check as this surface shows it: its definition, state and
// latest result, without the rule's target, description, timestamps or history.
type qualityRuleRow struct {
	Name                string          `json:"name"`
	Title               string          `json:"title"`
	Dimension           string          `json:"dimension,omitempty"`
	RuleType            string          `json:"ruleType,omitempty"`
	Config              json.RawMessage `json:"config,omitempty"`
	Severity            string          `json:"severity,omitempty"`
	State               string          `json:"state,omitempty"`
	Owner               string          `json:"owner,omitempty"`
	Revision            int32           `json:"revision"`
	FrequencyTier       string          `json:"frequencyTier,omitempty"`
	Tags                []string        `json:"tags"`
	TargetSchemaChanged bool            `json:"targetSchemaChanged"`
	OrphanedReason      string          `json:"orphanedReason,omitempty"`
	MuteUntil           string          `json:"muteUntil,omitempty"`
	LastResult          json.RawMessage `json:"lastResult,omitempty"`
}

func (t *Tools) GetQualityTarget(ctx context.Context, raw json.RawMessage) (any, error) {
	var args GetQualityTargetArgs
	if err := metadata.DecodeArguments(raw, &args); err != nil {
		return nil, err
	}
	metaType, err := parseMetaType(args.MetaType)
	if err != nil {
		return nil, err
	}
	if metaType == v1pb.MetaType_UNSPECIFIED && args.Object.GUID != "" {
		return nil, metadata.NewError(metadata.CodeInvalidArgument, "meta_type is required when the reference is a bare guid",
			"list_metadata and search_metadata return metaType with every object, or address the object by name instead")
	}
	object, err := t.resolver.ResolveObject(ctx, args.Object, metaType)
	if err != nil {
		return nil, err
	}
	if !qualitySignalMetaTypes[object.MetaType] {
		// A rule can only watch a table or view; the quality RPC would refuse the
		// key, so "nothing watches this" is the truthful answer.
		return qualitySignalNotMonitored(object), nil
	}
	response, err := t.quality.GetQualityTarget(ctx, connect.NewRequest(&v1pb.GetQualityTargetRequest{
		Parent:     qualityListParent,
		TargetGuid: object.GUID,
		MetaType:   object.MetaType,
	}))
	if err != nil {
		return nil, metadata.FromRPC(err)
	}
	summary := response.Msg.GetSummary()
	allPaused := qualityTargetAllPaused(summary)
	var encodedSummary, targetPath json.RawMessage
	if summary != nil {
		// A paused check's last verdict is history, not a current claim: clear it
		// from a copy of the summary, the way get_quality_signal omits it.
		rendered := summary
		if allPaused {
			cloned := &v1pb.QualityTargetSummary{}
			proto.Merge(cloned, summary)
			cloned.WorstStatus = v1pb.QualityStatus_QUALITY_STATUS_UNSPECIFIED
			rendered = cloned
		}
		encodedSummary, err = metadata.ProtoJSON(rendered)
		if err != nil {
			return nil, err
		}
		targetPath, err = qualityTargetPath(summary.GetTarget())
		if err != nil {
			return nil, err
		}
	}
	rules, err := qualityRuleRows(response.Msg.GetQualityRules())
	if err != nil {
		return nil, err
	}
	// Every open issue belongs to this target, so the workspace's path fills them
	// all, the same shape list_quality_issues returns.
	openIssues := qualityIssueRows(response.Msg.GetOpenIssues(), func(*v1pb.QualityTarget) json.RawMessage {
		return targetPath
	})
	return getQualityTargetResult{
		Monitored: summary.GetRuleCount() > 0,
		AllPaused: allPaused,
		Target: qualityTargetInfo{
			GUID:     object.GUID,
			MetaType: object.MetaType.String(),
			Name:     qualitySignalName(object.Name, summary),
		},
		Summary:        encodedSummary,
		Rules:          rules,
		OpenIssues:     openIssues,
		OrphanedReason: response.Msg.GetOrphanedReason(),
	}, nil
}

// qualityTargetAllPaused reports a target whose checks all exist but are paused:
// the target is monitored in the sense that checks are defined, and nothing is
// being verified.
func qualityTargetAllPaused(summary *v1pb.QualityTargetSummary) bool {
	return summary.GetRuleCount() > 0 && summary.GetEnabledRuleCount() == 0
}

// qualityStatusJudged reports whether this verdict carries a measurement. Only
// PASS, WARN and FAIL are judged outcomes; ERROR, SKIPPED and NOT_EVALUATED have
// no observation.
func qualityStatusJudged(status v1pb.QualityStatus) bool {
	switch status {
	case v1pb.QualityStatus_QUALITY_STATUS_PASS,
		v1pb.QualityStatus_QUALITY_STATUS_WARN,
		v1pb.QualityStatus_QUALITY_STATUS_FAIL:
		return true
	case v1pb.QualityStatus_QUALITY_STATUS_UNSPECIFIED,
		v1pb.QualityStatus_QUALITY_STATUS_ERROR,
		v1pb.QualityStatus_QUALITY_STATUS_SKIPPED,
		v1pb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED:
		return false
	default:
		return false
	}
}

// qualityRuleRows projects a target's checks.
func qualityRuleRows(rules []*v1pb.QualityRule) ([]qualityRuleRow, error) {
	rows := make([]qualityRuleRow, 0, len(rules))
	for _, rule := range rules {
		if rule == nil {
			continue
		}
		row := qualityRuleRow{
			Name:                rule.GetName(),
			Title:               rule.GetTitle(),
			Dimension:           qualityEnumName(rule.GetDimension()),
			RuleType:            qualityEnumName(rule.GetRuleType()),
			Severity:            qualityEnumName(rule.GetSeverity()),
			State:               qualityEnumName(rule.GetState()),
			Owner:               rule.GetOwner(),
			Revision:            rule.GetRevision(),
			FrequencyTier:       qualityEnumName(rule.GetFrequencyTier()),
			Tags:                metadata.StringsOrEmpty(rule.GetTags()),
			TargetSchemaChanged: rule.GetTargetSchemaChanged(),
			OrphanedReason:      rule.GetOrphanedReason(),
			MuteUntil:           qualitySignalTime(rule.GetMuteUntil()),
		}
		if config := rule.GetConfig(); config != nil {
			encoded, err := metadata.ProtoJSON(config)
			if err != nil {
				return nil, err
			}
			row.Config = encoded
		}
		if lastResult := rule.GetLastResult(); lastResult != nil {
			encoded, err := metadata.ProtoJSON(lastResult)
			if err != nil {
				return nil, err
			}
			row.LastResult = encoded
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// --- list_quality_executions ---

type ListQualityExecutionsArgs struct {
	Object    metadata.ObjectRef `json:"object"`
	Rule      string             `json:"rule"`
	State     string             `json:"state"`
	Outcome   string             `json:"outcome"`
	Trigger   string             `json:"trigger"`
	PageSize  int32              `json:"page_size"`
	PageToken string             `json:"page_token"`
}

// qualityExecutionRow is one run as a listing shows it: the verdict and the flags
// that say how far to trust it. The evidence itself stays in get_quality_execution:
// failed sample rows and executed SQL are one run's worth of data, not a page't.
type qualityExecutionRow struct {
	Name      string           `json:"name"`
	Rule      string           `json:"rule"`
	Target    qualityTargetRef `json:"target"`
	Instance  string           `json:"instance,omitempty"`
	Run       string           `json:"run,omitempty"`
	Trigger   string           `json:"trigger,omitempty"`
	State     string           `json:"state,omitempty"`
	Outcome   string           `json:"outcome,omitempty"`
	Attempt   int32            `json:"attempt"`
	Initiator string           `json:"initiator,omitempty"`
	// ObservedValue is absent when the run produced no measurement: only a judged
	// verdict (PASS, WARN, FAIL) carries one, and a present 0 is a real
	// observation.
	ObservedValue *float64        `json:"observedValue,omitempty"`
	ObservedKind  string          `json:"observedKind,omitempty"`
	Thresholds    json.RawMessage `json:"thresholds,omitempty"`
	FailedRows    int64           `json:"failedRows"`
	TotalRows     int64           `json:"totalRows"`
	Sampled       bool            `json:"sampled"`
	SampleRows    int64           `json:"sampleRows"`
	Truncated     bool            `json:"truncated"`
	ErrorCode     string          `json:"errorCode,omitempty"`
	ErrorMessage  string          `json:"errorMessage,omitempty"`
	Duration      json.RawMessage `json:"duration,omitempty"`
	StartTime     string          `json:"startTime,omitempty"`
	FinishTime    string          `json:"finishTime,omitempty"`
	BaselineValue *float64        `json:"baselineValue,omitempty"`
	BaselineTime  string          `json:"baselineTime,omitempty"`
}

type listQualityExecutionsResult struct {
	Executions    []qualityExecutionRow `json:"executions"`
	NextPageToken string                `json:"nextPageToken,omitempty"`
}

func (t *Tools) ListQualityExecutions(ctx context.Context, raw json.RawMessage) (any, error) {
	var args ListQualityExecutionsArgs
	if err := metadata.DecodeArguments(raw, &args); err != nil {
		return nil, err
	}
	state, err := qualityEnum[v1pb.QualityExecutionState](args.State, "QUALITY_EXECUTION_STATE_", v1pb.QualityExecutionState_value)
	if err != nil {
		return nil, err
	}
	outcome, err := qualityEnum[v1pb.QualityStatus](args.Outcome, "QUALITY_STATUS_", v1pb.QualityStatus_value)
	if err != nil {
		return nil, err
	}
	trigger, err := qualityEnum[v1pb.QualityExecutionTrigger](args.Trigger, "QUALITY_EXECUTION_TRIGGER_", v1pb.QualityExecutionTrigger_value)
	if err != nil {
		return nil, err
	}
	pageSize, err := normalizePageSize(args.PageSize)
	if err != nil {
		return nil, err
	}
	request := &v1pb.ListQualityExecutionsRequest{
		Parent:    qualityListParent,
		PageSize:  pageSize,
		PageToken: args.PageToken,
		State:     state,
		Outcome:   outcome,
		Trigger:   trigger,
		Rule:      strings.TrimSpace(args.Rule),
	}
	if !objectRefIsZero(args.Object) {
		object, err := t.resolver.ResolveObject(ctx, args.Object, v1pb.MetaType_UNSPECIFIED)
		if err != nil {
			return nil, err
		}
		if err := requireQualityListTarget(object, "quality executions"); err != nil {
			return nil, err
		}
		request.TargetGuid = object.GUID
	}
	response, err := t.quality.ListQualityExecutions(ctx, connect.NewRequest(request))
	if err != nil {
		return nil, metadata.FromRPC(err)
	}
	executions := response.Msg.GetQualityExecutions()
	result := listQualityExecutionsResult{
		Executions:    make([]qualityExecutionRow, 0, len(executions)),
		NextPageToken: response.Msg.GetNextPageToken(),
	}
	for _, execution := range executions {
		if execution == nil {
			continue
		}
		row, err := qualityExecutionRowOf(execution)
		if err != nil {
			return nil, err
		}
		result.Executions = append(result.Executions, row)
	}
	return result, nil
}

// qualityExecutionRowOf projects one run.
func qualityExecutionRowOf(execution *v1pb.QualityExecution) (qualityExecutionRow, error) {
	path, err := qualityTargetPath(execution.GetTarget())
	if err != nil {
		return qualityExecutionRow{}, err
	}
	row := qualityExecutionRow{
		Name:          execution.GetName(),
		Rule:          execution.GetRule(),
		Target:        qualityTargetRefOf(execution.GetTarget(), path),
		Instance:      execution.GetInstance(),
		Run:           execution.GetRun(),
		Trigger:       qualityEnumName(execution.GetTrigger()),
		State:         qualityEnumName(execution.GetState()),
		Outcome:       qualityEnumName(execution.GetOutcome()),
		Attempt:       execution.GetAttempt(),
		Initiator:     execution.GetInitiator(),
		ObservedKind:  qualityEnumName(execution.GetObservedKind()),
		FailedRows:    execution.GetFailedRows(),
		TotalRows:     execution.GetTotalRows(),
		Sampled:       execution.GetSampled(),
		SampleRows:    execution.GetSampleRows(),
		Truncated:     execution.GetTruncated(),
		ErrorCode:     execution.GetErrorCode(),
		ErrorMessage:  execution.GetErrorMessage(),
		StartTime:     qualitySignalTime(execution.GetStartTime()),
		FinishTime:    qualitySignalTime(execution.GetFinishTime()),
		BaselineValue: execution.BaselineValue,
		BaselineTime:  qualitySignalTime(execution.GetBaselineTime()),
	}
	// Only a judged verdict records a measurement; for ERROR, SKIPPED and
	// NOT_EVALUATED the API's non-optional field is 0, which would read as an
	// observed zero.
	if qualityStatusJudged(execution.GetOutcome()) {
		observed := execution.GetObservedValue()
		row.ObservedValue = &observed
	}
	if thresholds := execution.GetThresholds(); thresholds != nil {
		row.Thresholds, err = metadata.ProtoJSON(thresholds)
		if err != nil {
			return qualityExecutionRow{}, err
		}
	}
	if duration := execution.GetDuration(); duration != nil {
		row.Duration, err = metadata.ProtoJSON(duration)
		if err != nil {
			return qualityExecutionRow{}, err
		}
	}
	return row, nil
}

// --- get_quality_execution ---

type GetQualityExecutionArgs struct {
	Execution string `json:"execution"`
}

func (t *Tools) GetQualityExecution(ctx context.Context, raw json.RawMessage) (any, error) {
	var args GetQualityExecutionArgs
	if err := metadata.DecodeArguments(raw, &args); err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.Execution) == "" {
		return nil, metadata.InvalidArgument("execution is required; list_quality_executions returns one per run")
	}
	response, err := t.quality.GetQualityExecution(ctx, connect.NewRequest(&v1pb.GetQualityExecutionRequest{
		Name: args.Execution,
	}))
	if err != nil {
		return nil, metadata.FromRPC(err)
	}
	// The whole execution is the answer: this is the evidence tool, so nothing is
	// projected away.
	return metadata.ProtoJSON(response.Msg)
}

// --- get_quality_impact ---

type GetQualityImpactArgs struct {
	Object      metadata.ObjectRef `json:"object"`
	MetaType    string             `json:"meta_type"`
	Depth       int32              `json:"depth"`
	WindowHours int32              `json:"window_hours"`
}

func (t *Tools) GetQualityImpact(ctx context.Context, raw json.RawMessage) (any, error) {
	var args GetQualityImpactArgs
	if err := metadata.DecodeArguments(raw, &args); err != nil {
		return nil, err
	}
	metaType, err := parseMetaType(args.MetaType)
	if err != nil {
		return nil, err
	}
	if metaType == v1pb.MetaType_UNSPECIFIED && args.Object.GUID != "" {
		return nil, metadata.NewError(metadata.CodeInvalidArgument, "meta_type is required when the reference is a bare guid",
			"list_metadata and search_metadata return metaType with every object, or address the object by name instead")
	}
	object, err := t.resolver.ResolveObject(ctx, args.Object, metaType)
	if err != nil {
		return nil, err
	}
	if !qualitySignalMetaTypes[object.MetaType] {
		// Only the four object types a check can watch carry an impact: a walk from
		// a reported statement or a schema has no assets to answer with.
		return nil, metadata.NewError(metadata.CodeInvalidArgument,
			"impact can only be read for a table, external table, view or materialized view",
			"list_metadata and search_metadata return metaType with every object")
	}
	response, err := t.quality.GetQualityTargetImpact(ctx, connect.NewRequest(&v1pb.GetQualityTargetImpactRequest{
		Parent: qualityListParent,
		Target: &v1pb.QualityTargetKey{
			TargetGuid: object.GUID,
			MetaType:   object.MetaType,
		},
		Depth:       args.Depth,
		WindowHours: args.WindowHours,
	}))
	if err != nil {
		return nil, metadata.FromRPC(err)
	}
	// The whole answer is evidence: the model decides what matters, so nothing is
	// projected away.
	return metadata.ProtoJSON(response.Msg)
}

// --- get_quality_consumption_coverage ---

type GetQualityConsumptionCoverageArgs struct {
	PageSize int32 `json:"page_size"`
}

func (t *Tools) GetQualityConsumptionCoverage(ctx context.Context, raw json.RawMessage) (any, error) {
	var args GetQualityConsumptionCoverageArgs
	if err := metadata.DecodeArguments(raw, &args); err != nil {
		return nil, err
	}
	// The RPC owns this bound (0 means 20, above 100 is coerced), so the raw value
	// goes through; the tool's schema documents the same range.
	response, err := t.quality.GetQualityConsumptionCoverage(ctx, connect.NewRequest(&v1pb.GetQualityConsumptionCoverageRequest{
		Parent:   qualityListParent,
		PageSize: args.PageSize,
	}))
	if err != nil {
		return nil, metadata.FromRPC(err)
	}
	return metadata.ProtoJSON(response.Msg)
}

// --- list_quality_targets ---

type ListQualityTargetsArgs struct {
	Search       string `json:"search"`
	WorstStatus  string `json:"worst_status"`
	HasOpenIssue bool   `json:"has_open_issue"`
	PageSize     int32  `json:"page_size"`
	PageToken    string `json:"page_token"`
}

// qualityTargetRow is one row of the target directory.
type qualityTargetRow struct {
	GUID             string          `json:"guid"`
	MetaType         string          `json:"metaType"`
	Path             json.RawMessage `json:"path,omitempty"`
	RuleCount        int32           `json:"ruleCount"`
	EnabledRuleCount int32           `json:"enabledRuleCount"`
	PausedRuleCount  int32           `json:"pausedRuleCount"`
	// AllPaused says every check is paused: the target's worst latest result is
	// history, so WorstStatus stays absent rather than reading as a current claim.
	AllPaused            bool            `json:"allPaused"`
	ColumnCount          int32           `json:"columnCount"`
	OpenIssueCount       int32           `json:"openIssueCount"`
	PendingDraftCount    int32           `json:"pendingDraftCount"`
	PendingLabelCount    int32           `json:"pendingLabelCount"`
	WorstStatus          string          `json:"worstStatus,omitempty"`
	LastRunTime          string          `json:"lastRunTime,omitempty"`
	SchemaChanged        bool            `json:"schemaChanged"`
	Orphaned             bool            `json:"orphaned"`
	QualityScore         *float64        `json:"qualityScore,omitempty"`
	ScoredRuleCount      int32           `json:"scoredRuleCount"`
	RecentPassRate       *float64        `json:"recentPassRate,omitempty"`
	RecentExecutionCount int32           `json:"recentExecutionCount"`
	CoveredColumnCount   int32           `json:"coveredColumnCount"`
	TotalColumnCount     int32           `json:"totalColumnCount"`
	LatestRun            json.RawMessage `json:"latestRun,omitempty"`
	RunCount             int32           `json:"runCount"`
	Schedule             json.RawMessage `json:"schedule,omitempty"`
}

type listQualityTargetsResult struct {
	Targets       []qualityTargetRow `json:"targets"`
	NextPageToken string             `json:"nextPageToken,omitempty"`
}

func (t *Tools) ListQualityTargets(ctx context.Context, raw json.RawMessage) (any, error) {
	var args ListQualityTargetsArgs
	if err := metadata.DecodeArguments(raw, &args); err != nil {
		return nil, err
	}
	worstStatus, err := qualityEnum[v1pb.QualityStatus](args.WorstStatus, "QUALITY_STATUS_", v1pb.QualityStatus_value)
	if err != nil {
		return nil, err
	}
	pageSize, err := normalizePageSize(args.PageSize)
	if err != nil {
		return nil, err
	}
	response, err := t.quality.ListQualityTargets(ctx, connect.NewRequest(&v1pb.ListQualityTargetsRequest{
		Parent:       qualityListParent,
		PageSize:     pageSize,
		PageToken:    args.PageToken,
		Search:       strings.TrimSpace(args.Search),
		WorstStatus:  worstStatus,
		HasOpenIssue: args.HasOpenIssue,
	}))
	if err != nil {
		return nil, metadata.FromRPC(err)
	}
	summaries := response.Msg.GetQualityTargets()
	result := listQualityTargetsResult{
		Targets:       make([]qualityTargetRow, 0, len(summaries)),
		NextPageToken: response.Msg.GetNextPageToken(),
	}
	for _, summary := range summaries {
		if summary == nil {
			continue
		}
		row, err := qualityTargetRowOf(summary)
		if err != nil {
			return nil, err
		}
		result.Targets = append(result.Targets, row)
	}
	return result, nil
}

// qualityTargetRowOf projects one directory row.
func qualityTargetRowOf(summary *v1pb.QualityTargetSummary) (qualityTargetRow, error) {
	path, err := qualityTargetPath(summary.GetTarget())
	if err != nil {
		return qualityTargetRow{}, err
	}
	allPaused := qualityTargetAllPaused(summary)
	row := qualityTargetRow{
		GUID:                 summary.GetTarget().GetMetaGuid(),
		MetaType:             qualityEnumName(summary.GetTarget().GetMetaType()),
		Path:                 path,
		RuleCount:            summary.GetRuleCount(),
		EnabledRuleCount:     summary.GetEnabledRuleCount(),
		PausedRuleCount:      summary.GetPausedRuleCount(),
		AllPaused:            allPaused,
		ColumnCount:          summary.GetColumnCount(),
		OpenIssueCount:       summary.GetOpenIssueCount(),
		PendingDraftCount:    summary.GetPendingDraftCount(),
		PendingLabelCount:    summary.GetPendingLabelCount(),
		LastRunTime:          qualitySignalTime(summary.GetLastRunTime()),
		SchemaChanged:        summary.GetSchemaChanged(),
		Orphaned:             summary.GetOrphaned(),
		QualityScore:         summary.QualityScore,
		ScoredRuleCount:      summary.GetScoredRuleCount(),
		RecentPassRate:       summary.RecentPassRate,
		RecentExecutionCount: summary.GetRecentExecutionCount(),
		CoveredColumnCount:   summary.GetCoveredColumnCount(),
		TotalColumnCount:     summary.GetTotalColumnCount(),
		RunCount:             summary.GetRunCount(),
	}
	// A paused target's last verdict is history, so it is omitted the way
	// get_quality_signal omits it.
	if !allPaused {
		row.WorstStatus = qualitySignalStatus(summary.GetWorstStatus())
	}
	if latestRun := summary.GetLatestRun(); latestRun != nil {
		row.LatestRun, err = metadata.ProtoJSON(latestRun)
		if err != nil {
			return qualityTargetRow{}, err
		}
	}
	if schedule := summary.GetSchedule(); schedule != nil {
		row.Schedule, err = metadata.ProtoJSON(schedule)
		if err != nil {
			return qualityTargetRow{}, err
		}
	}
	return row, nil
}
