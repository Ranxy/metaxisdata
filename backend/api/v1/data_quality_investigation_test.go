package v1

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

// A concluded investigation must carry a verdict and at least one evidence
// reference; a failed one must say why. An investigation without evidence is the
// thing the design calls "not an investigation".
func TestValidateQualityInvestigationRequest(t *testing.T) {
	t.Parallel()

	base := func() *v1pb.CreateQualityInvestigationRequest {
		return &v1pb.CreateQualityInvestigationRequest{
			TargetGuid:       "inst;db;public;orders",
			MetaType:         v1pb.MetaType_TABLE,
			Trigger:          v1pb.QualityInvestigationTrigger_QUALITY_INVESTIGATION_TRIGGER_EXECUTION,
			TriggerReference: "qualityExecutions/7",
			Verdict:          v1pb.QualityInvestigationVerdict_QUALITY_INVESTIGATION_VERDICT_ROOT_CAUSE,
			Evidence: []*v1pb.QualityInvestigationEvidence{{
				Kind: "EXECUTION", Reference: "qualityExecutions/7",
			}},
		}
	}

	input, err := validateQualityInvestigationRequest(base(), "inst;db;public;orders", storepb.MetaType_TABLE, 7)
	require.NoError(t, err)
	require.Equal(t, storepb.QualityInvestigationStatus_QUALITY_INVESTIGATION_STATUS_CONCLUDED, input.Status)
	require.Len(t, input.Evidence.GetEvidence(), 1)
	require.True(t, len(input.RequestFingerprint) > 0, "an idempotency key is computed")

	noEvidence := base()
	noEvidence.Evidence = nil
	_, err = validateQualityInvestigationRequest(noEvidence, "inst;db;public;orders", storepb.MetaType_TABLE, 7)
	require.Error(t, err, "a concluded investigation needs evidence")

	noVerdict := base()
	noVerdict.Verdict = v1pb.QualityInvestigationVerdict_QUALITY_INVESTIGATION_VERDICT_UNSPECIFIED
	_, err = validateQualityInvestigationRequest(noVerdict, "inst;db;public;orders", storepb.MetaType_TABLE, 7)
	require.Error(t, err, "a concluded investigation needs a verdict")

	failed := &v1pb.CreateQualityInvestigationRequest{
		TargetGuid:       "inst;db;public;orders",
		MetaType:         v1pb.MetaType_TABLE,
		Trigger:          v1pb.QualityInvestigationTrigger_QUALITY_INVESTIGATION_TRIGGER_MANUAL,
		TriggerReference: "operator:integration",
		Status:           v1pb.QualityInvestigationStatus_QUALITY_INVESTIGATION_STATUS_FAILED,
	}
	_, err = validateQualityInvestigationRequest(failed, "inst;db;public;orders", storepb.MetaType_TABLE, 7)
	require.Error(t, err, "a failed investigation needs a failure reason")
	failed.FailureReason = "the model call timed out"
	input, err = validateQualityInvestigationRequest(failed, "inst;db;public;orders", storepb.MetaType_TABLE, 7)
	require.NoError(t, err)
	require.Equal(t, storepb.QualityInvestigationStatus_QUALITY_INVESTIGATION_STATUS_FAILED, input.Status)

	badHypothesis := base()
	badHypothesis.Hypotheses = []*v1pb.QualityInvestigationHypothesis{{Statement: "upstream is late", Status: "MAYBE"}}
	_, err = validateQualityInvestigationRequest(badHypothesis, "inst;db;public;orders", storepb.MetaType_TABLE, 7)
	require.Error(t, err)

	emptyEvidence := base()
	emptyEvidence.Evidence = []*v1pb.QualityInvestigationEvidence{{Kind: "EXECUTION"}}
	_, err = validateQualityInvestigationRequest(emptyEvidence, "inst;db;public;orders", storepb.MetaType_TABLE, 7)
	require.Error(t, err)

	noTrigger := base()
	noTrigger.Trigger = v1pb.QualityInvestigationTrigger_QUALITY_INVESTIGATION_TRIGGER_UNSPECIFIED
	_, err = validateQualityInvestigationRequest(noTrigger, "inst;db;public;orders", storepb.MetaType_TABLE, 7)
	require.Error(t, err)

	// A concluded investigation cannot carry a failure reason, and the fingerprint is
	// bounded so a caller cannot use it as an unbounded payload.
	withFailure := base()
	withFailure.FailureReason = "not actually failed"
	_, err = validateQualityInvestigationRequest(withFailure, "inst;db;public;orders", storepb.MetaType_TABLE, 7)
	require.Error(t, err)
	tooLongFingerprint := base()
	tooLongFingerprint.RequestFingerprint = strings.Repeat("x", maxInvestigationFingerprint+1)
	_, err = validateQualityInvestigationRequest(tooLongFingerprint, "inst;db;public;orders", storepb.MetaType_TABLE, 7)
	require.Error(t, err)
}

