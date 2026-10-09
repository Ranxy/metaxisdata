package v1

import "context"

// The languages the server writes prose in. Every user stores the language their
// UI is in on their profile (User.language) and the Explain SQL prompt is built
// from that tag, so the answer comes back in the language the user selected.
//
// A tag has to be listed here to be stored: prompt text is compiled in, not
// derived, so accepting a tag the server cannot honour would only produce an
// answer in the wrong language. The tags must match the locales the SPA ships
// (frontend/src/locales/index.ts).
const (
	// explainSectionCount is the number of ## sections an explanation is built
	// from. parseStructuredResponse and the prompt headings share it.
	explainSectionCount = 4
	// defaultLanguage is what an empty or unrecognized tag reads as: the SPA's
	// DEFAULT_LOCALE.
	defaultLanguage = "en-US"
)

// languagePrompt is one language's prompt text: the name the model is told to
// write in, and the four section headings. Only the "## " prefix of a heading is
// parsed back, so translating one cannot break the structured answer.
type languagePrompt struct {
	name     string
	headings [explainSectionCount]string
}

var languagePrompts = map[string]languagePrompt{
	"en-US": {
		name: "English",
		headings: [explainSectionCount]string{
			"Execution Logic",
			"Objects Involved",
			"Potential Problems",
			"Optimization Suggestions",
		},
	},
	"zh-CN": {
		name: "Simplified Chinese",
		headings: [explainSectionCount]string{
			"执行逻辑",
			"涉及对象",
			"潜在问题",
			"优化建议",
		},
	},
}

// normalizeLanguage returns the tag to prompt in: the requested one when the
// server ships prompt text for it, the default otherwise. Empty is a user who
// never chose a language, and a tag from a client the server does not know.
func normalizeLanguage(tag string) string {
	if _, ok := languagePrompts[tag]; ok {
		return tag
	}
	return defaultLanguage
}

// isSupportedLanguage reports whether tag names a language the server ships
// prompt text for. UpdateUser uses it to refuse a preference that could never be
// honoured instead of storing it and answering in the default language anyway.
func isSupportedLanguage(tag string) bool {
	_, ok := languagePrompts[tag]
	return ok
}

// currentUserLanguage returns the BCP-47 tag the caller stored on their profile.
// An unauthenticated caller has none, which normalizeLanguage reads as the
// default.
func currentUserLanguage(ctx context.Context) string {
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return ""
	}
	return user.Profile.GetLanguage()
}
