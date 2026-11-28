package agentlang

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// TestLanguageMapping pins the two tags the frontend offers and the server's
// fallback: an unknown tag reads as English, the same fallback the Explain SQL
// prompts use.
func TestLanguageMapping(t *testing.T) {
	t.Parallel()

	require.True(t, Supported(""))
	require.True(t, Supported("en-US"))
	require.True(t, Supported("zh-CN"))
	require.True(t, Supported(" zh-CN "))
	require.False(t, Supported("fr-FR"))
	require.Equal(t, Chinese, Normalize("zh-TW"))
	require.Equal(t, English, Normalize("fr-FR"))
	// The prompt names are part of the model contract, not display copy.
	require.Equal(t, "Simplified Chinese (zh-CN)", Name("zh-CN"))
	require.Equal(t, "English (en-US)", Name("en-US"))
}

// TestForAgentPrefersTheExplicitAgentLanguage pins the one language decision per
// agent: the agent's explicit setting wins, then the owner's profile language,
// then English.
func TestForAgentPrefersTheExplicitAgentLanguage(t *testing.T) {
	t.Parallel()

	chineseOwner := &store.UserMessage{Profile: &storepb.UserProfile{Language: "zh-CN"}}
	englishOwner := &store.UserMessage{Profile: &storepb.UserProfile{Language: "en-US"}}

	tests := []struct {
		name  string
		agent *store.QualityAgentMessage
		owner *store.UserMessage
		want  string
	}{
		{"explicit agent language wins over the owner", &store.QualityAgentMessage{Language: "en-US"}, chineseOwner, English},
		{"explicit Chinese agent language wins over the owner", &store.QualityAgentMessage{Language: "zh-CN"}, englishOwner, Chinese},
		{"agent language is trimmed", &store.QualityAgentMessage{Language: "  zh-CN  "}, nil, Chinese},
		{"empty agent language follows the owner", &store.QualityAgentMessage{}, chineseOwner, Chinese},
		{"empty agent language follows an English owner", &store.QualityAgentMessage{}, englishOwner, English},
		{"blank agent language follows the owner", &store.QualityAgentMessage{Language: "  "}, chineseOwner, Chinese},
		{"nil agent follows the owner", nil, chineseOwner, Chinese},
		{"nil agent and no owner read as English", nil, nil, English},
		{"owner without a profile reads as English", &store.QualityAgentMessage{}, &store.UserMessage{}, English},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, test.want, ForAgent(test.agent, test.owner))
		})
	}
}

// TestServerProseLocalizesAndCarriesTheCallerValues pins the escalation prose: the
// two languages never render the same text, and the model reason, the write
// failure, the counts and the period are shown inside it as-is.
func TestServerProseLocalizesAndCarriesTheCallerValues(t *testing.T) {
	t.Parallel()

	counts := ProposalCounts{Remaining: 31, Adopted: 12, Skipped: 13, Failed: 14, Failure: "boom-xyz"}
	revision := CaseRevision{
		Check: "performed_at must be recent", Field: "range.min", From: "now - 1 day",
		To: "2024-01-01T00:00:00Z", Effect: "CRITERION", Rationale: "the engine refused this bound",
	}
	tests := []struct {
		name   string
		render func(language string) string
		value  string
	}{
		{"blocked question", func(language string) string { return BlockedQuestion(language, "model failed") }, "model failed"},
		{"case revision question", func(language string) string { return CaseRevisionQuestion(language, revision) }, "2024-01-01T00:00:00Z"},
		{"case frozen question", func(language string) string { return CaseFrozenQuestion(language, "performed_at must be recent", 2) }, "performed_at must be recent"},
		{"proposal question", func(language string) string { return ProposalQuestion(language, counts) }, "boom-xyz"},
		{"report question", func(language string) string { return ReportQuestion(language, 1234, 5678) }, "1234"},
		{"report ready", func(language string) string { return ReportReady(language, "2026-11-01", "2026-11-07") }, "2026-11-01"},
		{"scope summary", func(language string) string {
			return ScopeSummary(language, 101, 102, 103, 104, 105, 106)
		}, "101"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			english, chinese := test.render(English), test.render(Chinese)
			require.NotEqual(t, english, chinese, "the two languages must not render the same prose")
			require.Contains(t, english, test.value)
			require.Contains(t, chinese, test.value)
		})
	}

	// A proposal that reads like an applied change is worse than no proposal: the body has to say
	// that nothing has changed yet, in both languages.
	for _, language := range []string{English, Chinese} {
		question := CaseRevisionQuestion(language, revision)
		require.Contains(t, question, "range.min")
		require.Contains(t, question, "now - 1 day")
		require.Contains(t, question, RevisionEffectLabel(language, "CRITERION"))
	}
	require.Contains(t, CaseRevisionQuestion(English, revision), "Nothing changes until")
	require.Contains(t, CaseRevisionQuestion(Chinese, revision), "保持原样")

	// A field that holds nothing yet reads as "set", not as "change from an empty value".
	for _, language := range []string{English, Chinese} {
		require.NotContains(t, CaseRevisionQuestion(language, CaseRevision{Check: "c", Field: "f", To: "v"}), revision.From)
	}

	// Titles carry no caller value; the language alone decides them.
	require.NotEqual(t,
		ApprovalTitle(English),
		ApprovalTitle(Chinese))
}

// One target's decision card lists what still waits there: the newest hand-off, then the others, so
// a person decides about a target instead of opening one card per silenced check.
func TestCaseListQuestionAggregatesOneTarget(t *testing.T) {
	t.Parallel()

	newest := "检查「a」：引擎拒绝了这个定义。"
	require.Equal(t, newest, CaseListQuestion("zh-CN", newest, nil),
		"a single hand-off is not given a heading it does not need")

	english := CaseListQuestion("en-US", "Check \"a\": the engine refused it.", []string{"check \"b\": gave up"})
	require.Contains(t, english, "Check \"a\"")
	require.Contains(t, english, "Also waiting on this target:")
	require.Contains(t, english, "- check \"b\": gave up")

	chinese := CaseListQuestion("zh-CN", newest, []string{"check \"b\": gave up", "  "})
	require.Contains(t, chinese, "同一目标上还在等你决定：")
	require.Contains(t, chinese, "- check \"b\": gave up")
	require.NotContains(t, chinese, "-  \n", "a blank summary contributes no bullet")
}
