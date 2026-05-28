package store

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

type auditLogScannerStub struct {
	id        int64
	createdAt time.Time
	payload   []byte
}

func (s auditLogScannerStub) Scan(dest ...any) error {
	*(dest[0].(*int64)) = s.id
	*(dest[1].(*time.Time)) = s.createdAt
	*(dest[2].(*[]byte)) = s.payload
	return nil
}

func TestScanAuditLog(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.May, 28, 12, 34, 56, 0, time.UTC)
	payload := []byte(`{
		"parent": "workspaces/default",
		"method": "/metaxisdata.v1.AuthService/Login",
		"resource": "users/101",
		"user": "users/101",
		"severity": "INFO",
		"request": {"email": "alice@example.com"},
		"status": {"code": 0, "message": "ok"},
		"latencyMs": "42"
	}`)

	auditLog, err := scanAuditLog(auditLogScannerStub{id: 7, createdAt: createdAt, payload: payload})
	require.NoError(t, err)
	require.Equal(t, int64(7), auditLog.GetId())
	require.Equal(t, "workspaces/default", auditLog.GetParent())
	require.Equal(t, "/metaxisdata.v1.AuthService/Login", auditLog.GetMethod())
	require.Equal(t, "users/101", auditLog.GetResource())
	require.Equal(t, storepb.AuditSeverity_INFO, auditLog.GetSeverity())
	require.Equal(t, timestamppb.New(createdAt).AsTime(), auditLog.GetCreateTime().AsTime())
	require.Equal(t, "alice@example.com", auditLog.GetRequest().GetFields()["email"].GetStringValue())
	require.Equal(t, int64(42), auditLog.GetLatencyMs())
}
