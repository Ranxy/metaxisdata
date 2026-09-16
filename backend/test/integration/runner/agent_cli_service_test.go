//go:build integration

package runner

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	integrationenv "github.com/Ranxy/metaxisdata/backend/test/integration/env"
)

// These cover the three RPC groups the agent CLI is built on, against the real
// server: the device login flow, AnalyzeSQL and GetLineageGraph.

// apiClients bundles the service clients the scenarios need, each bound to the
// credential given.
type apiClients struct {
	auth     v1connect.AuthServiceClient
	user     v1connect.UserServiceClient
	database v1connect.DatabaseServiceClient
	lineage  v1connect.LineageServiceClient
}

// newAPIClients builds clients that send, or omit, a bearer token. Anonymous
// access is the whole point of two of the device login RPCs, so the token has
// to be genuinely absent there rather than merely ignored.
func newAPIClients(env *integrationenv.ServiceEnv, token string) *apiClients {
	httpClient := &http.Client{Timeout: 30 * time.Second}
	if token != "" {
		httpClient.Transport = bearerRoundTripper{base: http.DefaultTransport, token: token}
	}
	return &apiClients{
		auth:     v1connect.NewAuthServiceClient(httpClient, env.BaseURL),
		user:     v1connect.NewUserServiceClient(httpClient, env.BaseURL),
		database: v1connect.NewDatabaseServiceClient(httpClient, env.BaseURL),
		lineage:  v1connect.NewLineageServiceClient(httpClient, env.BaseURL),
	}
}

type bearerRoundTripper struct {
	base  http.RoundTripper
	token string
}

func (t bearerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	cloned.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(cloned)
}

// TestDeviceLoginRealServerIntegration walks the whole flow: an anonymous
// client opens a request, a signed-in user approves it, and the client exchanges
// the polling secret for a token it can then use.
func TestDeviceLoginRealServerIntegration(t *testing.T) {
	t.Parallel()

	env := sharedMySQLServiceEnvNoReset(t)
	ctx := context.Background()
	anonymous := newAPIClients(env, "")
	signedIn := newAPIClients(env, env.AdminToken())

	created, err := anonymous.auth.CreateDeviceLogin(ctx, connect.NewRequest(&v1pb.CreateDeviceLoginRequest{
		ClientName:    "mxd",
		ClientVersion: "0.1.0",
	}))
	require.NoError(t, err)
	require.NotEmpty(t, created.Msg.GetDeviceCode())
	require.Len(t, created.Msg.GetUserCode(), 9, "the human code is XXXX-XXXX")
	require.Equal(t, int32(600), created.Msg.GetExpiresIn())
	require.Positive(t, created.Msg.GetInterval())

	// The page reads the request before anyone decides anything.
	detail, err := signedIn.auth.GetDeviceLogin(ctx, connect.NewRequest(&v1pb.GetDeviceLoginRequest{
		Name: "deviceLogins/" + created.Msg.GetUserCode(),
	}))
	require.NoError(t, err)
	require.Equal(t, v1pb.DeviceLoginState_PENDING, detail.Msg.GetState())
	require.Equal(t, "mxd", detail.Msg.GetClientName())
	require.Equal(t, created.Msg.GetUserCode(), detail.Msg.GetUserCode())

	// Polling before the decision reports that nothing has happened yet.
	pending, err := anonymous.auth.ExchangeDeviceLogin(ctx, connect.NewRequest(&v1pb.ExchangeDeviceLoginRequest{
		DeviceCode: created.Msg.GetDeviceCode(),
	}))
	require.NoError(t, err)
	require.Equal(t, v1pb.DeviceLoginState_PENDING, pending.Msg.GetState())
	require.Empty(t, pending.Msg.GetToken())

	_, err = signedIn.auth.ApproveDeviceLogin(ctx, connect.NewRequest(&v1pb.ApproveDeviceLoginRequest{
		Name:    "deviceLogins/" + created.Msg.GetUserCode(),
		Approve: true,
	}))
	require.NoError(t, err)

	exchanged, err := anonymous.auth.ExchangeDeviceLogin(ctx, connect.NewRequest(&v1pb.ExchangeDeviceLoginRequest{
		DeviceCode: created.Msg.GetDeviceCode(),
	}))
	require.NoError(t, err)
	require.Equal(t, v1pb.DeviceLoginState_APPROVED, exchanged.Msg.GetState())
	require.NotEmpty(t, exchanged.Msg.GetToken())
	require.NotEmpty(t, exchanged.Msg.GetUser().GetEmail())

	// The token is a real credential for the user who approved the request.
	deviceClients := newAPIClients(env, exchanged.Msg.GetToken())
	me, err := deviceClients.user.GetCurrentUser(ctx, connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)
	require.Equal(t, exchanged.Msg.GetUser().GetEmail(), me.Msg.GetEmail())

	// The request is consumed exactly once.
	_, err = anonymous.auth.ExchangeDeviceLogin(ctx, connect.NewRequest(&v1pb.ExchangeDeviceLoginRequest{
		DeviceCode: created.Msg.GetDeviceCode(),
	}))
	require.Equal(t, connect.CodeNotFound, connect.CodeOf(err), "a device login answers once")
}