// The same work product hashes identically, so a retried agent run does not record a
// second investigation; a different summary is a different investigation.
func TestQualityInvestigationFingerprint(t *testing.T) {
	t.Parallel()

	evidence := &storepb.QualityInvestigationEvidenceList{Evidence: []*storepb.QualityInvestigationEvidence{{
		Kind: "EXECUTION", Reference: "qualityExecutions/7",
	}}}
	hypotheses := &storepb.QualityInvestigationHypothesisList{}
	refs := &storepb.QualityInvestigationActionRefs{}
	first := qualityInvestigationFingerprint("inst;db;public;orders", storepb.MetaType_TABLE,
		v1pb.QualityInvestigationTrigger_QUALITY_INVESTIGATION_TRIGGER_EXECUTION, "qualityExecutions/7",
		storepb.QualityInvestigationStatus_QUALITY_INVESTIGATION_STATUS_CONCLUDED,
		storepb.QualityInvestigationVerdict_QUALITY_INVESTIGATION_VERDICT_ROOT_CAUSE, "row count drop", hypotheses, evidence, refs)
	require.Equal(t, first, qualityInvestigationFingerprint("inst;db;public;orders", storepb.MetaType_TABLE,
		v1pb.QualityInvestigationTrigger_QUALITY_INVESTIGATION_TRIGGER_EXECUTION, "qualityExecutions/7",
		storepb.QualityInvestigationStatus_QUALITY_INVESTIGATION_STATUS_CONCLUDED,
		storepb.QualityInvestigationVerdict_QUALITY_INVESTIGATION_VERDICT_ROOT_CAUSE, "row count drop", hypotheses, evidence, refs))
	require.NotEqual(t, first, qualityInvestigationFingerprint("inst;db;public;orders", storepb.MetaType_TABLE,
		v1pb.QualityInvestigationTrigger_QUALITY_INVESTIGATION_TRIGGER_EXECUTION, "qualityExecutions/7",
		storepb.QualityInvestigationStatus_QUALITY_INVESTIGATION_STATUS_CONCLUDED,
		storepb.QualityInvestigationVerdict_QUALITY_INVESTIGATION_VERDICT_ROOT_CAUSE, "a different summary", hypotheses, evidence, refs))
	// The identity parts are part of the key: another target, trigger or evidence set is
	// a different investigation.
	require.NotEqual(t, first, qualityInvestigationFingerprint("inst;db;public;customers", storepb.MetaType_TABLE,
		v1pb.QualityInvestigationTrigger_QUALITY_INVESTIGATION_TRIGGER_EXECUTION, "qualityExecutions/7",
		storepb.QualityInvestigationStatus_QUALITY_INVESTIGATION_STATUS_CONCLUDED,
		storepb.QualityInvestigationVerdict_QUALITY_INVESTIGATION_VERDICT_ROOT_CAUSE, "row count drop", hypotheses, evidence, refs))
	require.NotEqual(t, first, qualityInvestigationFingerprint("inst;db;public;orders", storepb.MetaType_TABLE,
		v1pb.QualityInvestigationTrigger_QUALITY_INVESTIGATION_TRIGGER_ISSUE, "qualityExecutions/7",
		storepb.QualityInvestigationStatus_QUALITY_INVESTIGATION_STATUS_CONCLUDED,
		storepb.QualityInvestigationVerdict_QUALITY_INVESTIGATION_VERDICT_ROOT_CAUSE, "row count drop", hypotheses, evidence, refs))
	otherEvidence := &storepb.QualityInvestigationEvidenceList{Evidence: []*storepb.QualityInvestigationEvidence{{
		Kind: "ISSUE", Reference: "qualityIssues/3",
	}}}
	require.NotEqual(t, first, qualityInvestigationFingerprint("inst;db;public;orders", storepb.MetaType_TABLE,
		v1pb.QualityInvestigationTrigger_QUALITY_INVESTIGATION_TRIGGER_EXECUTION, "qualityExecutions/7",
		storepb.QualityInvestigationStatus_QUALITY_INVESTIGATION_STATUS_CONCLUDED,
		storepb.QualityInvestigationVerdict_QUALITY_INVESTIGATION_VERDICT_ROOT_CAUSE, "row count drop", hypotheses, otherEvidence, refs))
	otherRefs := &storepb.QualityInvestigationActionRefs{Refs: []string{"workspaces/ws1/qualityChangeRequests/cr1"}}
	require.NotEqual(t, first, qualityInvestigationFingerprint("inst;db;public;orders", storepb.MetaType_TABLE,
		v1pb.QualityInvestigationTrigger_QUALITY_INVESTIGATION_TRIGGER_EXECUTION, "qualityExecutions/7",
		storepb.QualityInvestigationStatus_QUALITY_INVESTIGATION_STATUS_CONCLUDED,
		storepb.QualityInvestigationVerdict_QUALITY_INVESTIGATION_VERDICT_ROOT_CAUSE, "row count drop", hypotheses, evidence, otherRefs))
}
