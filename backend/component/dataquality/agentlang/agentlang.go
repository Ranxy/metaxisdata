// Package agentlang renders the server-side prose the quality agent stores for its
// owner (escalation titles/questions, the deterministic scope summary). The report
// body has its own renderer; these helpers keep the strings next to the language
// rule and out of the runners.
package agentlang

import (
	"fmt"
	"strings"

	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	English = "en-US"
	Chinese = "zh-CN"
)

// Supported reports whether a stored agent language is one this product can render
// both model prose and server prose in. Empty follows the owner and is accepted.
func Supported(language string) bool {
	language = strings.TrimSpace(language)
	return language == "" || language == English || language == Chinese
}

// Normalize maps a tag to the product's two supported languages. An empty or
// unknown tag reads as English, the same fallback the Explain SQL prompts use.
func Normalize(language string) string {
	if IsChinese(language) {
		return Chinese
	}
	return English
}

// Name is the human-readable label injected into the model prompt.
func Name(language string) string {
	if IsChinese(language) {
		return "Simplified Chinese (zh-CN)"
	}
	return "English (en-US)"
}

// Of reads the language a user chose for server-side prose. An empty profile or an
// unknown tag reads as English, the same fallback the Explain SQL prompts use.
func Of(user *store.UserMessage) string {
	if user == nil || user.Profile == nil {
		return English
	}
	if strings.HasPrefix(strings.ToLower(user.Profile.GetLanguage()), "zh") {
		return Chinese
	}
	return English
}

// IsChinese reports whether the tag is a Chinese locale.
func IsChinese(lang string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(lang)), "zh")
}

// ForAgent is the one language decision for an agent: its explicit setting wins,
// then the owner's profile language, then English. Model prose and server prose
// both use this so an agent never mixes the two.
func ForAgent(agent *store.QualityAgentMessage, owner *store.UserMessage) string {
	if agent != nil && strings.TrimSpace(agent.Language) != "" {
		return Normalize(agent.Language)
	}
	return Of(owner)
}

// AttentionTitle is the localized headline of one owner escalation.
func AttentionTitle(lang string, kind store.QualityAgentAttentionKind) string {
	zh := IsChinese(lang)
	switch kind {
	case store.QualityAgentAttentionKindProposalPending:
		if zh {
			return "有检查草案等待审核"
		}
		return "Check drafts are waiting for review"
	case store.QualityAgentAttentionKindReportAnomaly:
		if zh {
			return "巡检报告发现异常"
		}
		return "A patrol report found anomalies"
	default:
		if zh {
			return "Agent 需要你处理一个目标会话"
		}
		return "The agent needs help with a target session"
	}
}

// BlockedQuestion is the escalation body for a blocked target session. The reason
// comes from the engine/model and is shown as-is; only the framing is localized.
func BlockedQuestion(lang, reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		if IsChinese(lang) {
			return "会话已阻塞，需要你的决定才能继续。"
		}
		return "The session is blocked and needs your decision to continue."
	}
	if IsChinese(lang) {
		return "会话已阻塞：" + reason
	}
	return "The session is blocked: " + reason
}

// CaseRevisionTitle is the localized headline of a proposal that asks for one concrete value on
// one existing check. It is a different thing from a queue of drafts, though both are pending
// decisions: here the check exists, and one of its values is the owner's to set.
func CaseRevisionTitle(lang string) string {
	if IsChinese(lang) {
		return "有一条检查需要你改一个值"
	}
	return "One check needs a value only you can set"
}

// CaseRevision is one proposed change to one check, as the owner reads it. Every value is shown
// verbatim: these are config values, and a person compares the two strings the check would store.
type CaseRevision struct {
	// Check names the check, by title with the id as the fallback.
	Check string
	// Field is the dotted config path.
	Field string
	// From is the current value; empty when the field holds nothing yet.
	From string
	// To is the value the agent proposes.
	To string
	// Effect is the change's class (agentpolicy.Effect.Name): a criterion, the subject,
	// executability, state or removal.
	Effect string
	// Rationale is the agent's reason, already bounded by the caller.
	Rationale string
}

