package v1

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// RecommendQualityChecks derives candidate checks for one target from its stored
// column profiles. It is the deterministic generation pass: it needs no model and no
// provider configuration, so a deployment with the AI switch off can still turn a
// freshly synced table into a reviewable set of checks.
//
// The evidence is the cached profile, so every requested column must have a profile
// younger than the same bound the label pass uses; without one the request fails with
// FailedPrecondition pointing at ProfileColumns. Every candidate then goes through the
// rule pipeline (`RequireTarget` and `ValidateConfig`, plus the parent-table lookup for
// a referential check), so the queue never holds a draft the engine would refuse.
//
// The pass reconciles with facts and decisions exactly like the label pass: a saved
// rule with the same identity wins (a pending draft is closed as SUPERSEDED), a
// decided identity is not re-proposed unless forced, and a pending proposal produced by
// another pass is left alone unless the recommendation is explicitly allowed to
// replace it (see dataquality.SuggestionPassMayRefreshPending).
func (s *DataQualityService) RecommendQualityChecks(ctx context.Context, req *connect.Request[v1pb.RecommendQualityChecksRequest]) (*connect.Response[v1pb.RecommendQualityChecksResponse], error) {
	if req == nil {
		return nil, connectErrorInvalid("a request is required")
	}
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	guid := strings.TrimSpace(req.Msg.GetTableGuid())
	if guid == "" {
		return nil, connectErrorInvalid("table_guid is required")
	}
	metaType := storepb.MetaType(req.Msg.GetMetaType())
	if metaType == storepb.MetaType_UNSPECIFIED {
		return nil, connectErrorInvalid("meta_type is required")
	}
	settings, err := dataquality.LoadSettings(ctx, s.store)
	if err != nil {
		return nil, errLoadQualitySettings(err)
	}
	resolution, err := dataquality.ResolveTarget(ctx, s.store, guid, metaType)
	if err != nil {
		return nil, err
	}
	requestedColumns := trimmedQualityColumns(req.Msg.GetColumns())
	if len(requestedColumns) == 0 {
		requestedColumns = resolution.Target.ColumnNames()
	}
	if len(requestedColumns) == 0 {
		return nil, connectErrorInvalid("the target has no columns to recommend checks for")
	}
	if len(requestedColumns) > settings.MaxColumnsPerAnalysis {
		return nil, connectErrorInvalid("at most %d columns may be analyzed at once", settings.MaxColumnsPerAnalysis)
	}
	for _, column := range requestedColumns {
		if _, ok := resolution.Target.Column(column); !ok {
			return nil, connectErrorInvalid("column %q does not exist on the target", column)
		}
	}

	// The profile is the evidence. Reading it through the shared helper gives the same
	// freshness bound and the same actionable error as the label pass.
	profiles, err := s.qualityAnalysisProfiles(ctx, resolution.Target, requestedColumns)
	if err != nil {
		return nil, err
	}
	labels, err := s.qualityColumnLabels(ctx, resolution.Target, requestedColumns)
	if err != nil {
		return nil, err
	}
	rules, err := s.store.ListQualityRules(ctx, &store.FindQualityRuleMessage{TargetGUID: &guid})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to read the target's existing rules"))
	}
	ruleSignatures := dataquality.RuleSignatures(rules)

	now := time.Now().UTC()
	recommended := dataquality.RecommendChecks(dataquality.RecommendInput{
		Target:   resolution.Target,
		Metadata: resolution.Metadata,
		Profiles: profiles,
		Labels:   labels,
		Columns:  requestedColumns,
		Language: req.Msg.GetLanguage(),
		Now:      now,
	})
	candidates := s.validRecommendedChecks(ctx, resolution, recommended.Recommendations)

	summary := &storepb.QualitySuggestionQueueSummary{}
	summary.SkippedColumns = recommended.SkippedColumns
	if len(candidates) == 0 {
		// Nothing could be derived, so there is nothing to reconcile: the queue stays as
		// it is and the summary lists every column that produced no check.
		return connect.NewResponse(&v1pb.RecommendQualityChecksResponse{
			Summary: convertQualitySuggestionQueueSummary(summary),
		}), nil
	}

	signatures := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		signatures = append(signatures, candidate.Signature)
	}
	states, err := s.store.QualitySuggestionSignatureStates(ctx, guid, metaType, signatures)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to read the target's suggestion history"))
	}
	plan := planRecommendationQueue(candidates, ruleSignatures, states, req.Msg.GetForceRegenerate())
	summary.SkippedExistingFact = plan.Summary.GetSkippedExistingFact()
	summary.SkippedPending = plan.Summary.GetSkippedPending()
	summary.SkippedDecided = plan.Summary.GetSkippedDecided()

	if len(plan.Supersede) > 0 {
		// One statement closes every stale proposal this pass found; the returned count
		// is what actually changed (a concurrent reviewer may have decided one).
		superseded, err := s.store.SupersedePendingQualitySuggestions(ctx, guid, metaType,
			plan.Supersede, "a saved rule with the same identity exists", now)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to close a stale recommended check"))
		}
		summary.Superseded += int32(superseded)
	}

	callerID := principalIDPointer(currentUserID(ctx))
	queued := make([]*store.QualitySuggestionMessage, 0, len(plan.Queue))
	for _, candidate := range plan.Queue {
		suggestion, created, err := s.queueRecommendedCheck(ctx, candidate, resolution.Target, callerID, now)
		if err != nil {
			return nil, err
		}
		if suggestion == nil {
			// A concurrent pass queued its own proposal for this identity after the state
			// read; the database refused the refresh. Nothing was written.
			summary.SkippedPending++
			continue
		}
		if created {
			summary.Created++
		} else {
			summary.Refreshed++
		}
		queued = append(queued, suggestion)
	}

	suggestions := make([]*v1pb.QualitySuggestion, 0, len(queued))
	for _, suggestion := range queued {
		suggestions = append(suggestions, convertQualitySuggestion(workspaceID, suggestion))
	}
	return connect.NewResponse(&v1pb.RecommendQualityChecksResponse{
		QualitySuggestions: suggestions,
		Summary:            convertQualitySuggestionQueueSummary(summary),
	}), nil
}

