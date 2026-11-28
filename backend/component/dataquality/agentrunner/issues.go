package agentrunner

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/component/tools/quality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// maxAgentIssues bounds one issue-ledger read. The ledger is a current-problem view,
// not a history export; a target with more open issues than this needs a person, not a
// longer tool result.
const maxAgentIssues = 20

// agentIssueRow is one problem-ledger row as a wake reads it. It carries the issue id
// the model must report and the rule it belongs to; the detail stays behind
// inspect_check.
type agentIssueRow struct {
	Issue             string `json:"issue"`
	Rule              string `json:"rule,omitempty"`
	RuleTitle         string `json:"ruleTitle,omitempty"`
	Status            string `json:"status"`
	Severity          string `json:"severity"`
	OccurrenceCount   int32  `json:"occurrenceCount"`
	FirstSeenTime     string `json:"firstSeenTime,omitempty"`
	LastSeenTime      string `json:"lastSeenTime,omitempty"`
	LastSeenExecution string `json:"lastSeenExecution,omitempty"`
	MutedUntil        string `json:"mutedUntil,omitempty"`
}

// listIssues is the target session's problem-ledger read. A FAIL is a finding, not a
// work item; this is the surface that proves it and gives the model the issue id to
// report instead of asking for a case.
func (e *Engine) listIssues(ctx context.Context, target quality.AgentSession, arguments string) (string, error) {
	if strings.TrimSpace(target.TargetGUID) == "" {
		return "", errors.New("a target is required")
	}
	var input struct {
		Status string `json:"status"`
		Limit  int    `json:"limit"`
	}
	if strings.TrimSpace(arguments) != "" {
		if err := json.Unmarshal([]byte(arguments), &input); err != nil {
			return "", errors.Wrap(err, "invalid list_issues arguments")
		}
	}
	limit := input.Limit
	if limit <= 0 || limit > maxAgentIssues {
		limit = 10
	}
	find := &store.FindQualityIssueMessage{TargetGUID: &target.TargetGUID, Limit: &limit}
	if err := applyIssueStatus(find, input.Status); err != nil {
		return "", err
	}
	issues, err := e.store.ListQualityIssues(ctx, find)
	if err != nil {
		return "", errors.Wrap(err, "failed to read the target's issues")
	}
	rules, err := e.store.ListQualityRules(ctx, &store.FindQualityRuleMessage{TargetGUID: &target.TargetGUID})
	if err != nil {
		return "", errors.Wrap(err, "failed to read the target's rules")
	}
	return marshalJSON(map[string]any{
		"target": target.TargetGUID,
		"issues": agentIssueRows(issues, rules),
	})
}

// applyIssueStatus maps the model's status word onto the ledger's own definition of
// open: an acknowledged issue is still an unresolved problem, so OPEN is the
// "not RESOLVED" predicate rather than a single enum value.
func applyIssueStatus(find *store.FindQualityIssueMessage, value string) error {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "", "OPEN":
		find.OpenOnly = true
	case "ACKNOWLEDGED":
		find.Statuses = []storepb.QualityIssueStatus{storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_ACKNOWLEDGED}
	case "RESOLVED":
		find.Statuses = []storepb.QualityIssueStatus{storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_RESOLVED}
	case "ALL":
		// No status predicate: the caller asked for the whole ledger.
	default:
		return errors.Errorf("unknown issue status %q: use OPEN, ACKNOWLEDGED, RESOLVED or ALL", value)
	}
	return nil
}

// agentIssueRows projects a bounded page. The rule map is built once so a page of
// issues does not query the registry per row.
func agentIssueRows(issues []*store.QualityIssueMessage, rules []*store.QualityRuleMessage) []agentIssueRow {
	byID := make(map[int64]*store.QualityRuleMessage, len(rules))
	for _, rule := range rules {
		if rule != nil {
			byID[rule.ID] = rule
		}
	}
	rows := make([]agentIssueRow, 0, len(issues))
	for _, issue := range issues {
		if issue == nil {
			continue
		}
		row := agentIssueRow{
			Issue:           "qualityIssues/" + issue.ResourceID,
			Status:          strings.TrimPrefix(issue.Status.String(), "QUALITY_ISSUE_STATUS_"),
			Severity:        strings.TrimPrefix(issue.Severity.String(), "QUALITY_SEVERITY_"),
			OccurrenceCount: issue.OccurrenceCount,
			FirstSeenTime:   issue.FirstSeenAt.UTC().Format(time.RFC3339),
			LastSeenTime:    issue.LastSeenAt.UTC().Format(time.RFC3339),
		}
		if rule := byID[issue.RuleID]; rule != nil {
			row.Rule = "qualityRules/" + rule.ResourceID
			row.RuleTitle = rule.Title
		}
		if issue.LastSeenExecutionID != nil {
			row.LastSeenExecution = "qualityExecutions/" + strconv.FormatInt(*issue.LastSeenExecutionID, 10)
		}
		if issue.MutedUntil != nil {
			row.MutedUntil = issue.MutedUntil.UTC().Format(time.RFC3339)
		}
		rows = append(rows, row)
	}
	return rows
}
