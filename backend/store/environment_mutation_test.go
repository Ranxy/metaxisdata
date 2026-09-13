package store

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

func TestSlugifyEnvironmentID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		title string
		want  string
	}{
		{title: "Prod", want: "prod"},
		{title: "  Staging  ", want: "staging"},
		{title: "Dev/Test", want: "dev-test"},
		{title: "My  Env!!", want: "my-env"},
		{title: "Team_1", want: "team-1"},
		{title: "预发布", want: ""},
		{title: "prod-", want: "prod"},
	}

	for _, test := range tests {
		require.Equal(t, test.want, slugifyEnvironmentID(test.title), "title %q", test.title)
	}
}

func TestUniqueEnvironmentIDFallsBackForNonASCII(t *testing.T) {
	t.Parallel()

	setting := &storepb.EnvironmentSetting{}
	require.Equal(t, "env", uniqueEnvironmentID(setting, "预发布"))

	env, err := applyEnvironmentCreate(setting, "生产", "")
	require.NoError(t, err)
	require.Equal(t, "env", env.Id)

	// A second non-ASCII title must not reuse the first id.
	env, err = applyEnvironmentCreate(setting, "测试", "")
	require.NoError(t, err)
	require.Equal(t, "env-1", env.Id)
}

func TestUniqueEnvironmentIDSuffixesCollisions(t *testing.T) {
	t.Parallel()

	setting := &storepb.EnvironmentSetting{}
	first, err := applyEnvironmentCreate(setting, "Prod", "")
	require.NoError(t, err)
	require.Equal(t, "prod", first.Id)

	// "pro-d" slugs differently, so it keeps its own id.
	second, err := applyEnvironmentCreate(setting, "Pro-d", "")
	require.NoError(t, err)
	require.Equal(t, "pro-d", second.Id)

	// A title whose slug collides with "prod" but is not an exact title match
	// gets a suffixed id instead of silently merging into the first entry.
	third, err := applyEnvironmentCreate(setting, "Prod!", "")
	require.NoError(t, err)
	require.Equal(t, "prod-1", third.Id)
}

func TestApplyEnvironmentCreateRejectsDuplicatesAndEmptyTitle(t *testing.T) {
	t.Parallel()

	setting := &storepb.EnvironmentSetting{}
	_, err := applyEnvironmentCreate(setting, "Prod", "")
	require.NoError(t, err)

	_, err = applyEnvironmentCreate(setting, "  prod ", "")
	require.Error(t, err)
	require.Equal(t, common.Conflict, common.ErrorCode(err))

	_, err = applyEnvironmentCreate(setting, "   ", "")
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))
}

func TestApplyEnvironmentCreateAssignsPaletteColor(t *testing.T) {
	t.Parallel()

	setting := &storepb.EnvironmentSetting{}
	env, err := applyEnvironmentCreate(setting, "Prod", "")
	require.NoError(t, err)
	require.Contains(t, environmentPalette, env.Color)

	// The default is stable for the same id.
	recreated, err := applyEnvironmentCreate(&storepb.EnvironmentSetting{}, "Prod", "")
	require.NoError(t, err)
	require.Equal(t, env.Color, recreated.Color)

	// An explicit palette key is kept.
	explicit, err := applyEnvironmentCreate(setting, "Dev", "BLUE")
	require.NoError(t, err)
	require.Equal(t, "blue", explicit.Color)

	// Anything outside the palette is rejected: the frontend cannot style it.
	_, err = applyEnvironmentCreate(setting, "Staging", "chartreuse")
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))
}

func TestApplyEnvironmentUpdateKeepsIDAndTags(t *testing.T) {
	t.Parallel()

	setting := &storepb.EnvironmentSetting{}
	env, err := applyEnvironmentCreate(setting, "Prod", "red")
	require.NoError(t, err)
	env.Tags = map[string]string{"team": "platform"}

	title := "Production"
	updated, err := applyEnvironmentUpdate(setting, env.Id, &EnvironmentPatch{Title: &title})
	require.NoError(t, err)
	require.Equal(t, env.Id, updated.Id)
	require.Equal(t, "Production", updated.Title)
	require.Equal(t, map[string]string{"team": "platform"}, updated.Tags)

	// A tags patch replaces the whole map.
	updated, err = applyEnvironmentUpdate(setting, env.Id, &EnvironmentPatch{Tags: map[string]string{"team": "core"}})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"team": "core"}, updated.Tags)

	color := "green"
	updated, err = applyEnvironmentUpdate(setting, env.Id, &EnvironmentPatch{Color: &color})
	require.NoError(t, err)
	require.Equal(t, "green", updated.Color)
}

func TestApplyEnvironmentUpdateRejectsBadInput(t *testing.T) {
	t.Parallel()

	setting := &storepb.EnvironmentSetting{}
	prod, err := applyEnvironmentCreate(setting, "Prod", "")
	require.NoError(t, err)
	_, err = applyEnvironmentCreate(setting, "Dev", "")
	require.NoError(t, err)

	duplicate := "dev"
	_, err = applyEnvironmentUpdate(setting, prod.Id, &EnvironmentPatch{Title: &duplicate})
	require.Error(t, err)
	require.Equal(t, common.Conflict, common.ErrorCode(err))

	blank := "   "
	_, err = applyEnvironmentUpdate(setting, prod.Id, &EnvironmentPatch{Title: &blank})
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))

	badColor := "chartreuse"
	_, err = applyEnvironmentUpdate(setting, prod.Id, &EnvironmentPatch{Color: &badColor})
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))

	_, err = applyEnvironmentUpdate(setting, "missing", &EnvironmentPatch{})
	require.Error(t, err)
	require.Equal(t, common.NotFound, common.ErrorCode(err))
}

func TestApplyEnvironmentDelete(t *testing.T) {
	t.Parallel()

	setting := &storepb.EnvironmentSetting{}
	prod, err := applyEnvironmentCreate(setting, "Prod", "")
	require.NoError(t, err)
	_, err = applyEnvironmentCreate(setting, "Dev", "")
	require.NoError(t, err)

	require.NoError(t, applyEnvironmentDelete(setting, prod.Id))
	require.Len(t, setting.GetEnvironments(), 1)
	require.Equal(t, "dev", setting.GetEnvironments()[0].GetId())

	err = applyEnvironmentDelete(setting, prod.Id)
	require.Error(t, err)
	require.Equal(t, common.NotFound, common.ErrorCode(err))
}
