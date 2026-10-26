package v1

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// GetQualityContract reads one target's contract: the target-level settings, the
// newest published version and whether the live rules differ from it.
//
// The contract is a projection over the rules, not a second definition store: a
// rule edit changes the live state immediately, and only publishing records it.
func (s *DataQualityService) GetQualityContract(ctx context.Context, req *connect.Request[v1pb.GetQualityContractRequest]) (*connect.Response[v1pb.QualityContract], error) {
	if _, err := s.workspaceID(ctx, req.Msg.GetParent()); err != nil {
		return nil, err
	}
	contract, err := s.qualityContractView(ctx, req.Msg.GetTargetGuid(), storepb.MetaType(req.Msg.GetMetaType()))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(contract), nil
}

// UpdateQualityContract replaces the target-level settings. The filter is
// validated against the target's columns here, because it is compiled into every
// rule on the target rather than stored per rule.
func (s *DataQualityService) UpdateQualityContract(ctx context.Context, req *connect.Request[v1pb.UpdateQualityContractRequest]) (*connect.Response[v1pb.QualityContract], error) {
	if _, err := s.workspaceID(ctx, req.Msg.GetParent()); err != nil {
		return nil, err
	}
	targetGUID := strings.TrimSpace(req.Msg.GetTargetGuid())
	if targetGUID == "" {
		return nil, connectErrorInvalid("target_guid is required")
	}
	if req.Msg.GetSettings() == nil {
		return nil, connectErrorInvalid("settings is required")
	}
	settings := convertQualityContractSettingsFromV1(req.Msg.GetSettings())
	if err := validateQualityContractSettings(settings); err != nil {
		return nil, err
	}
	rules, err := s.store.ListQualityRules(ctx, &store.FindQualityRuleMessage{TargetGUID: &targetGUID})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the target's quality rules")
	}
	metaType := storepb.MetaType(req.Msg.GetMetaType())
	if metaType == storepb.MetaType_UNSPECIFIED && len(rules) > 0 {
		metaType = rules[0].TargetType
	}
	if metaType == storepb.MetaType_UNSPECIFIED {
		return nil, connectErrorInvalid("meta_type is required when the target has no rules")
	}
	if err := dataquality.ValidateTargetFilter(ctx, s.store, targetGUID, metaType, settings.GetFilter()); err != nil {
		return nil, err
	}
	if _, err := s.store.UpsertQualityContractSettings(ctx, targetGUID, metaType, settings); err != nil {
		return nil, err
	}
	contract, err := s.qualityContractView(ctx, targetGUID, metaType)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(contract), nil
}

// PublishQualityContract records the live rules and settings as the next version.
// Publishing an unchanged target is a no-op that returns the newest version.
func (s *DataQualityService) PublishQualityContract(ctx context.Context, req *connect.Request[v1pb.PublishQualityContractRequest]) (*connect.Response[v1pb.PublishQualityContractResponse], error) {
	if _, err := s.workspaceID(ctx, req.Msg.GetParent()); err != nil {
		return nil, err
	}
	targetGUID := strings.TrimSpace(req.Msg.GetTargetGuid())
	if targetGUID == "" {
		return nil, connectErrorInvalid("target_guid is required")
	}
	metaType := storepb.MetaType(req.Msg.GetMetaType())
	rules, contract, resolution, err := s.qualityContractContext(ctx, targetGUID, metaType)
	if err != nil {
		return nil, err
	}
	if metaType == storepb.MetaType_UNSPECIFIED {
		metaType = resolution.Target.MetaType
	}
	var settings *storepb.QualityContractSettings
	if contract != nil {
		settings = contract.Settings
	}
	if settings == nil {
		settings = &storepb.QualityContractSettings{}
	}

	checks := buildQualityContractChecks(rules)
	snapshot := &storepb.QualityContractSnapshot{
		Settings:    settings,
		Checks:      checks,
		Fingerprint: qualityContractFingerprint(settings, checks),
	}
	var publishedChecks []*storepb.QualityContractCheck
	var latest *store.QualityContractRevisionMessage
	if contract != nil {
		latest, err = s.store.GetLatestQualityContractRevision(ctx, contract.ID)
		if err != nil {
			return nil, err
		}
		if latest != nil {
			publishedChecks = latest.Snapshot.GetChecks()
		}
	}
	change := qualityContractChangeSummary(publishedChecks, checks)

	publishedBy := currentUserID(ctx)
	var publishedByPtr *int32
	if publishedBy > 0 {
		id := int32(publishedBy)
		publishedByPtr = &id
	}
	revision, storedContract, created, err := s.store.PublishQualityContractRevision(ctx, &store.PublishQualityContractInput{
		TargetGUID:    targetGUID,
		TargetType:    metaType,
		Settings:      settings,
		Snapshot:      snapshot,
		CheckCount:    int32(len(checks)),
		AddedCount:    change.GetAdded(),
		ModifiedCount: change.GetModified(),
		RemovedCount:  change.GetRemoved(),
		Note:          strings.TrimSpace(req.Msg.GetNote()),
		PublishedBy:   publishedByPtr,
	})
	if err != nil {
		return nil, err
	}
	view, err := s.qualityContractViewWith(ctx, targetGUID, metaType, storedContract, rules, revision)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1pb.PublishQualityContractResponse{
		Contract:  view,
		Revision:  convertQualityContractRevisionToV1(revision),
		NoChanges: !created,
	}), nil
}

