package v1

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// TestQualityCrossTableRightGUID pins the container rules: the MySQL family
// qualifies with the database, PostgreSQL and SQL Server with the schema, and a
// qualified name may not move to another container.
func TestQualityCrossTableRightGUID(t *testing.T) {
	t.Parallel()

	mysql := dataquality.ResolvedTarget{
		Target:             dataquality.Target{GUID: "instances/ins1;shop;orders"},
		Engine:             storepb.Engine_MYSQL,
		Database:           "shop",
		Schema:             "",
		Table:              "orders",
		InstanceResourceID: "instances/ins1",
	}
	guid, err := qualityCrossTableRightGUID(mysql, "customers")
	require.NoError(t, err)
	require.Equal(t, "instances/ins1;shop;;customers", guid)
	guid, err = qualityCrossTableRightGUID(mysql, "shop.customers")
	require.NoError(t, err)
	require.Equal(t, "instances/ins1;shop;;customers", guid)
	_, err = qualityCrossTableRightGUID(mysql, "billing.customers")
	require.Error(t, err)
	_, err = qualityCrossTableRightGUID(mysql, "shop.other.customers")
	require.Error(t, err)

	postgres := dataquality.ResolvedTarget{
		Target:             dataquality.Target{GUID: "instances/ins1;shop;public;orders"},
		Engine:             storepb.Engine_POSTGRES,
		Database:           "shop",
		Schema:             "public",
		Table:              "orders",
		InstanceResourceID: "instances/ins1",
	}
	guid, err = qualityCrossTableRightGUID(postgres, "customers")
	require.NoError(t, err)
	require.Equal(t, "instances/ins1;shop;public;customers", guid)
	guid, err = qualityCrossTableRightGUID(postgres, "public.customers")
	require.NoError(t, err)
	require.Equal(t, "instances/ins1;shop;public;customers", guid)
	_, err = qualityCrossTableRightGUID(postgres, "billing.customers")
	require.Error(t, err)
}
