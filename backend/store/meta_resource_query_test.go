package store

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// placeholderRe extracts the $N placeholders of a generated query so a guard can
// assert that the numbering stays consistent with the argument slice.
var placeholderRe = regexp.MustCompile(`\$(\d+)`)

func requirePlaceholdersMatchArgs(t *testing.T, query string, args []any) {
	t.Helper()

	maxIndex := 0
	for _, match := range placeholderRe.FindAllStringSubmatch(query, -1) {
		index, err := strconv.Atoi(match[1])
		require.NoError(t, err)
		if index > maxIndex {
			maxIndex = index
		}
	}
	require.Equalf(t, len(args), maxIndex, "query placeholders and args disagree:\n%s\n%v", query, args)
}

func TestBuildSublevelMetaRegistryResourceQueryScopesByGUID(t *testing.T) {
	query, args := buildSublevelMetaRegistryResourceQuery(
		"meta_registry_resource",
		[]storepb.MetaType{storepb.MetaType_DATABASE},
		"instance_1",
		20,
		40,
		nil,
	)

	require.Contains(t, query, "meta_registry_resource.guid = $1")
	require.Contains(t, query, "meta_registry_resource.guid LIKE $2 ESCAPE E'\\\\'")
	require.Contains(t, query, "meta_registry_resource.object_type = $3")
	require.Contains(t, query, "limit 20 offset 40")
	require.Equal(t, []any{"instance_1", `instance\_1;%`, storepb.MetaType_DATABASE}, args)
	requirePlaceholdersMatchArgs(t, query, args)
}

func TestBuildSublevelMetaRegistryResourceQueryEscapesLikeMetacharacters(t *testing.T) {
	// The exact-match argument must stay verbatim while the descendant pattern
	// escapes every LIKE metacharacter.
	query, args := buildSublevelMetaRegistryResourceQuery(
		"meta_registry_resource",
		[]storepb.MetaType{storepb.MetaType_TABLE},
		`a\b%c_d`,
		10,
		0,
		nil,
	)

	require.Equal(t, `a\b%c_d`, args[0])
	require.Equal(t, `a\\b\%c\_d;%`, args[1])
	require.NotContains(t, query, `a\b%c_d;%`)
	requirePlaceholdersMatchArgs(t, query, args)
}

func TestBuildSublevelMetaRegistryResourceQueryUnionsEveryChildType(t *testing.T) {
	nextTypes := []storepb.MetaType{storepb.MetaType_SCHEMA, storepb.MetaType_TABLE}
	query, args := buildSublevelMetaRegistryResourceQuery(
		"meta_registry_resource",
		nextTypes,
		"instance_1",
		5,
		0,
		nil,
	)

	require.Equal(t, 1, strings.Count(query, "UNION ALL"))
	require.Equal(t, 2, strings.Count(query, "meta_registry_resource.guid LIKE"))
	// Each branch repeats the two GUID args followed by its object type, so the
	// numbering stays aligned with the argument order.
	require.Equal(t, []any{
		"instance_1", `instance\_1;%`, storepb.MetaType_SCHEMA,
		"instance_1", `instance\_1;%`, storepb.MetaType_TABLE,
	}, args)
	for index, nextType := range nextTypes {
		require.Contains(t, query, fmt.Sprintf("meta_registry_resource.object_type = $%d", index*3+3))
		require.Equal(t, nextType, args[index*3+2])
	}
	requirePlaceholdersMatchArgs(t, query, args)
}

func TestBuildSublevelMetaRegistryResourceQueryHistoryAddsValidityWindow(t *testing.T) {
	asOf := time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC)
	query, args := buildSublevelMetaRegistryResourceQuery(
		"meta_registry_resource_history",
		[]storepb.MetaType{storepb.MetaType_DATABASE},
		"instance_1",
		10,
		0,
		&asOf,
	)

	require.Contains(t, query, "meta_registry_resource_history.valid_from <= $4")
	require.Contains(t, query, "(meta_registry_resource_history.valid_to IS NULL OR meta_registry_resource_history.valid_to > $4)")
	require.Equal(t, []any{"instance_1", `instance\_1;%`, storepb.MetaType_DATABASE, asOf}, args)
	requirePlaceholdersMatchArgs(t, query, args)
}

func TestBuildSublevelMetaRegistryResourceQueryHasNoPredicateWhenNoChildType(t *testing.T) {
	query, args := buildSublevelMetaRegistryResourceQuery(
		"meta_registry_resource",
		nil,
		"instance_1",
		10,
		0,
		nil,
	)
	require.Empty(t, query)
	require.Empty(t, args)
}
