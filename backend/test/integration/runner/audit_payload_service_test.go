//go:build integration

package runner

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/audit"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
)

const createUserMethod = "/metaxisdata.v1.UserService/CreateUser"

// H6: CreateUser needs no credential and is audited, and audit_log is never
// pruned. Before this fix an anonymous caller could post 100MB and leave a row
// of that size behind, one request at a time. Two independent bounds close it:
// the handler refuses a body over maxUserServiceRequestBytes before the audit
// interceptor runs at all, and the audit payload itself is bounded, field by
// field and as a whole.
func TestAnonymousCreateUserCannotBloatTheAuditLedgerRealServerIntegration(t *testing.T) {
	// Not parallel: it reads the shared ledger of the shared server.
	ctx := context.Background()
	env := sharedPostgresServiceEnvNoReset(t)
	userClient := v1connect.NewUserServiceClient(&http.Client{Timeout: 5 * time.Second}, env.BaseURL)

	unique := fmt.Sprintf("%d", time.Now().UnixNano())
	// The audit row's resource column is resolved from user.name, so it names the
	// row this test wrote and no other.
	oversizedMarker := "users/audit-body-probe-" + unique
	boundedMarker := "users/audit-payload-probe-" + unique
	multiByteMarker := "users/audit-utf8-probe-" + unique

	t.Run("an oversized body is refused before it reaches the ledger", func(t *testing.T) {
		_, err := userClient.CreateUser(ctx, connect.NewRequest(&v1pb.CreateUserRequest{
			User: &v1pb.User{
				Name:     oversizedMarker,
				Email:    "audit-body-" + unique + "@example.com",
				Title:    strings.Repeat("t", 512<<10),
				Password: "Integration-Pass-1!",
				UserType: v1pb.UserType_END_USER,
			},
		}))
		require.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err), "unexpected error: %v", err)

		var rows int
		require.NoError(t, env.Store.GetDB().QueryRowContext(ctx, `
			SELECT COUNT(*) FROM audit_log
			WHERE payload->>'method' = $1 AND payload->>'resource' = $2`,
			createUserMethod, oversizedMarker).Scan(&rows))
		require.Zero(t, rows, "a refused body must not become an audit row")
	})

	t.Run("a request under the body cap still yields a bounded row", func(t *testing.T) {
		// The title trips validateUserTitle and the address is malformed, but a
		// rejected request still leaves an audit row — that is exactly the case
		// H6 described.
		_, err := userClient.CreateUser(ctx, connect.NewRequest(&v1pb.CreateUserRequest{
			User: &v1pb.User{
				Name:     boundedMarker,
				Email:    "not-an-address",
				Title:    strings.Repeat("t", 12<<10),
				Password: "Integration-Pass-1!",
				UserType: v1pb.UserType_END_USER,
			},
		}))
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "unexpected error: %v", err)

		var title string
		var payloadSize int
		require.NoError(t, env.Store.GetDB().QueryRowContext(ctx, `
			SELECT payload->'request'->'user'->>'title', length(payload::text)
			FROM audit_log
			WHERE payload->>'method' = $1 AND payload->>'resource' = $2
			ORDER BY id DESC
			LIMIT 1`,
			createUserMethod, boundedMarker).Scan(&title, &payloadSize),
			"the rejected request must still be recorded")

		require.Equal(t, audit.MaxAuditFieldBytes+len("...(truncated)"), len(title),
			"the field bound must cut the title short")
		// The response is nil on this path, so the row carries one payload.
		require.LessOrEqual(t, payloadSize, audit.MaxAuditPayloadBytes+64<<10,
			"the row must stay within the audit payload bound")
	})

	t.Run("a multi-byte field is truncated, not dropped", func(t *testing.T) {
		// Cutting 9000 bytes of three-byte runes at 8192 used to split a rune; the
		// invalid UTF-8 that produced made structpb and protojson reject the row,
		// so the request left no audit record at all. The row must survive.
		_, err := userClient.CreateUser(ctx, connect.NewRequest(&v1pb.CreateUserRequest{
			User: &v1pb.User{
				Name:     multiByteMarker,
				Email:    "not-an-address",
				Title:    strings.Repeat("名", 3000),
				Password: "Integration-Pass-1!",
				UserType: v1pb.UserType_END_USER,
			},
		}))
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "unexpected error: %v", err)

		var title string
		require.NoError(t, env.Store.GetDB().QueryRowContext(ctx, `
			SELECT payload->'request'->'user'->>'title'
			FROM audit_log
			WHERE payload->>'method' = $1 AND payload->>'resource' = $2
			ORDER BY id DESC
			LIMIT 1`,
			createUserMethod, multiByteMarker).Scan(&title),
			"a multi-byte field must not make the audit row disappear")

		require.True(t, utf8.ValidString(title), "the truncated title must stay valid UTF-8")
		require.True(t, strings.HasSuffix(title, "...(truncated)"))
		require.LessOrEqual(t, len(title), audit.MaxAuditFieldBytes+len("...(truncated)"))
	})
}