// CaseRevisionQuestion is the escalation body for a proposed change. It states what would change,
// from what to what, and that nothing changes until the owner acts: a proposal that reads like an
// applied change is worse than no proposal.
func CaseRevisionQuestion(lang string, revision CaseRevision) string {
	check := strings.TrimSpace(revision.Check)
	rationale := strings.TrimSpace(revision.Rationale)
	effect := RevisionEffectLabel(lang, revision.Effect)
	if IsChinese(lang) {
		question := "检查「" + check + "」需要你改一个值："
		if revision.From != "" {
			question += "把 " + revision.Field + " 从「" + revision.From + "」改为「" + revision.To + "」"
		} else {
			question += "把 " + revision.Field + " 设为「" + revision.To + "」"
		}
		question += "（" + effect + "类改动）。"
		if rationale != "" {
			question += "理由：" + rationale + "。"
		}
		question += "在你修改之前这条检查保持原样。"
		return question
	}
	question := "The check \"" + check + "\" needs a value only you can set: "
	if revision.From != "" {
		question += "change " + revision.Field + " from \"" + revision.From + "\" to \"" + revision.To + "\""
	} else {
		question += "set " + revision.Field + " to \"" + revision.To + "\""
	}
	question += " (" + effect + " change)."
	if rationale != "" {
		question += " Reason: " + rationale + "."
	}
	question += " Nothing changes until you make the edit."
	return question
}

// RevisionEffectLabel renders one change class for the owner.
func RevisionEffectLabel(lang, effect string) string {
	zh := IsChinese(lang)
	switch strings.ToUpper(strings.TrimSpace(effect)) {
	case "CRITERION":
		if zh {
			return "判定标准"
		}
		return "criterion"
	case "SUBJECT":
		if zh {
			return "判定对象"
		}
		return "subject"
	case "EXECUTABILITY":
		if zh {
			return "可执行性"
		}
		return "executability"
	case "STATE":
		if zh {
			return "状态"
		}
		return "state"
	case "REMOVAL":
		if zh {
			return "移除"
		}
		return "removal"
	default:
		return strings.ToLower(strings.TrimSpace(effect))
	}
}

// CaseEscalationQuestion is the escalation body for a work item the agent handed to a person. The
// reason comes from the model and is shown as-is; only the framing is localized.
func CaseEscalationQuestion(lang, check, reason string) string {
	check = strings.TrimSpace(check)
	reason = strings.TrimSpace(reason)
	if IsChinese(lang) {
		return "检查「" + check + "」：" + reason + " Agent 已在这个工作项上停下，需要你决定。"
	}
	return "Check \"" + check + "\": " + reason + " The agent has stopped on this work item and needs your decision."
}

// CaseFrozenQuestion is the escalation body for a work item the agent gave up on: it tried the
// budget of repairs and the check still fails the same way. The sentence has to say that the agent
// stopped and that a person now owns the decision, because a frozen item that reads like an
// in-progress repair is a silent dead end.
func CaseFrozenQuestion(lang, check string, attempts int32) string {
	check = strings.TrimSpace(check)
	attemptsText := fmt.Sprintf("%d", attempts)
	if IsChinese(lang) {
		return "检查「" + check + "」在 " + attemptsText + " 次修复后仍以同样原因失败，Agent 已停止尝试。需要你决定是修改这条检查，还是处理目标本身的问题。"
	}
	return "The check \"" + check + "\" still fails the same way after " + attemptsText + " repair attempt(s), so the agent stopped. " +
		"A person decides whether the check or the target needs to change."
}

// ProposalCounts is the outcome of one adoption pass for one target.
type ProposalCounts struct {
	Remaining int
	Adopted   int
	Skipped   int
	Failed    int
	// Failure is the most recent write failure, already bounded by the caller.
	Failure string
}

