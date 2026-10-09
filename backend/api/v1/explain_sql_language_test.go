package v1

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func TestNormalizeLanguage(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		tag  string
		want string
	}{
		{name: "a shipped locale is kept", tag: "zh-CN", want: "zh-CN"},
		{name: "the default is kept", tag: "en-US", want: "en-US"},
		{name: "an unset preference reads as the default", tag: "", want: defaultLanguage},
		{name: "a locale the server cannot prompt in reads as the default", tag: "fr-FR", want: defaultLanguage},
		// The tags match the SPA's AppLocale exactly; a case variant is not one
		// of them, and matching it loosely would only hide a client bug.
		{name: "a case variant is not a supported tag", tag: "zh-cn", want: defaultLanguage},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.want, normalizeLanguage(test.tag))
		})
	}
}

func TestIsSupportedLanguage(t *testing.T) {
	t.Parallel()

	require.True(t, isSupportedLanguage("en-US"))
	require.True(t, isSupportedLanguage("zh-CN"))
	// Empty clears the preference rather than selecting a language, so it is
	// not accepted as a value the server can prompt in.
	require.False(t, isSupportedLanguage(""))
	require.False(t, isSupportedLanguage("fr-FR"))
}

func TestCurrentUserLanguage(t *testing.T) {
	t.Parallel()

	require.Empty(t, currentUserLanguage(context.Background()), "an anonymous caller has no preference")

	noProfile := context.WithValue(context.Background(), common.UserContextKey, &store.UserMessage{ID: 7})
	require.Empty(t, currentUserLanguage(noProfile), "an account whose profile was never written reads as unset")

	configured := context.WithValue(context.Background(), common.UserContextKey, &store.UserMessage{
		ID:      7,
		Profile: &storepb.UserProfile{Language: "zh-CN"},
	})
	require.Equal(t, "zh-CN", currentUserLanguage(configured))
}

func TestBuildSystemPromptWritesInTheRequestedLanguage(t *testing.T) {
	t.Parallel()

	zh := buildSystemPrompt("zh-CN", storepb.MetaType_VIEW, nil)
	require.Contains(t, zh, "## 执行逻辑")
	require.Contains(t, zh, "## 优化建议")
	require.Contains(t, zh, "Simplified Chinese")
	require.NotContains(t, zh, "Execution Logic")
	require.Equal(t, explainSectionCount, strings.Count(zh, "\n## "), "the four sections parse back by their ## prefix")

	en := buildSystemPrompt("en-US", storepb.MetaType_VIEW, nil)
	require.Contains(t, en, "## Execution Logic")
	require.Contains(t, en, "## Optimization Suggestions")
	require.NotContains(t, en, "执行逻辑")
	require.Equal(t, explainSectionCount, strings.Count(en, "\n## "))

	// A tag the server cannot prompt in falls back to the default, not to
	// whichever language happens to be listed first.
	require.Equal(t, en, buildSystemPrompt("fr-FR", storepb.MetaType_VIEW, nil))
}

func TestExplainSQLCacheKeySeparatesLanguages(t *testing.T) {
	t.Parallel()

	english := explainSQLCacheKey("sql:abc", "instance-1", "profiles/1", "gpt", "en-US")
	require.Contains(t, english, "lang:en-US")
	require.Equal(t, english, explainSQLCacheKey("sql:abc", "instance-1", "profiles/1", "gpt", "en-US"))
	// One reader's explanation is not the other's: same SQL, different language.
	require.NotEqual(t, english, explainSQLCacheKey("sql:abc", "instance-1", "profiles/1", "gpt", "zh-CN"))
	require.NotEqual(t, english, explainSQLCacheKey("sql:abc", "instance-1", "profiles/1", "other", "en-US"))
}

func TestProfileWithLanguageCarriesTheRestOfTheProfile(t *testing.T) {
	t.Parallel()

	lastLogin := timestamppb.New(time.Now().Truncate(time.Second))
	changedAt := timestamppb.New(time.Now().Add(-time.Hour).Truncate(time.Second))
	profile := &storepb.UserProfile{
		LastLoginTime:          lastLogin,
		LastChangePasswordTime: changedAt,
		Language:               "zh-CN",
	}

	updated := profileWithLanguage(profile, "en-US")
	require.Equal(t, "en-US", updated.Language)
	// The store writes the whole column back, so dropping the password change
	// time here would retire sessions that are still valid.
	require.Equal(t, lastLogin, updated.LastLoginTime)
	require.Equal(t, changedAt, updated.LastChangePasswordTime)
	// The profile the caller read may be the store's cached entry.
	require.Equal(t, "zh-CN", profile.Language)

	fresh := profileWithLanguage(nil, "zh-CN")
	require.Equal(t, "zh-CN", fresh.Language)
	require.Nil(t, fresh.LastLoginTime)
}