// TestDeviceLoginDeniedRealServerIntegration covers the refusal path, and that
// a denial cannot be turned into an approval afterwards.
func TestDeviceLoginDeniedRealServerIntegration(t *testing.T) {
	t.Parallel()

	env := sharedMySQLServiceEnvNoReset(t)
	ctx := context.Background()
	anonymous := newAPIClients(env, "")
	signedIn := newAPIClients(env, env.AdminToken())

	created, err := anonymous.auth.CreateDeviceLogin(ctx, connect.NewRequest(&v1pb.CreateDeviceLoginRequest{ClientName: "mxd"}))
	require.NoError(t, err)
	name := "deviceLogins/" + created.Msg.GetUserCode()

	_, err = signedIn.auth.ApproveDeviceLogin(ctx, connect.NewRequest(&v1pb.ApproveDeviceLoginRequest{Name: name, Approve: false}))
	require.NoError(t, err)

	// A denial is final: the decision cannot be flipped while the request is
	// still waiting to be collected.
	_, err = signedIn.auth.ApproveDeviceLogin(ctx, connect.NewRequest(&v1pb.ApproveDeviceLoginRequest{Name: name, Approve: true}))
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))

	exchanged, err := anonymous.auth.ExchangeDeviceLogin(ctx, connect.NewRequest(&v1pb.ExchangeDeviceLoginRequest{
		DeviceCode: created.Msg.GetDeviceCode(),
	}))
	require.NoError(t, err)
	require.Equal(t, v1pb.DeviceLoginState_DENIED, exchanged.Msg.GetState())
	require.Empty(t, exchanged.Msg.GetToken())

	// Reporting the denial consumes the request, so nothing is left to decide.
	_, err = signedIn.auth.ApproveDeviceLogin(ctx, connect.NewRequest(&v1pb.ApproveDeviceLoginRequest{Name: name, Approve: true}))
	require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))

	// An unknown code is not found rather than a server error.
	_, err = signedIn.auth.GetDeviceLogin(ctx, connect.NewRequest(&v1pb.GetDeviceLoginRequest{Name: "deviceLogins/UUUU-UUUU"}))
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "a code outside the alphabet is rejected")
	_, err = signedIn.auth.GetDeviceLogin(ctx, connect.NewRequest(&v1pb.GetDeviceLoginRequest{Name: "deviceLogins/0000-0000"}))
	require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
}

