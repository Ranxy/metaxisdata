package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/plugin/openlineage"
)

// A lineage edge can name a relation the registry has no row for - the writer has
// not synced yet, or the reference is to an object the sync excludes - and the
// graph has to render that node rather than fail the request. The node keeps the
// type the edge claims and invents no metadata.
func TestLineageNodeWithMissingMeta(t *testing.T) {
	node := lineageNodeWithMissingMeta("mysql-dev-1;e2e_ods;;base", v1pb.MetaType_TABLE)
	assert.Equal(t, "mysql-dev-1;e2e_ods;;base", node.GUID)
	assert.Equal(t, storepb.MetaType_TABLE, node.ObjectType)
	assert.Nil(t, node.Metadata, "an unresolved node must not invent metadata")

	fallback := lineageNodeWithMissingMeta("mysql-dev-1;information_schema;;COLUMNS", v1pb.MetaType_UNSPECIFIED)
	assert.Equal(t, storepb.MetaType_TABLE, fallback.ObjectType, "an untyped edge still yields a renderable node")

	view := lineageNodeWithMissingMeta("test-pg-1;e2e;e2e_dwd;v_order_base", v1pb.MetaType_VIEW)
	assert.Equal(t, storepb.MetaType_VIEW, view.ObjectType, "the type the edge claims is kept")

	// An external dataset keeps its own branch, which is unchanged.
	assert.True(t, openlineage.IsExternalGUID("external:postgres://localhost:5432:e2e.public.t"))
}
