package mysql

import (
	"context"
	"testing"

	gomysql "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"
)

// The sync path builds its DSN from a database name read out of the target
// instance's catalog, so a name carrying DSN syntax must stay a name: left raw,
// `x?tls=false&` ends the path segment, turns TLS off and connects to `x`.
func TestBuildDSNEscapesCatalogDatabaseName(t *testing.T) {
	t.Parallel()

	for _, databaseName := range []string{
		"app",
		"x?tls=false&",
		"x?allowAllFiles=true&",
		"a&b=c",
		"a#b",
		"a/b",
		"100%",
		"?",
	} {
		t.Run(databaseName, func(t *testing.T) {
			t.Parallel()

			dsn := BuildDSN("root", "pw", "tcp", "127.0.0.1", "3306", databaseName, []string{"maxAllowedPacket=0"})
			cfg, err := gomysql.ParseDSN(dsn)
			require.NoError(t, err)
			require.Equal(t, databaseName, cfg.DBName, "the catalog name must survive the round trip")
			require.Empty(t, cfg.TLSConfig, "a database name must not be able to turn TLS off")
			require.False(t, cfg.MultiStatements, "a database name must not be able to turn multiStatements on")
			require.False(t, cfg.AllowAllFiles, "a database name must not be able to allow every file")
			require.Empty(t, cfg.Params, "a database name must not be able to add connection parameters")
			require.Equal(t, "127.0.0.1:3306", cfg.Addr)
			require.Equal(t, "root", cfg.User)
			require.Equal(t, "pw", cfg.Passwd)
		})
	}
}

// The interpolation this escaping replaced, kept as an executable description:
// the vulnerable DSN parses without error, so nothing fails loudly — the
// connection silently drops TLS and uses the truncated database name.
func TestRawDSNInterpolationRewritesTheConnection(t *testing.T) {
	t.Parallel()

	cfg, err := gomysql.ParseDSN("root:pw@tcp(127.0.0.1:3306)/x?tls=false&?maxAllowedPacket=0")
	require.NoError(t, err)
	require.Equal(t, "x", cfg.DBName)
	require.Equal(t, "false", cfg.TLSConfig)
}

// TestGetMySQLConnectionEscapesCatalogDatabaseName pins the driver's own call
// site, not only the builder: this is what a per-database sync opens.
func TestGetMySQLConnectionEscapesCatalogDatabaseName(t *testing.T) {
	t.Parallel()

	const databaseName = "x?tls=false&"
	d := &Driver{}
	dsn, err := d.getMySQLConnection(context.Background(), db.ConnectionConfig{
		DataSource:        &storepb.DataSource{Host: "127.0.0.1", Port: "3306", Username: "root"},
		ConnectionContext: db.ConnectionContext{DatabaseName: databaseName},
		Password:          "pw",
	})
	require.NoError(t, err)

	cfg, err := gomysql.ParseDSN(dsn)
	require.NoError(t, err)
	require.Equal(t, databaseName, cfg.DBName)
	require.Empty(t, cfg.TLSConfig)
	require.False(t, cfg.MultiStatements)
}