// TestAnalyzeSQLRealServerIntegration checks the statement analysis against the
// fixture, including the two rules the implementation depends on: a bare SELECT
// has no target object, and a statement that writes somewhere real has exactly
// one.
//
//nolint:tparallel // the subtests share one synced database and one client on purpose
func TestAnalyzeSQLRealServerIntegration(t *testing.T) {
	t.Parallel()

	env, ctx, instanceID, sourceDatabase, databaseName := setupMySQLServiceDatabase(t)
	env.SyncDatabase(ctx, t, databaseName)
	client := newAPIClients(env, env.AdminToken())

	scopeGUID := fmt.Sprintf("%s;%s", instanceID, sourceDatabase)
	scope := &v1pb.AnalysisScope{Name: "fixture", Guid: scopeGUID}

	usersGUID := fmt.Sprintf("%s;%s;;users", instanceID, sourceDatabase)

	t.Run("bare select", func(t *testing.T) {
		response, err := client.lineage.AnalyzeSQL(ctx, connect.NewRequest(&v1pb.AnalyzeSQLRequest{
			Scopes:  []*v1pb.AnalysisScope{scope},
			SqlText: "SELECT name, age FROM users",
		}))
		require.NoError(t, err)
		require.Len(t, response.Msg.GetResults(), 1)

		result := response.Msg.GetResults()[0]
		require.Equal(t, "fixture", result.GetScopeName())
		require.Equal(t, scopeGUID, result.GetScopeGuid())
		require.NotEmpty(t, result.GetRelations())

		for _, relation := range result.GetRelations() {
			require.Equal(t, usersGUID, relation.GetSourceGuid())
			require.Empty(t, relation.GetTargetGuid(), "a bare SELECT has no target object")
			require.True(t, relation.GetIsTemp())
			require.NotEmpty(t, relation.GetTargetColumn(), "the output alias is still reported")
			require.Equal(t, v1pb.MetaType_TABLE, relation.GetSourceType(), "the type comes from the registry")
		}
	})

	t.Run("create view", func(t *testing.T) {
		response, err := client.lineage.AnalyzeSQL(ctx, connect.NewRequest(&v1pb.AnalyzeSQLRequest{
			Scopes:  []*v1pb.AnalysisScope{scope},
			SqlText: "CREATE VIEW order_totals AS SELECT user_id, amount FROM orders",
		}))
		require.NoError(t, err)

		relations := response.Msg.GetResults()[0].GetRelations()
		require.NotEmpty(t, relations)
		require.False(t, relations[0].GetIsTemp(), "the synthetic result edge is dropped when a real target exists")
		require.Equal(t, fmt.Sprintf("%s;%s;;order_totals", instanceID, sourceDatabase), relations[0].GetTargetGuid())
		require.Equal(t, fmt.Sprintf("%s;%s;;orders", instanceID, sourceDatabase), relations[0].GetSourceGuid())
	})

	t.Run("view definition resolves like the runner does", func(t *testing.T) {
		// The same statement the stored view holds must resolve to the same
		// edges the runner persisted, which is what sharing the identifier
		// completion buys.
		response, err := client.lineage.AnalyzeSQL(ctx, connect.NewRequest(&v1pb.AnalyzeSQLRequest{
			Scopes:  []*v1pb.AnalysisScope{scope},
			SqlText: "SELECT u.id AS user_id, u.name AS user_name, o.amount AS order_amount FROM users u JOIN orders o ON u.id = o.user_id",
		}))
		require.NoError(t, err)

		relations := response.Msg.GetResults()[0].GetRelations()
		require.NotEmpty(t, relations)
		for _, relation := range relations {
			require.Contains(t, []string{usersGUID, fmt.Sprintf("%s;%s;;orders", instanceID, sourceDatabase)}, relation.GetSourceGuid())
			require.Empty(t, relation.GetTargetGuid())
		}
	})

	t.Run("one scope may fail while the request succeeds", func(t *testing.T) {
		response, err := client.lineage.AnalyzeSQL(ctx, connect.NewRequest(&v1pb.AnalyzeSQLRequest{
			Scopes: []*v1pb.AnalysisScope{
				scope,
				{Name: "missing", Guid: "it-does-not-exist;nope"},
			},
			SqlText: "SELECT name FROM users",
		}))
		require.NoError(t, err, "a scope list is not defeated by one unusable scope")
		require.Len(t, response.Msg.GetResults(), 2)

		require.Empty(t, response.Msg.GetResults()[0].GetWarnings())
		require.NotEmpty(t, response.Msg.GetResults()[1].GetWarnings())
		require.Empty(t, response.Msg.GetResults()[1].GetRelations())
	})

	t.Run("every scope failing the same way is an error", func(t *testing.T) {
		_, err := client.lineage.AnalyzeSQL(ctx, connect.NewRequest(&v1pb.AnalyzeSQLRequest{
			Scopes:  []*v1pb.AnalysisScope{{Name: "missing", Guid: "it-does-not-exist;nope"}},
			SqlText: "SELECT name FROM users",
		}))
		require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
	})

	t.Run("limits and shapes are enforced", func(t *testing.T) {
		_, err := client.lineage.AnalyzeSQL(ctx, connect.NewRequest(&v1pb.AnalyzeSQLRequest{SqlText: "SELECT 1"}))
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "at least one scope is required")

		_, err = client.lineage.AnalyzeSQL(ctx, connect.NewRequest(&v1pb.AnalyzeSQLRequest{
			Scopes:  []*v1pb.AnalysisScope{{Guid: "only-one-segment"}},
			SqlText: "SELECT 1",
		}))
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "a scope is instance;database")

		_, err = client.lineage.AnalyzeSQL(ctx, connect.NewRequest(&v1pb.AnalyzeSQLRequest{
			Scopes:  []*v1pb.AnalysisScope{scope},
			SqlText: "   ",
		}))
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	})
}

