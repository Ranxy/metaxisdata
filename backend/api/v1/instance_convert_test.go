package v1

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// The proto marks `environment` OPTIONAL, so an instance without one must both
// convert and render back. Parsing the empty name used to fail with
// `invalid request ""`, which also blocked every test-connection call made
// before the user had picked an environment.
func TestInstanceWithoutEnvironment(t *testing.T) {
	t.Parallel()

	message, err := convertInstanceToInstanceMessage("id-1", &v1pb.Instance{
		Engine: v1pb.Engine_MYSQL,
		DataSources: []*v1pb.DataSource{
			{Name: "instances/id-1/dataSources/admin", Type: v1pb.DataSourceType_ADMIN},
		},
	})
	require.NoError(t, err)
	require.Empty(t, message.EnvironmentID)
	require.Len(t, message.Metadata.GetDataSources(), 1)

	instance := convertInstanceMessage(&store.InstanceMessage{
		ResourceID: "id-1",
		Metadata:   &storepb.Instance{Engine: storepb.Engine_MYSQL},
	})
	require.Empty(t, instance.GetEnvironment(), `an unset environment must not render as "environments/"`)
}

func TestInstanceEnvironmentRoundTrip(t *testing.T) {
	t.Parallel()

	message, err := convertInstanceToInstanceMessage("id-1", &v1pb.Instance{
		Engine:      v1pb.Engine_MYSQL,
		Environment: "environments/prod",
	})
	require.NoError(t, err)
	require.Equal(t, "prod", message.EnvironmentID)
	require.Equal(t, "environments/prod", convertInstanceMessage(message).GetEnvironment())
}
