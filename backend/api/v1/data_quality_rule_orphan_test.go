package v1

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// TestTargetOrphanReason pins the rule read path's orphan decision against the
// same conditions dataquality.ResolveTarget applies before an execution: the
// registry row, the instance (present, not deleted, with metadata) and the
// database.
func TestTargetOrphanReason(t *testing.T) {
	t.Parallel()

	rule := &store.QualityRuleMessage{
		TargetGUID: "inst;shop;dq_orders",
		TargetType: storepb.MetaType_TABLE,
		TargetPath: &storepb.QualityTargetPath{Instance: "inst", Database: "shop", Object: "dq_orders"},
	}
	const registryGone = "the target \"inst;shop;dq_orders\" is no longer in the metadata registry"

	for _, test := range []struct {
		name            string
		registered      bool
		instanceFound   bool
		instanceDeleted bool
		instanceHasMeta bool
		databaseLive    bool
		want            string
	}{
		{name: "healthy", registered: true, instanceFound: true, instanceHasMeta: true, databaseLive: true},
		{name: "registry row gone", registered: false, want: registryGone},
		{name: "instance missing", registered: true, instanceDeleted: false, instanceHasMeta: true, databaseLive: true, want: "the target's instance \"inst\" is gone"},
		{name: "instance deleted", registered: true, instanceFound: true, instanceDeleted: true, instanceHasMeta: true, databaseLive: true, want: "the target's instance \"inst\" is gone"},
		{name: "instance without metadata", registered: true, instanceFound: true, instanceHasMeta: false, databaseLive: true, want: "the target's instance \"inst\" has no metadata"},
		{name: "database gone", registered: true, instanceFound: true, instanceHasMeta: true, databaseLive: false, want: "the target's database \"shop\" is gone or hidden"},
	} {
		var want string
		if test.want != "" {
			want = test.want
		}
		got := targetOrphanReason(rule, test.registered, test.instanceFound, test.instanceDeleted, test.instanceHasMeta, test.databaseLive)
		require.Equalf(t, want, got, "%s", test.name)
	}
}

// TestRuleTargetPathSegments covers the path snapshot and the GUID fallback the
// orphan check uses to find a rule's instance and database. A rule without a
// path snapshot (or with a shorter GUID) must not invent segments.
func TestRuleTargetPathSegments(t *testing.T) {
	t.Parallel()

	withPath := &store.QualityRuleMessage{
		TargetGUID: "inst;shop;dq_orders",
		TargetPath: &storepb.QualityTargetPath{Instance: "inst", Database: "shop", Object: "dq_orders"},
	}
	require.Equal(t, "inst", ruleTargetInstanceID(withPath))
	require.Equal(t, "shop", ruleTargetDatabaseName(withPath))

	guidOnly := &store.QualityRuleMessage{TargetGUID: "inst;shop;dq_orders"}
	require.Equal(t, "inst", ruleTargetInstanceID(guidOnly))
	require.Equal(t, "shop", ruleTargetDatabaseName(guidOnly))

	require.Empty(t, ruleTargetInstanceID(nil))
	require.Empty(t, ruleTargetDatabaseName(nil))
	require.Equal(t, "inst", ruleTargetInstanceID(&store.QualityRuleMessage{TargetGUID: "inst"}))
	require.Empty(t, ruleTargetDatabaseName(&store.QualityRuleMessage{TargetGUID: "inst"}))
}