// recommendQueuePlan is the pure decision of one recommendation pass: which candidates
// to queue, which stale pending proposals to close, and what was skipped. Keeping it
// separate from the handler makes the fact/decision/cross-pass rules testable without a
// store, which is where the queue semantics actually live.
type recommendQueuePlan struct {
	// Queue is the set of candidates to upsert, in derivation order.
	Queue []dataquality.Recommendation
	// Supersede are pending proposals whose identity a saved rule now covers.
	Supersede []string
	// Summary counts the skips; created/refreshed/superseded are filled by the caller
	// as the writes happen.
	Summary *storepb.QualitySuggestionQueueSummary
}

// planRecommendationQueue applies the reconciliation rules to the facts and queue state
// the caller read. A saved rule wins over a proposal; a pending proposal from another
// pass is left alone unless the cross-pass rule allows the replacement; a decided
// identity is not re-proposed unless forced.
func planRecommendationQueue(candidates []dataquality.Recommendation, ruleSignatures map[string]struct{}, states map[string]store.QualitySuggestionSignatureState, force bool) recommendQueuePlan {
	plan := recommendQueuePlan{Summary: &storepb.QualitySuggestionQueueSummary{}}
	for _, candidate := range candidates {
		state := states[candidate.Signature]
		if _, covered := ruleSignatures[candidate.Signature]; covered {
			if state.Pending {
				plan.Supersede = append(plan.Supersede, candidate.Signature)
			} else {
				plan.Summary.SkippedExistingFact++
			}
			continue
		}
		if state.Pending {
			if !dataquality.SuggestionPassMayRefreshPending(state,
				storepb.QualitySuggestionSource_QUALITY_SUGGESTION_SOURCE_RECOMMENDATION, force) {
				plan.Summary.SkippedPending++
				continue
			}
			plan.Queue = append(plan.Queue, candidate)
			continue
		}
		if !force && dataquality.SuggestionDecisionSuppressesRuleDraft(state.LastDecision) {
			plan.Summary.SkippedDecided++
			continue
		}
		plan.Queue = append(plan.Queue, candidate)
	}
	return plan
}

