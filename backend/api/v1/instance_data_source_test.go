package v1

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// patchDataSource writes only the fields named in the update mask. A request
// built from a read never carries credentials, so an unmasked password, SSL
// material or TLS verification setting must survive the update — overwriting the
// stored entry blanked every password and reset TLS verification to false.
func TestPatchDataSourcePreservesUnmaskedFields(t *testing.T) {
	t.Parallel()

	stored := &storepb.DataSource{
		Id:                   "admin",
		Type:                 storepb.DataSourceType_ADMIN,
		Host:                 "old-host",
		Port:                 "3306",
		Username:             "root",
		Password:             "stored-password",
		ObfuscatedPassword:   "obfuscated",
		UseSsl:               true,
		SslCa:                "stored-ca",
		SslKey:               "stored-key",
		VerifyTlsCertificate: true,
		SshHost:              "bastion",
		SshPort:              "22",
		SshUser:              "tunnel",
		SshPrivateKey:        "stored-private-key",
		ExtraConnectionParameters: map[string]string{
			"timeout": "5s",
		},
	}
	requested := &v1pb.DataSource{
		Host:     "new-host",
		Port:     "3307",
		Password: "",
	}

	require.NoError(t, patchDataSource(stored, requested, []string{"host", "port"}))

	require.Equal(t, "new-host", stored.GetHost(), "a masked field is written")
	require.Equal(t, "3307", stored.GetPort())
	require.Equal(t, "stored-password", stored.GetPassword(), "an unmasked credential keeps its stored value")
	require.Equal(t, "obfuscated", stored.GetObfuscatedPassword())
	require.Equal(t, "stored-ca", stored.GetSslCa())
	require.Equal(t, "stored-key", stored.GetSslKey())
	require.True(t, stored.GetVerifyTlsCertificate(), "TLS verification must not be downgraded")
	require.Equal(t, "bastion", stored.GetSshHost())
	require.Equal(t, "22", stored.GetSshPort())
	require.Equal(t, "tunnel", stored.GetSshUser())
	require.Equal(t, "stored-private-key", stored.GetSshPrivateKey())
	require.Equal(t, map[string]string{"timeout": "5s"}, stored.GetExtraConnectionParameters())
}

func TestPatchDataSourceWritesEverySupportedField(t *testing.T) {
	t.Parallel()

	stored := &storepb.DataSource{Id: "admin", Type: storepb.DataSourceType_ADMIN}
	requested := &v1pb.DataSource{
		Username:                  "new-user",
		Password:                  "new-password",
		SslCa:                     "new-ca",
		SslCert:                   "new-cert",
		SslKey:                    "new-key",
		Host:                      "new-host",
		Port:                      "3307",
		Database:                  "app",
		SshHost:                   "new-bastion",
		SshPort:                   "2222",
		SshUser:                   "new-tunnel",
		SshPassword:               "new-ssh-password",
		SshPrivateKey:             "new-private-key",
		UseSsl:                    true,
		ExtraConnectionParameters: map[string]string{"timeout": "9s"},
	}

	require.NoError(t, patchDataSource(stored, requested, []string{
		"username", "password", "ssl_ca", "ssl_cert", "ssl_key", "host", "port", "database",
		"ssh_host", "ssh_port", "ssh_user", "ssh_password", "ssh_private_key", "use_ssl",
		"extra_connection_parameters",
	}))

	require.Equal(t, "new-user", stored.GetUsername())
	require.Equal(t, "new-password", stored.GetPassword())
	require.Equal(t, "new-ca", stored.GetSslCa())
	require.Equal(t, "new-cert", stored.GetSslCert())
	require.Equal(t, "new-key", stored.GetSslKey())
	require.Equal(t, "new-host", stored.GetHost())
	require.Equal(t, "3307", stored.GetPort())
	require.Equal(t, "app", stored.GetDatabase())
	require.Equal(t, "new-bastion", stored.GetSshHost())
	require.Equal(t, "2222", stored.GetSshPort())
	require.Equal(t, "new-tunnel", stored.GetSshUser())
	require.Equal(t, "new-ssh-password", stored.GetSshPassword())
	require.Equal(t, "new-private-key", stored.GetSshPrivateKey())
	require.True(t, stored.GetUseSsl())
	require.Equal(t, map[string]string{"timeout": "9s"}, stored.GetExtraConnectionParameters())
}

func TestPatchDataSourceRejectsUnknownMaskPaths(t *testing.T) {
	t.Parallel()

	stored := &storepb.DataSource{Id: "admin", Host: "old-host"}

	require.ErrorContains(t, patchDataSource(stored, &v1pb.DataSource{}, []string{"name"}), "unsupported update_mask")
	require.ErrorContains(t, patchDataSource(stored, &v1pb.DataSource{}, []string{"type"}), "unsupported update_mask")
	require.Equal(t, "old-host", stored.GetHost(), "a rejected mask leaves the stored entry untouched")
}

func TestCheckInstanceDataSourcesRequiresOneAdmin(t *testing.T) {
	t.Parallel()

	service := &InstanceService{}
	instance := &store.InstanceMessage{}

	require.NoError(t, service.checkInstanceDataSources(instance, []*storepb.DataSource{
		{Id: "admin", Type: storepb.DataSourceType_ADMIN},
		{Id: "readonly", Type: storepb.DataSourceType_READ_ONLY},
	}))

	for _, dataSources := range [][]*storepb.DataSource{
		{},
		{{Id: "readonly", Type: storepb.DataSourceType_READ_ONLY}},
		{
			{Id: "admin-1", Type: storepb.DataSourceType_ADMIN},
			{Id: "admin-2", Type: storepb.DataSourceType_ADMIN},
		},
	} {
		err := service.checkInstanceDataSources(instance, dataSources)
		require.Error(t, err)
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	}

	err := service.checkInstanceDataSources(instance, []*storepb.DataSource{
		{Id: "admin", Type: storepb.DataSourceType_ADMIN},
		{Id: "admin", Type: storepb.DataSourceType_READ_ONLY},
	})
	require.Error(t, err, "duplicate IDs are still rejected")
}

// Only the engines the server actually has a driver for may be stored; the
// proto enum also carries reserved values.
func TestIsSupportedStoreEngine(t *testing.T) {
	t.Parallel()

	for _, engine := range []storepb.Engine{
		storepb.Engine_MYSQL,
		storepb.Engine_POSTGRES,
		storepb.Engine_TIDB,
		storepb.Engine_MARIADB,
		storepb.Engine_OCEANBASE,
	} {
		require.True(t, isSupportedStoreEngine(engine), engine.String())
	}
	require.False(t, isSupportedStoreEngine(storepb.Engine_ENGINE_UNSPECIFIED))
	require.False(t, isSupportedStoreEngine(storepb.Engine(28)))
}