// ListQualityContractRevisions lists the target's published versions, newest
// first. A target with no contract yet has no versions rather than an error.
func (s *DataQualityService) ListQualityContractRevisions(ctx context.Context, req *connect.Request[v1pb.ListQualityContractRevisionsRequest]) (*connect.Response[v1pb.ListQualityContractRevisionsResponse], error) {
	if _, err := s.workspaceID(ctx, req.Msg.GetParent()); err != nil {
		return nil, err
	}
	targetGUID := strings.TrimSpace(req.Msg.GetTargetGuid())
	if targetGUID == "" {
		return nil, connectErrorInvalid("target_guid is required")
	}
	offset, err := parseLimitAndOffset(&pageSize{
		token:   req.Msg.GetPageToken(),
		limit:   int(req.Msg.GetPageSize()),
		maximum: qualityListMaximum,
	})
	if err != nil {
		return nil, err
	}
	contract, err := s.findQualityContract(ctx, targetGUID, storepb.MetaType(req.Msg.GetMetaType()))
	if err != nil {
		return nil, err
	}
	response := &v1pb.ListQualityContractRevisionsResponse{}
	if contract == nil {
		return connect.NewResponse(response), nil
	}
	revisions, err := s.store.ListQualityContractRevisions(ctx, contract.ID, offset.limit+1, offset.offset)
	if err != nil {
		return nil, err
	}
	revisions, nextPageToken, err := paginate(revisions, offset)
	if err != nil {
		return nil, err
	}
	response.NextPageToken = nextPageToken
	for _, revision := range revisions {
		response.Revisions = append(response.Revisions, convertQualityContractRevisionToV1(revision))
	}
	return connect.NewResponse(response), nil
}

// GetQualityContractRevision reads one published version with its ordered checks.
func (s *DataQualityService) GetQualityContractRevision(ctx context.Context, req *connect.Request[v1pb.GetQualityContractRevisionRequest]) (*connect.Response[v1pb.GetQualityContractRevisionResponse], error) {
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	targetGUID := strings.TrimSpace(req.Msg.GetTargetGuid())
	if targetGUID == "" {
		return nil, connectErrorInvalid("target_guid is required")
	}
	version := req.Msg.GetVersion()
	if version <= 0 {
		return nil, connectErrorInvalid("version must be positive")
	}
	contract, err := s.findQualityContract(ctx, targetGUID, storepb.MetaType(req.Msg.GetMetaType()))
	if err != nil {
		return nil, err
	}
	if contract == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("target %q has no published contract", targetGUID))
	}
	revision, err := s.store.GetQualityContractRevisionByVersion(ctx, contract.ID, version)
	if err != nil {
		return nil, err
	}
	if revision == nil {
		return nil, connect.NewError(connect.CodeNotFound,
			errors.Errorf("quality contract version %d not found", version))
	}
	response := &v1pb.GetQualityContractRevisionResponse{
		Revision: convertQualityContractRevisionToV1(revision),
	}
	for _, check := range revision.Snapshot.GetChecks() {
		response.Checks = append(response.Checks, convertQualityContractCheckToV1(workspaceID, check))
	}
	return connect.NewResponse(response), nil
}

