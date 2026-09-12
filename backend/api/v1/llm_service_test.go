package v1

import (
	"testing"

	"github.com/stretchr/testify/require"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

// A title-only PATCH with an empty mask must not touch the model list, which
// used to be replaced wholesale and disabled the profile.
func TestBuildLLMProfileUpdateWithAnEmptyMaskIsPartial(t *testing.T) {
	t.Parallel()

	update, err := buildLLMProfileUpdate(&v1pb.LlmProviderProfile{Title: "renamed"}, nil)
	require.NoError(t, err)
	require.NotNil(t, update.Title)
	require.Equal(t, "renamed", *update.Title)
	require.Nil(t, update.Models, "an omitted model list must be left alone")
	require.Nil(t, update.BaseURL)
	require.Nil(t, update.APIKey)
}

func TestBuildLLMProfileUpdateWithAMask(t *testing.T) {
	t.Parallel()

	profile := &v1pb.LlmProviderProfile{
		Title:   "renamed",
		BaseUrl: "https://llm.example.com",
		Models:  []*v1pb.LlmProviderModel{{Name: "m1"}},
	}

	update, err := buildLLMProfileUpdate(profile, []string{"title"})
	require.NoError(t, err)
	require.NotNil(t, update.Title)
	require.Nil(t, update.BaseURL)
	require.Nil(t, update.Models)

	// An explicit models mask does replace the list, including with an empty one.
	update, err = buildLLMProfileUpdate(profile, []string{"models"})
	require.NoError(t, err)
	require.NotNil(t, update.Models)
	require.Len(t, update.Models, 1)

	_, err = buildLLMProfileUpdate(&v1pb.LlmProviderProfile{BaseUrl: "not-a-url"}, []string{"base_url"})
	require.Error(t, err)
}