// ProposalQuestion names what is left in the owner's queue and why. It separates a
// policy skip from a write failure: the owner cannot fix a server error by deciding
// a draft.
func ProposalQuestion(lang string, counts ProposalCounts) string {
	failure := strings.TrimSpace(counts.Failure)
	if IsChinese(lang) {
		question := fmt.Sprintf("该目标还有 %d 条草案需要处理：本轮自动采纳 %d 条，按策略跳过 %d 条，写入失败 %d 条。",
			counts.Remaining, counts.Adopted, counts.Skipped, counts.Failed)
		if failure != "" {
			question += "最近一次失败：" + failure + "。"
		}
		question += "回复要采纳哪些草案（例如「采纳全部」），我会按你的决定创建检查；自动采纳仍遵守当前护栏，也可以到建议中心逐条决定。"
		return question
	}
	question := fmt.Sprintf("%d draft(s) for this target still need a decision: %d auto-adopted, %d skipped by policy, %d failed to write this pass.",
		counts.Remaining, counts.Adopted, counts.Skipped, counts.Failed)
	if failure != "" {
		question += " Last failure: " + failure + "."
	}
	question += " Reply with the drafts to adopt (for example \"adopt them all\") and I will create them as your decision; automatic adoption still follows the configured guardrails, or decide them one by one in the review queue."
	return question
}

// ReportQuestion names the anomalies in a generated report.
func ReportQuestion(lang string, failed, newIssues int32) string {
	if IsChinese(lang) {
		return fmt.Sprintf("本周期 %d 个检查失败、%d 个新问题打开。", failed, newIssues)
	}
	return fmt.Sprintf("%d check(s) failed and %d new issue(s) opened in this period.", failed, newIssues)
}

// ReportReady is the conversation note appended when a report is generated.
func ReportReady(lang, start, end string) string {
	if IsChinese(lang) {
		return fmt.Sprintf("巡检报告已生成（%s 至 %s），可在报告页查看。", start, end)
	}
	return fmt.Sprintf("Patrol report ready for %s to %s. Open the Reports tab to read it.", start, end)
}

// ScopeSummary is the deterministic conversation answer when no model is configured.
func ScopeSummary(lang string, total, pending, active, dormant, outOfScope, gone int) string {
	if IsChinese(lang) {
		lines := []string{
			fmt.Sprintf("范围：已发现 %d 个目标。", total),
			fmt.Sprintf("队列：%d 待准入、%d 活跃、%d 休眠、%d 出范围、%d 已消失。", pending, active, dormant, outOfScope, gone),
		}
		if pending > 0 {
			lines = append(lines, "Agent 仍在准入待处理目标。")
		}
		if dormant > 0 {
			lines = append(lines, "休眠目标在等待复查或人工决定。")
		}
		lines = append(lines, "当前没有配置模型，这是确定性范围汇总；配置 LLM Provider 后可以继续提问。")
		return strings.Join(lines, "\n")
	}
	lines := []string{
		fmt.Sprintf("Scope: %d target(s) discovered.", total),
		fmt.Sprintf("Queue: %d pending, %d active, %d dormant, %d out of scope, %d gone.", pending, active, dormant, outOfScope, gone),
	}
	if pending > 0 {
		lines = append(lines, "The agent is still admitting pending targets.")
	}
	if dormant > 0 {
		lines = append(lines, "Dormant targets are waiting for their review or for a person's decision.")
	}
	lines = append(lines, "A model is not configured, so this is the deterministic summary; configure an LLM provider to ask follow-up questions.")
	return strings.Join(lines, "\n")
}

// CaseListQuestion is the body of one target's decision card. The newest hand-off is written as the
// model wrote it, and the work items still waiting on the same target are listed under it: one card
// answers "what does this target still owe me" instead of one card per check, which is what made a
// single empty table look like nine separate decisions.
func CaseListQuestion(lang, newest string, alsoWaiting []string) string {
	newest = strings.TrimSpace(newest)
	if len(alsoWaiting) == 0 {
		return newest
	}
	heading := "Also waiting on this target:"
	if IsChinese(lang) {
		heading = "同一目标上还在等你决定："
	}
	lines := make([]string, 0, len(alsoWaiting)+3)
	lines = append(lines, newest, "", heading)
	for _, item := range alsoWaiting {
		if item = strings.TrimSpace(item); item != "" {
			lines = append(lines, "- "+item)
		}
	}
	return strings.Join(lines, "\n")
}