// qualityContractView renders the live contract for one target.
func (s *DataQualityService) qualityContractView(ctx context.Context, targetGUID string, metaType storepb.MetaType) (*v1pb.QualityContract, error) {
	rules, contract, resolution, err := s.qualityContractContext(ctx, targetGUID, metaType)
	if err != nil {
		return nil, err
	}
	if metaType == storepb.MetaType_UNSPECIFIED && resolution != nil {
		metaType = resolution.Target.MetaType
	}
	return s.qualityContractViewWith(ctx, targetGUID, metaType, contract, rules, nil)
}

// qualityContractViewWith renders a contract from already-loaded rows. revision is
// the version the caller just published, or nil to read the newest one.
func (s *DataQualityService) qualityContractViewWith(ctx context.Context, targetGUID string, metaType storepb.MetaType, contract *store.QualityContractMessage, rules []*store.QualityRuleMessage, revision *store.QualityContractRevisionMessage) (*v1pb.QualityContract, error) {
	var settings *storepb.QualityContractSettings
	if contract != nil {
		settings = contract.Settings
	}
	if settings == nil {
		settings = &storepb.QualityContractSettings{}
	}
	var latest *store.QualityContractRevisionMessage
	if revision != nil {
		latest = revision
	} else if contract != nil {
		var err error
		latest, err = s.store.GetLatestQualityContractRevision(ctx, contract.ID)
		if err != nil {
			return nil, err
		}
	}
	checks := buildQualityContractChecks(rules)
	view := &v1pb.QualityContract{
		Target: &v1pb.QualityTarget{
			MetaGuid: targetGUID,
			MetaType: v1pb.MetaType(metaType),
			Path:     convertQualityTargetPathToV1(qualityContractTargetPath(rules)),
		},
		Settings:         convertQualityContractSettingsToV1(settings),
		CheckCount:       int32(len(checks)),
		PublishedVersion: qualityContractPublishedVersion(contract),
	}
	if latest != nil {
		published := qualityContractChangeSummary(latest.Snapshot.GetChecks(), checks)
		view.ChangeSummary = published
		view.PublishedAt = timestamppb.New(latest.PublishedAt.UTC())
		view.PublishedBy = formatQualityContractPublisher(latest.PublishedBy)
		view.PublishNote = latest.Note
		view.HasUnpublishedChanges = latest.Snapshot.GetFingerprint() != qualityContractFingerprint(settings, checks)
	} else {
		view.ChangeSummary = &v1pb.QualityContractChangeSummary{Added: int32(len(checks))}
		view.HasUnpublishedChanges = len(checks) > 0 || !qualityContractSettingsEmpty(settings)
	}
	return view, nil
}

// qualityContractContext loads the rules, contract row and resolved target in the
// order the handlers need. The resolution may be nil when the target is gone but
// rules still reference it.
func (s *DataQualityService) qualityContractContext(ctx context.Context, targetGUID string, metaType storepb.MetaType) ([]*store.QualityRuleMessage, *store.QualityContractMessage, *dataquality.Resolution, error) {
	rules, err := s.store.ListQualityRules(ctx, &store.FindQualityRuleMessage{TargetGUID: &targetGUID})
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "failed to read the target's quality rules")
	}
	if metaType == storepb.MetaType_UNSPECIFIED && len(rules) > 0 {
		metaType = rules[0].TargetType
	}
	if metaType == storepb.MetaType_UNSPECIFIED {
		return nil, nil, nil, connectErrorInvalid("meta_type is required when the target has no rules")
	}
	resolution, resolveErr := dataquality.ResolveTarget(ctx, s.store, targetGUID, metaType)
	if resolveErr != nil {
		if !errors.Is(resolveErr, dataquality.ErrOrphaned) || len(rules) == 0 {
			return nil, nil, nil, resolveErr
		}
	}
	contract, err := s.store.GetQualityContract(ctx, targetGUID, metaType)
	if err != nil {
		return nil, nil, nil, err
	}
	return rules, contract, resolution, nil
}

