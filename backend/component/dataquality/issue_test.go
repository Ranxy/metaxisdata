package dataquality

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

func qualityStatusPtr(status storepb.QualityStatus) *storepb.QualityStatus {
	return &status
}

// The problem-transition table is the whole reason the ledger does not flap: a
// rule that keeps failing must not open an issue every run, and an execution
// failure must not resolve the problem it failed to judge.
func TestPlanIssueTransition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		previous     *storepb.QualityStatus
		current      storepb.QualityStatus
		hasOpenIssue bool
		want         IssueAction
	}{
		{
			name:     "first failure opens an issue",
			previous: nil,
			current:  storepb.QualityStatus_QUALITY_STATUS_FAIL,
			want:     IssueActionOpen,
		},
		{
			name:     "a passing rule opens nothing",
			previous: qualityStatusPtr(storepb.QualityStatus_QUALITY_STATUS_FAIL),
			current:  storepb.QualityStatus_QUALITY_STATUS_PASS,
			want:     IssueActionNone,
		},
		{
			name:         "another failure touches the open issue",
			previous:     qualityStatusPtr(storepb.QualityStatus_QUALITY_STATUS_FAIL),
			current:      storepb.QualityStatus_QUALITY_STATUS_FAIL,
			hasOpenIssue: true,
			want:         IssueActionTouch,
		},
		{
			name:         "a failure after another status touches the open issue",
			previous:     qualityStatusPtr(storepb.QualityStatus_QUALITY_STATUS_WARN),
			current:      storepb.QualityStatus_QUALITY_STATUS_FAIL,
			hasOpenIssue: true,
			want:         IssueActionTouch,
		},
		{
			name:         "recovery resolves the issue",
			previous:     qualityStatusPtr(storepb.QualityStatus_QUALITY_STATUS_FAIL),
			current:      storepb.QualityStatus_QUALITY_STATUS_PASS,
			hasOpenIssue: true,
			want:         IssueActionResolve,
		},
		{
			name:         "a pass after an execution error does not resolve",
			previous:     qualityStatusPtr(storepb.QualityStatus_QUALITY_STATUS_ERROR),
			current:      storepb.QualityStatus_QUALITY_STATUS_PASS,
			hasOpenIssue: true,
			want:         IssueActionNone,
		},
		{
			name:         "fail to warn keeps the issue alive",
			previous:     qualityStatusPtr(storepb.QualityStatus_QUALITY_STATUS_FAIL),
			current:      storepb.QualityStatus_QUALITY_STATUS_WARN,
			hasOpenIssue: true,
			want:         IssueActionTouch,
		},
		{
			name:         "fail to not evaluated keeps the issue alive",
			previous:     qualityStatusPtr(storepb.QualityStatus_QUALITY_STATUS_FAIL),
			current:      storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED,
			hasOpenIssue: true,
			want:         IssueActionTouch,
		},
		{
			name:         "fail to skipped keeps the issue alive",
			previous:     qualityStatusPtr(storepb.QualityStatus_QUALITY_STATUS_FAIL),
			current:      storepb.QualityStatus_QUALITY_STATUS_SKIPPED,
			hasOpenIssue: true,
			want:         IssueActionTouch,
		},
		{
			name:         "a warn without an issue does nothing",
			previous:     qualityStatusPtr(storepb.QualityStatus_QUALITY_STATUS_FAIL),
			current:      storepb.QualityStatus_QUALITY_STATUS_WARN,
			hasOpenIssue: false,
			want:         IssueActionNone,
		},
		{
			name:         "an execution error changes nothing",
			previous:     qualityStatusPtr(storepb.QualityStatus_QUALITY_STATUS_FAIL),
			current:      storepb.QualityStatus_QUALITY_STATUS_ERROR,
			hasOpenIssue: true,
			want:         IssueActionNone,
		},
		{
			name:         "an execution error does not open an issue",
			previous:     nil,
			current:      storepb.QualityStatus_QUALITY_STATUS_ERROR,
			hasOpenIssue: false,
			want:         IssueActionNone,
		},
		{
			name:         "unspecified says nothing",
			previous:     qualityStatusPtr(storepb.QualityStatus_QUALITY_STATUS_FAIL),
			current:      storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED,
			hasOpenIssue: true,
			want:         IssueActionNone,
		},
		{
			name:         "pass with an issue that did not come from a failure is left alone",
			previous:     nil,
			current:      storepb.QualityStatus_QUALITY_STATUS_PASS,
			hasOpenIssue: true,
			want:         IssueActionNone,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.want, PlanIssueTransition(test.previous, test.current, test.hasOpenIssue))
		})
	}
}
