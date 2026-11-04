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
	"google.golang.org/protobuf/proto"
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
	targetGUID := strings.TrimSpace(req.Msg.GetTargetGuid())
	if targetGUID == "" {
		return nil, connectErrorInvalid("target_guid is required")
	}
	contract, err := s.qualityContractView(ctx, targetGUID, storepb.MetaType(req.Msg.GetMetaType()))
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
	settings, err := convertQualityContractSettingsFromV1(req.Msg.GetSettings())
	if err != nil {
		return nil, err
	}
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
	existing, err := s.store.GetQualityContract(ctx, targetGUID, metaType)
	if err != nil {
		return nil, err
	}
	var previousIntent *storepb.QualityContractIntent
	if existing != nil {
		previousIntent = existing.Settings.GetIntent()
	}
	settings.Intent = normalizeQualityContractIntent(settings.GetIntent(), currentUserID(ctx), previousIntent)
	if err := dataquality.ValidateTargetFilter(ctx, s.store, targetGUID, metaType, settings.GetFilter()); err != nil {
		return nil, err
	}
	if err := dataquality.ValidateContractIntent(ctx, s.store, targetGUID, metaType, settings.GetIntent()); err != nil {
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
	// The plan payload is the same declaration the read view renders: the
	// criticality template plus the intent statements, with the target's schedule
	// projection. Publishing it makes "what this version promised" explicit.
	schedule, err := s.store.GetQualityTargetSchedule(ctx, targetGUID, metaType)
	if err != nil {
		return nil, err
	}
	plan := qualityContractPlanSnapshot(settings.GetIntent(), rules, schedule)
	// The store decides under its row lock whether this snapshot is a no-op: a
	// legacy row is compared by content, a v2 row by its own fingerprint schema, a
	// v3 row by the plan-bearing fingerprint. Deciding here would race a publish.
	snapshot := qualityContractSnapshotV3(settings, plan, checks)
	// The version's diff counts are computed by the store inside the publish
	// transaction, against the snapshot that really is the previous version.
	note, err := qualityContractNote(req.Msg.GetNote())
	if err != nil {
		return nil, err
	}

	publishedBy := currentUserID(ctx)
	var publishedByPtr *int32
	if publishedBy > 0 {
		id := int32(publishedBy)
		publishedByPtr = &id
	}
	revision, storedContract, created, err := s.store.PublishQualityContractRevision(ctx, &store.PublishQualityContractInput{
		TargetGUID:  targetGUID,
		TargetType:  metaType,
		Settings:    settings,
		Snapshot:    snapshot,
		Note:        note,
		PublishedBy: publishedByPtr,
	})
	if err != nil {
		if common.ErrorCode(err) == common.Conflict {
			// The settings changed between the read and the publish transaction.
			return nil, connect.NewError(connect.CodeAborted, err)
		}
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
		Plan:     convertQualityContractPlanToV1(revision.Snapshot.GetPlan()),
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
	// The plan is a read-only projection of the same rules; it joins the contract
	// view so the workspace can show what the target promises and what it lacks
	// without a second request.
	view.Plan = qualityPlanView(targetGUID, metaType, rules, contract)
	if latest != nil {
		added, modified, removed := store.QualityContractCheckDiff(latest.Snapshot.GetChecks(), checks)
		view.ChangeSummary = qualityContractChangeSummary(added, modified, removed)
		view.PublishedAt = timestamppb.New(latest.PublishedAt.UTC())
		view.PublishedBy = formatQualityContractPublisher(latest.PublishedBy)
		view.PublishNote = latest.Note
		switch latest.Snapshot.GetSnapshotVersion() {
		case 0:
			// A legacy snapshot's fingerprint was produced by a build-dependent
			// encoder; compare the content instead of re-hashing it.
			view.HasUnpublishedChanges = !store.QualityContractSnapshotContentEqual(
				latest.Snapshot,
				&storepb.QualityContractSnapshot{Settings: settings, Checks: checks},
			)
		case 2:
			// A v2 version predates the plan payload; compare it the way it was
			// written so an untouched contract does not look changed after upgrade.
			view.HasUnpublishedChanges = latest.Snapshot.GetFingerprint() !=
				qualityContractFingerprintV2(settings, checks)
		default:
			schedule, err := s.store.GetQualityTargetSchedule(ctx, targetGUID, metaType)
			if err != nil {
				return nil, err
			}
			plan := qualityContractPlanSnapshot(settings.GetIntent(), rules, schedule)
			view.HasUnpublishedChanges = latest.Snapshot.GetFingerprint() !=
				qualityContractFingerprintV3(settings, plan, checks)
		}
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
// The config is authoritative: the target path's column is only a display
// snapshot and can be missing.
func contractColumn(rule *store.QualityRuleMessage) string {
	return dataquality.ConfigColumn(rule.Config)
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

// qualityContractFingerprintV2 hashes the snapshot with a deterministic encoder.
// protojson is not stable across builds (its encoder randomizes insignificant
// whitespace with a per-binary seed), so hashing it would make an unchanged
// contract look changed after a redeploy and create a spurious revision. The
// version prefix and version field make a v1 and a v2 fingerprint unable to
// compare equal by accident.
func qualityContractFingerprintV2(settings *storepb.QualityContractSettings, checks []*storepb.QualityContractCheck) string {
	snapshot := &storepb.QualityContractSnapshot{
		Settings:        settings,
		Checks:          checks,
		SnapshotVersion: contractSnapshotVersionV2,
	}
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(snapshot)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(payload)
	return contractSnapshotVersionPrefixV2 + hex.EncodeToString(sum[:])
}

// contractSnapshotVersionV2 is the schema version every new publish records.
const contractSnapshotVersionV2 = 2

// contractSnapshotVersionPrefixV2 tags a v2 fingerprint. The prefix is derived
// from the same constant the snapshot version uses so the two cannot drift.
const contractSnapshotVersionPrefixV2 = "v2:"

// contractSnapshotVersionV3 is the schema version every new publish records: the
// v2 deterministic fingerprint plus the plan payload.
const contractSnapshotVersionV3 = 3

// contractSnapshotVersionPrefixV3 tags a v3 fingerprint. A v2 and a v3 fingerprint
// never compare equal, which is what keeps the no-op rule honest across the schema
// upgrade: an untouched v2 version is compared with its own schema, not this one.
const contractSnapshotVersionPrefixV3 = "v3:"

// qualityContractFingerprintV3 hashes the settings, plan, ordered checks and schema
// version with a deterministic encoder.
func qualityContractFingerprintV3(settings *storepb.QualityContractSettings, plan *storepb.QualityContractPlan, checks []*storepb.QualityContractCheck) string {
	snapshot := &storepb.QualityContractSnapshot{
		Settings:        settings,
		Plan:            plan,
		Checks:          checks,
		SnapshotVersion: contractSnapshotVersionV3,
	}
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(snapshot)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(payload)
	return contractSnapshotVersionPrefixV3 + hex.EncodeToString(sum[:])
}

// qualityContractSnapshotV3 builds the snapshot a publish writes: settings, plan,
// the ordered checks, the schema version and the deterministic fingerprint.
func qualityContractSnapshotV3(settings *storepb.QualityContractSettings, plan *storepb.QualityContractPlan, checks []*storepb.QualityContractCheck) *storepb.QualityContractSnapshot {
	return &storepb.QualityContractSnapshot{
		Settings:        settings,
		Plan:            plan,
		Checks:          checks,
		Fingerprint:     qualityContractFingerprintV3(settings, plan, checks),
		SnapshotVersion: contractSnapshotVersionV3,
	}
}

// qualityContractSnapshotV2 builds the snapshot a publish writes: settings, the
// ordered checks, the schema version and the deterministic fingerprint.
func qualityContractSnapshotV2(settings *storepb.QualityContractSettings, checks []*storepb.QualityContractCheck) *storepb.QualityContractSnapshot {
	return &storepb.QualityContractSnapshot{
		Settings:        settings,
		Checks:          checks,
		Fingerprint:     qualityContractFingerprintV2(settings, checks),
		SnapshotVersion: contractSnapshotVersionV2,
	}
}

// qualityContractChangeSummary renders the counts the store computed into the API
// message. The comparison itself lives in the store so the publish transaction and
// this read view cannot disagree about what "added/modified/removed" means.
func qualityContractChangeSummary(added, modified, removed int32) *v1pb.QualityContractChangeSummary {
	return &v1pb.QualityContractChangeSummary{
		Added:    added,
		Modified: modified,
		Removed:  removed,
	}
}

// qualityContractSettingsEmpty reports whether settings carry anything.
func qualityContractSettingsEmpty(settings *storepb.QualityContractSettings) bool {
	return len(settings.GetFilter().GetClauses()) == 0 &&
		settings.GetDefaultSeverity() == storepb.QualitySeverity_QUALITY_SEVERITY_UNSPECIFIED &&
		qualityContractIntentEmpty(settings.GetIntent())
}

// qualityContractIntentEmpty reports whether the intent declares nothing. A source
// alone is not a declaration: setting it does not make an empty intent look like a
// change.
func qualityContractIntentEmpty(intent *storepb.QualityContractIntent) bool {
	return intent == nil ||
		(intent.GetCriticality() == storepb.QualityContractCriticality_QUALITY_CONTRACT_CRITICALITY_UNSPECIFIED &&
			len(intent.GetStatements()) == 0 &&
			len(intent.GetDependents()) == 0 &&
			len(intent.GetExemptions()) == 0 &&
			intent.GetEffectiveFromUnixMs() == 0 &&
			intent.GetDomainRef() == "")
}

// maxQualityContractNoteBytes bounds a published version's note. The UI limits
// the field to 500 characters; the API allows some headroom but not an unbounded
// value.
const maxQualityContractNoteBytes = 2000

// qualityContractNote trims a publish note and rejects one that is too long
// instead of silently truncating the history text.
func qualityContractNote(note string) (string, error) {
	note = strings.TrimSpace(note)
	if len(note) > maxQualityContractNoteBytes {
		return "", connectErrorInvalid("the publish note is too long (max %d bytes)", maxQualityContractNoteBytes)
	}
	return note, nil
}

// validateQualityContractSettings checks the bounds of the settings that do not
// need the target's metadata.
func validateQualityContractSettings(settings *storepb.QualityContractSettings) error {
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
		Filter:          convertQualityFilterToV1(settings.GetFilter()),
		DefaultSeverity: v1pb.QualitySeverity(settings.GetDefaultSeverity()),
		Intent:          convertQualityContractIntentToV1(settings.GetIntent()),
	}
}

// convertQualityContractSettingsFromV1 renders the request settings and rejects an
// intent whose shape is invalid. The enums are validated here because the
// conversions are casts: an unknown value must not reach the store.
func convertQualityContractSettingsFromV1(settings *v1pb.QualityContractSettings) (*storepb.QualityContractSettings, error) {
	if settings == nil {
		return &storepb.QualityContractSettings{}, nil
	}
	intent, err := convertQualityContractIntentFromV1(settings.GetIntent())
	if err != nil {
		return nil, err
	}
	return &storepb.QualityContractSettings{
		Filter:          convertQualityFilterFromV1(settings.GetFilter()),
		DefaultSeverity: storepb.QualitySeverity(settings.GetDefaultSeverity()),
		Intent:          intent,
	}, nil
}

// normalizeQualityContractIntent records who wrote the intent and treats an intent
// that declares nothing as absent. This phase has no approval workflow, so an
// exemption keeps the approver it already had and a new one records the editor; the
// workflow replaces that attribution later. Replacing settings (for example to
// change the contract filter) must not rewrite an existing exemption's approver.
func normalizeQualityContractIntent(intent *storepb.QualityContractIntent, editorID int, previous *storepb.QualityContractIntent) *storepb.QualityContractIntent {
	if intent == nil || qualityContractIntentEmpty(intent) {
		return nil
	}
	if intent.GetSource() == storepb.QualityContractIntentSource_QUALITY_CONTRACT_INTENT_SOURCE_UNSPECIFIED {
		intent.Source = storepb.QualityContractIntentSource_QUALITY_CONTRACT_INTENT_SOURCE_HUMAN
	}
	for _, exemption := range intent.GetExemptions() {
		if approver := retainedQualityContractExemptionApprover(exemption, previous); approver > 0 {
			exemption.ApprovedBy = approver
		} else if editorID > 0 {
			exemption.ApprovedBy = int32(editorID)
		}
	}
	return intent
}

// retainedQualityContractExemptionApprover returns the approver an identical
// previously stored exemption already records. Identity is the full declaration:
// editing its reason or expiry makes it a new exemption, attributed to this editor.
func retainedQualityContractExemptionApprover(exemption *storepb.QualityContractIntentExemption, previous *storepb.QualityContractIntent) int32 {
	for _, candidate := range previous.GetExemptions() {
		if candidate.GetApprovedBy() == 0 ||
			candidate.GetProperty() != exemption.GetProperty() ||
			candidate.GetColumn() != exemption.GetColumn() ||
			candidate.GetReason() != exemption.GetReason() ||
			candidate.GetExpiresAtUnixMs() != exemption.GetExpiresAtUnixMs() {
			continue
		}
		return candidate.GetApprovedBy()
	}
	return 0
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
		Intent:     convertQualityContractIntentToV1(revision.Snapshot.GetSettings().GetIntent()),
		ChangeKind: v1pb.QualityContractChangeKind(revision.ChangeKind),
		Plan:       convertQualityContractPlanToV1(revision.Snapshot.GetPlan()),
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

// Bounds for an intent declaration: the whole intent lives inside the contract
// settings JSONB, so it must not be an unbounded payload.
const (
	maxContractIntentStatements  = 32
	maxContractIntentDependents  = 32
	maxContractIntentExemptions  = 32
	maxContractIntentTextBytes   = 500
	maxContractIntentDomainBytes = 200
)

// convertQualityContractIntentToV1 renders a stored intent.
func convertQualityContractIntentToV1(intent *storepb.QualityContractIntent) *v1pb.QualityContractIntent {
	if intent == nil {
		return nil
	}
	converted := &v1pb.QualityContractIntent{
		Criticality:   v1pb.QualityAssetCriticality(intent.GetCriticality()),
		Dependents:    slices.Clone(intent.GetDependents()),
		EffectiveFrom: qualityContractTimestampFromMillis(intent.GetEffectiveFromUnixMs()),
		Source:        v1pb.QualityContractIntentSource(intent.GetSource()),
		DomainRef:     intent.GetDomainRef(),
	}
	for _, statement := range intent.GetStatements() {
		converted.Statements = append(converted.Statements, &v1pb.QualityContractIntentStatement{
			Property:  v1pb.QualityCoverageProperty(statement.GetProperty()),
			Column:    statement.GetColumn(),
			Tolerance: statement.GetTolerance(),
		})
	}
	for _, exemption := range intent.GetExemptions() {
		var approvedBy string
		if exemption.GetApprovedBy() > 0 {
			id := exemption.GetApprovedBy()
			approvedBy = formatQualityContractPublisher(&id)
		}
		converted.Exemptions = append(converted.Exemptions, &v1pb.QualityContractIntentExemption{
			Property:   v1pb.QualityCoverageProperty(exemption.GetProperty()),
			Column:     exemption.GetColumn(),
			Reason:     exemption.GetReason(),
			ApprovedBy: approvedBy,
			ExpiresAt:  qualityContractTimestampFromMillis(exemption.GetExpiresAtUnixMs()),
		})
	}
	return converted
}

// convertQualityContractIntentFromV1 renders a request intent after validating its
// shape. The approved_by field is output only: the server records the editor.
func convertQualityContractIntentFromV1(intent *v1pb.QualityContractIntent) (*storepb.QualityContractIntent, error) {
	if intent == nil {
		return nil, nil
	}
	if err := validateQualityContractIntentShape(intent); err != nil {
		return nil, err
	}
	converted := &storepb.QualityContractIntent{
		Criticality:         storepb.QualityContractCriticality(intent.GetCriticality()),
		EffectiveFromUnixMs: qualityContractMillisFromTimestamp(intent.GetEffectiveFrom()),
		Source:              storepb.QualityContractIntentSource(intent.GetSource()),
		DomainRef:           strings.TrimSpace(intent.GetDomainRef()),
	}
	for _, dependent := range intent.GetDependents() {
		if dependent = strings.TrimSpace(dependent); dependent != "" {
			converted.Dependents = append(converted.Dependents, dependent)
		}
	}
	for _, statement := range intent.GetStatements() {
		converted.Statements = append(converted.Statements, &storepb.QualityContractIntentStatement{
			Property:  storepb.QualityCoverageProperty(statement.GetProperty()),
			Column:    strings.TrimSpace(statement.GetColumn()),
			Tolerance: strings.TrimSpace(statement.GetTolerance()),
		})
	}
	for _, exemption := range intent.GetExemptions() {
		converted.Exemptions = append(converted.Exemptions, &storepb.QualityContractIntentExemption{
			Property:        storepb.QualityCoverageProperty(exemption.GetProperty()),
			Column:          strings.TrimSpace(exemption.GetColumn()),
			Reason:          strings.TrimSpace(exemption.GetReason()),
			ExpiresAtUnixMs: qualityContractMillisFromTimestamp(exemption.GetExpiresAt()),
		})
	}
	return converted, nil
}

// validateQualityContractIntentShape rejects an intent the store must not hold:
// unknown enum values, unbounded lists or malformed timestamps.
func validateQualityContractIntentShape(intent *v1pb.QualityContractIntent) error {
	switch intent.GetCriticality() {
	case v1pb.QualityAssetCriticality_QUALITY_ASSET_CRITICALITY_UNSPECIFIED,
		v1pb.QualityAssetCriticality_QUALITY_ASSET_CRITICALITY_EXPERIMENTAL,
		v1pb.QualityAssetCriticality_QUALITY_ASSET_CRITICALITY_IMPORTANT,
		v1pb.QualityAssetCriticality_QUALITY_ASSET_CRITICALITY_CRITICAL:
	default:
		return connectErrorInvalid("unknown intent criticality %d", intent.GetCriticality())
	}
	switch intent.GetSource() {
	case v1pb.QualityContractIntentSource_QUALITY_CONTRACT_INTENT_SOURCE_UNSPECIFIED,
		v1pb.QualityContractIntentSource_QUALITY_CONTRACT_INTENT_SOURCE_HUMAN,
		v1pb.QualityContractIntentSource_QUALITY_CONTRACT_INTENT_SOURCE_AGENT_DRAFT,
		v1pb.QualityContractIntentSource_QUALITY_CONTRACT_INTENT_SOURCE_IMPORTED:
	default:
		return connectErrorInvalid("unknown intent source %d", intent.GetSource())
	}
	if err := checkQualityContractIntentTimestamp(intent.GetEffectiveFrom(), "effective_from"); err != nil {
		return err
	}
	if len(intent.GetStatements()) > maxContractIntentStatements {
		return connectErrorInvalid("an intent carries at most %d statements", maxContractIntentStatements)
	}
	for _, statement := range intent.GetStatements() {
		if err := validateQualityCoverageProperty(statement.GetProperty()); err != nil {
			return err
		}
		if len(statement.GetTolerance()) > maxContractIntentTextBytes {
			return connectErrorInvalid("an intent statement tolerance is too long (max %d bytes)", maxContractIntentTextBytes)
		}
	}
	if len(intent.GetDependents()) > maxContractIntentDependents {
		return connectErrorInvalid("an intent names at most %d dependents", maxContractIntentDependents)
	}
	for _, dependent := range intent.GetDependents() {
		if strings.TrimSpace(dependent) == "" {
			return connectErrorInvalid("an intent dependent must not be empty")
		}
		if len(dependent) > maxContractIntentTextBytes {
			return connectErrorInvalid("an intent dependent is too long (max %d bytes)", maxContractIntentTextBytes)
		}
	}
	if len(intent.GetExemptions()) > maxContractIntentExemptions {
		return connectErrorInvalid("an intent carries at most %d exemptions", maxContractIntentExemptions)
	}
	for _, exemption := range intent.GetExemptions() {
		if err := validateQualityCoverageProperty(exemption.GetProperty()); err != nil {
			return err
		}
		if len(strings.TrimSpace(exemption.GetReason())) == 0 {
			return connectErrorInvalid("an intent exemption needs a reason")
		}
		if len(exemption.GetReason()) > maxContractIntentTextBytes {
			return connectErrorInvalid("an intent exemption reason is too long (max %d bytes)", maxContractIntentTextBytes)
		}
		if err := checkQualityContractIntentTimestamp(exemption.GetExpiresAt(), "expires_at"); err != nil {
			return err
		}
	}
	if len(intent.GetDomainRef()) > maxContractIntentDomainBytes {
		return connectErrorInvalid("an intent domain_ref is too long (max %d bytes)", maxContractIntentDomainBytes)
	}
	return nil
}

// validateQualityCoverageProperty rejects a property outside the declared range;
// the store and API enums share their numbering, so the conversion is a cast.
func validateQualityCoverageProperty(property v1pb.QualityCoverageProperty) error {
	if property < v1pb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_VOLUME ||
		property > v1pb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_RANGE {
		return connectErrorInvalid("unknown coverage property %d", property)
	}
	return nil
}

// checkQualityContractIntentTimestamp rejects a timestamp outside the protobuf
// range instead of letting the cast store a meaningless value.
func checkQualityContractIntentTimestamp(timestamp *timestamppb.Timestamp, field string) error {
	if timestamp == nil {
		return nil
	}
	if err := timestamp.CheckValid(); err != nil {
		return connectErrorInvalid("intent %s is not a valid timestamp", field)
	}
	return nil
}

// qualityContractTimestampFromMillis renders a stored millisecond instant; zero
// means unset.
func qualityContractTimestampFromMillis(millis int64) *timestamppb.Timestamp {
	if millis <= 0 {
		return nil
	}
	return timestamppb.New(time.UnixMilli(millis))
}

// qualityContractMillisFromTimestamp reads an API instant as milliseconds; unset
// stays zero.
func qualityContractMillisFromTimestamp(timestamp *timestamppb.Timestamp) int64 {
	if timestamp == nil {
		return 0
	}
	return timestamp.AsTime().UnixMilli()
}