// findQualityContract loads the contract row for a target without the rules
// needed by the read paths that only list versions.
func (s *DataQualityService) findQualityContract(ctx context.Context, targetGUID string, metaType storepb.MetaType) (*store.QualityContractMessage, error) {
	rules, err := s.store.ListQualityRules(ctx, &store.FindQualityRuleMessage{TargetGUID: &targetGUID})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the target's quality rules")
	}
	if metaType == storepb.MetaType_UNSPECIFIED && len(rules) > 0 {
		metaType = rules[0].TargetType
	}
	if metaType == storepb.MetaType_UNSPECIFIED {
		return nil, nil
	}
	return s.store.GetQualityContract(ctx, targetGUID, metaType)
}

// qualityContractTargetPath picks the display path: the live rules' snapshot when
// there is one, otherwise nothing (the contract has no path of its own).
func qualityContractTargetPath(rules []*store.QualityRuleMessage) *storepb.QualityTargetPath {
	for _, rule := range rules {
		if rule.TargetPath != nil && rule.TargetPath.Object != "" {
			return rule.TargetPath
		}
	}
	return nil
}

// buildQualityContractChecks renders the live rules as ordered contract checks:
// column rules first by column name, then table-level rules by id, so two
// publishes of the same set produce the same fingerprint.
func buildQualityContractChecks(rules []*store.QualityRuleMessage) []*storepb.QualityContractCheck {
	sorted := slices.Clone(rules)
	slices.SortFunc(sorted, func(a, b *store.QualityRuleMessage) int {
		if cmp := strings.Compare(contractColumn(a), contractColumn(b)); cmp != 0 {
			return cmp
		}
		switch {
		case a.ID < b.ID:
			return -1
		case a.ID > b.ID:
			return 1
		default:
			return 0
		}
	})
	checks := make([]*storepb.QualityContractCheck, 0, len(sorted))
	for _, rule := range sorted {
		checks = append(checks, &storepb.QualityContractCheck{
			RuleResourceId: rule.ResourceID,
			RuleRevision:   rule.Revision,
			Title:          rule.Title,
			RuleType:       rule.RuleType,
			Severity:       rule.Severity,
			Column:         contractColumn(rule),
		})
	}
	return checks
}

// contractColumn is the column a rule watches; empty for a table-level rule.
func contractColumn(rule *store.QualityRuleMessage) string {
	if rule.TargetPath == nil {
		return ""
	}
	return rule.TargetPath.Column
}