// TestAnalyzeSQLFromListedGUIDRealServerIntegration is the path a user takes:
// read the GUID the server reports, put it in the project's scope list, and get
// a usable answer out of it.
func TestAnalyzeSQLFromListedGUIDRealServerIntegration(t *testing.T) {
	t.Parallel()

	env, ctx, _, sourceDatabase, databaseName := setupMySQLServiceDatabase(t)
	env.SyncDatabase(ctx, t, databaseName)
	client := newAPIClients(env, env.AdminToken())

	listed, err := client.database.ListDatabases(ctx, connect.NewRequest(&v1pb.ListDatabasesRequest{
		Parent: "workspaces/-",
	}))
	require.NoError(t, err)

	var scopeGUID string
	for _, database := range listed.Msg.GetDatabases() {
		if database.GetName() == databaseName {
			scopeGUID = database.GetGuid()
			break
		}
	}
	require.NotEmpty(t, scopeGUID, "a listed database carries the GUID that is used as a scope")
	require.Contains(t, scopeGUID, sourceDatabase)

	response, err := client.lineage.AnalyzeSQL(ctx, connect.NewRequest(&v1pb.AnalyzeSQLRequest{
		Scopes:  []*v1pb.AnalysisScope{{Name: "listed", Guid: scopeGUID}},
		SqlText: "SELECT name FROM users",
	}))
	require.NoError(t, err)
	require.NotEmpty(t, response.Msg.GetResults()[0].GetRelations())

	// The metadata listing carries GUIDs too, so its results can be chained
	// into GetMetadata, GetSchemaString and the lineage calls.
	schemas, err := client.database.ListMetadata(ctx, connect.NewRequest(&v1pb.ListMetadataRequest{
		ParentGuid: scopeGUID + ";",
		PageSize:   100,
	}))
	require.NoError(t, err)
	for _, group := range schemas.Msg.GetTypesStoredMetadata() {
		for _, stored := range group.GetList() {
			require.NotEmpty(t, stored.GetGuid(), "every listed object reports the GUID it is addressed by")
		}
	}
}

// TestGetLineageGraphRealServerIntegration walks the stored graph around the
// fixture view and checks the ceilings are reported rather than exceeded.
func TestGetLineageGraphRealServerIntegration(t *testing.T) {
	t.Parallel()

	env, ctx, instanceID, sourceDatabase, databaseName := setupMySQLServiceDatabase(t)
	env.SyncDatabase(ctx, t, databaseName)

	viewGUID := fmt.Sprintf("%s;%s;;user_order_view", instanceID, sourceDatabase)
	usersGUID := fmt.Sprintf("%s;%s;;users", instanceID, sourceDatabase)
	// Wait for the runner to have analyzed the view, so the graph has edges.
	env.WaitForContextLineage(ctx, t, viewGUID, v1pb.MetaType_VIEW, func(relations []*v1pb.LineageRelation) bool {
		return hasAPILineageEdge(relations, usersGUID, "name", viewGUID, "user_name")
	})

	client := newAPIClients(env, env.AdminToken())
	response, err := client.lineage.GetLineageGraph(ctx, connect.NewRequest(&v1pb.GetLineageGraphRequest{
		Guid:  viewGUID,
		Depth: 3,
	}))
	require.NoError(t, err)

	graph := response.Msg
	require.Equal(t, viewGUID, graph.GetRootGuid())
	require.False(t, graph.GetTruncated(), "the fixture graph is tiny")
	require.Positive(t, graph.GetDepthReached())

	var foundUsers bool
	for _, node := range graph.GetNodes() {
		require.NotEmpty(t, node.GetGuid())
		if node.GetGuid() == usersGUID {
			foundUsers = true
			require.Equal(t, int32(-1), node.GetDistance(), "upstream is negative")
			require.Equal(t, "users", node.GetName())
			require.Equal(t, sourceDatabase, node.GetDatabase())
			require.Equal(t, v1pb.MetaType_TABLE, node.GetMetaType())
		}
	}
	require.True(t, foundUsers, "the view's upstream table is in the graph")

	// Upstream only must not reach anything downstream of the view.
	upstreamOnly, err := client.lineage.GetLineageGraph(ctx, connect.NewRequest(&v1pb.GetLineageGraphRequest{
		Guid:        viewGUID,
		LineageType: v1pb.LineageType_SOURCE,
		Depth:       3,
	}))
	require.NoError(t, err)
	for _, node := range upstreamOnly.Msg.GetNodes() {
		require.LessOrEqual(t, node.GetDistance(), int32(0), "a source walk never goes downstream")
	}

	// A depth outside the documented range is refused rather than clamped.
	_, err = client.lineage.GetLineageGraph(ctx, connect.NewRequest(&v1pb.GetLineageGraphRequest{
		Guid:  viewGUID,
		Depth: 99,
	}))
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}
