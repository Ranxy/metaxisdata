package v1

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/store"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// A data source list built from a Get response carries neither credentials nor
// store-only fields, because reads never return them. Overwriting the stored
// entry with it blanked every password, dropped SSL material and reset TLS
// verification to false.
func TestMergeDataSourcePreservesUnreturnedFields(t *testing.T) {
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
	requested := &storepb.DataSource{
		Id:       "admin",
		Type:     storepb.DataSourceType_ADMIN,
		Host:     "new-host",
		Port:     "3307",
		Username: "root",
	}

	merged := mergeDataSource(stored, requested)

	require.Equal(t, "new-host", merged.GetHost(), "requested host wins")
	require.Equal(t, "3307", merged.GetPort(), "requested port wins")
	require.Equal(t, "stored-password", merged.GetPassword(), "omitted credential is kept")
	require.Equal(t, "obfuscated", merged.GetObfuscatedPassword())
	require.Equal(t, "stored-ca", merged.GetSslCa())
	require.Equal(t, "stored-key", merged.GetSslKey())
	require.True(t, merged.GetVerifyTlsCertificate(), "TLS verification must not be downgraded")
	require.Equal(t, "bastion", merged.GetSshHost())
	require.Equal(t, "22", merged.GetSshPort())
	require.Equal(t, "tunnel", merged.GetSshUser())
	require.Equal(t, "stored-private-key", merged.GetSshPrivateKey())
	require.Equal(t, map[string]string{"timeout": "5s"}, merged.GetExtraConnectionParameters())

	// The stored entry itself is never mutated.
	require.Equal(t, "old-host", stored.GetHost())
}

func TestMergeDataSourceOverlaysProvidedValues(t *testing.T) {
	t.Parallel()

	stored := &storepb.DataSource{
		Id:       "admin",
		Type:     storepb.DataSourceType_ADMIN,
		Password: "old-password",
		ExtraConnectionParameters: map[string]string{
			"timeout": "5s",
			"sslmode": "disable",
		},
	}
	requested := &storepb.DataSource{
		Id:       "admin",
		Type:     storepb.DataSourceType_ADMIN,
		Password: "new-password",
		Database: "app",
		UseSsl:   true,
		SshHost:  "new-bastion",
		ExtraConnectionParameters: map[string]string{
			"timeout": "9s",
		},
	}

	merged := mergeDataSource(stored, requested)

	require.Equal(t, "new-password", merged.GetPassword(), "a provided credential replaces the stored one")
	require.Equal(t, "app", merged.GetDatabase())
	require.True(t, merged.GetUseSsl())
	require.Equal(t, "new-bastion", merged.GetSshHost())
	require.Equal(t, map[string]string{"timeout": "9s", "sslmode": "disable"}, merged.GetExtraConnectionParameters())
}

func TestMergeDataSourcesKeysByID(t *testing.T) {
	t.Parallel()

	stored := []*storepb.DataSource{
		{Id: "admin", Type: storepb.DataSourceType_ADMIN, Password: "admin-password", VerifyTlsCertificate: true},
		{Id: "readonly", Type: storepb.DataSourceType_READ_ONLY, Password: "readonly-password"},
	}
	requested := []*storepb.DataSource{
		{Id: "admin", Type: storepb.DataSourceType_ADMIN, Host: "db.internal"},
		{Id: "new-readonly", Type: storepb.DataSourceType_READ_ONLY, Host: "reporting.internal"},
	}

	merged := mergeDataSources(stored, requested)

	require.Len(t, merged, 2, "an ID missing from the request is removed")
	require.Equal(t, "admin", merged[0].GetId())
	require.Equal(t, "db.internal", merged[0].GetHost())
	require.Equal(t, "admin-password", merged[0].GetPassword())
	require.True(t, merged[0].GetVerifyTlsCertificate())
	require.Equal(t, "new-readonly", merged[1].GetId(), "an unknown ID is added as requested")
	require.Equal(t, "reporting.internal", merged[1].GetHost())
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
