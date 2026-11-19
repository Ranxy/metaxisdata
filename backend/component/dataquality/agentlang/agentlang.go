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
		return question
	}
	question := fmt.Sprintf("%d draft(s) for this target still need a decision: %d auto-adopted, %d skipped by policy, %d failed to write this pass.",
		counts.Remaining, counts.Adopted, counts.Skipped, counts.Failed)
	if failure != "" {
		question += " Last failure: " + failure + "."
	}
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