// validRecommendedChecks drops candidates the rule pipeline refuses. The pass derives
// its configs from the target's own metadata, so a refusal is a defect rather than a
// user error: it is logged and the remaining candidates are still offered.
func (s *DataQualityService) validRecommendedChecks(ctx context.Context, resolution *dataquality.Resolution, candidates []dataquality.Recommendation) []dataquality.Recommendation {
	valid := make([]dataquality.Recommendation, 0, len(candidates))
	for _, candidate := range candidates {
		if err := dataquality.RequireTarget(resolution, candidate.RuleType); err != nil {
			slog.Warn("dropping a recommended check the target cannot run",
				slog.String("table_guid", resolution.Target.GUID),
				slog.String("rule_type", candidate.RuleType.String()),
				slog.Any("error", err))
			continue
		}
		if err := dataquality.ValidateConfig(resolution.Target, candidate.RuleType, candidate.Config); err != nil {
			slog.Warn("dropping a recommended check whose config the target refuses",
				slog.String("table_guid", resolution.Target.GUID),
				slog.String("rule_type", candidate.RuleType.String()),
				slog.Any("error", err))
			continue
		}
		// A referential check names a parent relation the config-only layer cannot see;
		// the full rule validation resolves it against the registry.
		if candidate.RuleType == storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY {
			_, err := dataquality.ValidateRule(ctx, s.store, candidate.RuleType,
				resolution.Target.GUID, resolution.Target.MetaType, candidate.Config, nil, nil)
			if err != nil {
				slog.Warn("dropping a recommended referential check whose parent does not resolve",
					slog.String("table_guid", resolution.Target.GUID),
					slog.Any("error", err))
				continue
			}
		}
		valid = append(valid, candidate)
	}
	return valid
}

// queueRecommendedCheck upserts one recommended draft by its identity. A repeated pass
// refreshes the live row instead of appending a duplicate, and the returned bool says
// which happened. A deterministic draft records no provider or model: the empty strings
// are what tell the reviewer the proposal did not come from a model.
func (s *DataQualityService) queueRecommendedCheck(ctx context.Context, recommendation dataquality.Recommendation, target dataquality.ResolvedTarget, createdBy *int32, now time.Time) (*store.QualitySuggestionMessage, bool, error) {
	resourceID, err := common.RandomString(12)
	if err != nil {
		return nil, false, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to generate the suggestion id"))
	}
	suggestion, created, err := s.store.QueueQualitySuggestion(ctx, &store.QualitySuggestionMessage{
		ResourceID: resourceID,
		Kind:       storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT,
		Source:     storepb.QualitySuggestionSource_QUALITY_SUGGESTION_SOURCE_RECOMMENDATION,
		TargetGUID: target.GUID,
		TargetType: target.MetaType,
		Signature:  recommendation.Signature,
		Payload: &storepb.QualitySuggestionPayload{
			Payload: &storepb.QualitySuggestionPayload_Rule{Rule: &storepb.QualityRuleDraftConfig{
				Config:     recommendation.Config,
				Severity:   recommendation.Severity,
				Title:      recommendation.Title,
				Rationale:  recommendation.Rationale,
				Confidence: recommendation.Confidence,
			}},
		},
		Status:        storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING,
		PromptVersion: dataquality.RecommendVersion,
		CreatedBy:     createdBy,
		CreatedAt:     now,
	})
	if err != nil {
		return nil, false, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to queue a recommended check"))
	}
	return suggestion, created, nil
}