// qualityContractFingerprint hashes the settings and ordered checks. Publishing
// the same state twice produces the same value, which is what makes republishing
// a no-op.
func qualityContractFingerprint(settings *storepb.QualityContractSettings, checks []*storepb.QualityContractCheck) string {
	snapshot := &storepb.QualityContractSnapshot{Settings: settings, Checks: checks}
	payload, err := protojson.Marshal(snapshot)
	if err != nil {
		// The messages contain only strings and ints, so marshaling cannot fail in
		// practice; an empty fingerprint would only make the next publish a no-op.
		return ""
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// qualityContractChangeSummary compares the published checks with the live ones:
// added, removed, and modified (same rule id, different revision or display
// fields).
func qualityContractChangeSummary(published, live []*storepb.QualityContractCheck) *v1pb.QualityContractChangeSummary {
	publishedByID := make(map[string]*storepb.QualityContractCheck, len(published))
	for _, check := range published {
		publishedByID[check.GetRuleResourceId()] = check
	}
	liveByID := make(map[string]*storepb.QualityContractCheck, len(live))
	summary := &v1pb.QualityContractChangeSummary{}
	for _, check := range live {
		liveByID[check.GetRuleResourceId()] = check
		previous, ok := publishedByID[check.GetRuleResourceId()]
		if !ok {
			summary.Added++
			continue
		}
		if !qualityContractCheckEqual(previous, check) {
			summary.Modified++
		}
	}
	for _, check := range published {
		if _, ok := liveByID[check.GetRuleResourceId()]; !ok {
			summary.Removed++
		}
	}
	return summary
}

// qualityContractCheckEqual reports whether two checks describe the same rule
// definition and display state.
func qualityContractCheckEqual(a, b *storepb.QualityContractCheck) bool {
	return a.GetRuleRevision() == b.GetRuleRevision() &&
		a.GetTitle() == b.GetTitle() &&
		a.GetRuleType() == b.GetRuleType() &&
		a.GetSeverity() == b.GetSeverity() &&
		a.GetColumn() == b.GetColumn()
}

// qualityContractSettingsEmpty reports whether settings carry anything.
func qualityContractSettingsEmpty(settings *storepb.QualityContractSettings) bool {
	return len(settings.GetFilter().GetClauses()) == 0 &&
		settings.GetDefaultScheduleIntervalSeconds() == 0 &&
		settings.GetDefaultSeverity() == storepb.QualitySeverity_QUALITY_SEVERITY_UNSPECIFIED
}

// validateQualityContractSettings checks the bounds of the settings that do not
// need the target's metadata.
func validateQualityContractSettings(settings *storepb.QualityContractSettings) error {
	if seconds := settings.GetDefaultScheduleIntervalSeconds(); seconds != 0 {
		interval := time.Duration(seconds) * time.Second
		if interval < minQualityScheduleInterval || interval > maxQualityScheduleInterval {
			return connectErrorInvalid("the default schedule must be between 15 minutes and 7 days")
		}
	}
	severity := settings.GetDefaultSeverity()
	switch severity {
	case storepb.QualitySeverity_QUALITY_SEVERITY_UNSPECIFIED,
		storepb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		storepb.QualitySeverity_QUALITY_SEVERITY_ERROR,
		storepb.QualitySeverity_QUALITY_SEVERITY_CRITICAL:
	default:
		return connectErrorInvalid("unknown default severity %d", severity)
	}
	return nil
}

// convertQualityContractSettingsToV1 renders the stored settings.
func convertQualityContractSettingsToV1(settings *storepb.QualityContractSettings) *v1pb.QualityContractSettings {
	if settings == nil {
		return &v1pb.QualityContractSettings{}
	}
	return &v1pb.QualityContractSettings{
		Filter:                         convertQualityFilterToV1(settings.GetFilter()),
		DefaultScheduleIntervalSeconds: settings.GetDefaultScheduleIntervalSeconds(),
		DefaultSeverity:                v1pb.QualitySeverity(settings.GetDefaultSeverity()),
	}
}

// convertQualityContractSettingsFromV1 renders the request settings.
func convertQualityContractSettingsFromV1(settings *v1pb.QualityContractSettings) *storepb.QualityContractSettings {
	if settings == nil {
		return &storepb.QualityContractSettings{}
	}
	return &storepb.QualityContractSettings{
		Filter:                         convertQualityFilterFromV1(settings.GetFilter()),
		DefaultScheduleIntervalSeconds: settings.GetDefaultScheduleIntervalSeconds(),
		DefaultSeverity:                storepb.QualitySeverity(settings.GetDefaultSeverity()),
	}
}

// convertQualityContractRevisionToV1 renders one published version's metadata.
func convertQualityContractRevisionToV1(revision *store.QualityContractRevisionMessage) *v1pb.QualityContractRevision {
	if revision == nil {
		return nil
	}
	return &v1pb.QualityContractRevision{
		Version:     revision.Version,
		PublishedAt: timestamppb.New(revision.PublishedAt.UTC()),
		PublishedBy: formatQualityContractPublisher(revision.PublishedBy),
		Note:        revision.Note,
		CheckCount:  revision.CheckCount,
		ChangeSummary: &v1pb.QualityContractChangeSummary{
			Added:    revision.AddedCount,
			Modified: revision.ModifiedCount,
			Removed:  revision.RemovedCount,
		},
	}
}

// convertQualityContractCheckToV1 renders one published check with its rule name.
func convertQualityContractCheckToV1(workspaceID string, check *storepb.QualityContractCheck) *v1pb.QualityContractCheck {
	if check == nil {
		return nil
	}
	return &v1pb.QualityContractCheck{
		Rule:         formatQualityRuleName(workspaceID, check.GetRuleResourceId()),
		RuleRevision: check.GetRuleRevision(),
		Title:        check.GetTitle(),
		RuleType:     v1pb.QualityRuleType(check.GetRuleType()),
		Severity:     v1pb.QualitySeverity(check.GetSeverity()),
		Column:       check.GetColumn(),
	}
}

// formatQualityContractPublisher renders a principal id as a user resource name.
func formatQualityContractPublisher(id *int32) string {
	if id == nil {
		return ""
	}
	return common.FormatUserUID(int(*id))
}

// qualityContractPublishedVersion reads the published version from a possibly
// absent contract row.
func qualityContractPublishedVersion(contract *store.QualityContractMessage) int32 {
	if contract == nil {
		return 0
	}
	return contract.PublishedVersion
}
