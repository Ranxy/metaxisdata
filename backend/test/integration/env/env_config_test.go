package env

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateIntegrationEnv(t *testing.T) {
	vars := []string{
		integrationPostgresHostEnv,
		integrationPostgresPortEnv,
		integrationMySQLHostEnv,
		integrationMySQLPortEnv,
	}

	t.Run("all unset starts containers", func(t *testing.T) {
		for _, name := range vars {
			t.Setenv(name, "")
		}
		require.NoError(t, ValidateIntegrationEnv())
		require.False(t, ExternalServicesConfigured())
	})

	t.Run("all set uses external services", func(t *testing.T) {
		for _, name := range vars {
			t.Setenv(name, "value")
		}
		require.NoError(t, ValidateIntegrationEnv())
		require.True(t, ExternalServicesConfigured())
	})

	t.Run("database name alone is not enough", func(t *testing.T) {
		for _, name := range vars {
			t.Setenv(name, "")
		}
		t.Setenv(integrationPostgresDBEnv, "metaxisdata")
		require.NoError(t, ValidateIntegrationEnv())
		require.False(t, ExternalServicesConfigured())
	})

	t.Run("partial configuration fails fast", func(t *testing.T) {
		for _, name := range vars {
			t.Setenv(name, "")
		}
		t.Setenv(integrationMySQLHostEnv, "127.0.0.1")
		require.ErrorContains(t, ValidateIntegrationEnv(), integrationMySQLPortEnv)
		require.True(t, ExternalServicesConfigured())
	})
}
