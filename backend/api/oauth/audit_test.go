package oauth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAuditedKeepsTheResponseIntact pins the two things the wrapper must never
// get wrong: the client's response is passed through untouched, and a missing
// workspace store (the hermetic case) skips the ledger instead of panicking.
func TestAuditedKeepsTheResponseIntact(t *testing.T) {
	t.Parallel()

	server := NewServer(ServerConfig{Endpoints: testEndpoints})
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The endpoints communicate what the ledger should say through the
		// request context, because the wrapper runs after them.
		recordAuditActor(r, "user@example.com")
		recordAuditDetail(r, map[string]any{"clientId": "client-1"})
		require.NotNil(t, auditRecorderOf(r), "the wrapper must install a recorder")

		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("body"))
	})

	recorder := httptest.NewRecorder()
	server.Audited(inner).ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/oauth/token", nil))

	require.Equal(t, http.StatusTeapot, recorder.Code)
	require.Equal(t, "body", recorder.Body.String())
}

func TestStatusWriterDefaultsToOK(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	writer := &statusWriter{ResponseWriter: recorder}
	_, err := writer.Write([]byte("body"))
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, writer.status, "a handler that never calls WriteHeader answered 200")
	require.Equal(t, http.StatusOK, recorder.Code)
}
