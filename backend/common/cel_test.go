//nolint:revive
package common

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEvalBindingCondition(t *testing.T) {
	t.Parallel()

	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		expr    string
		want    bool
		wantErr bool
	}{
		{name: "an empty condition is satisfied", expr: "", want: true},
		{name: "request.time is bound", expr: `request.time > timestamp("2020-01-01T00:00:00Z")`, want: true},
		{name: "request.time can make a condition false", expr: `request.time < timestamp("2020-01-01T00:00:00Z")`, want: false},
		// A condition referencing a variable that is not bound cannot be
		// decided. Returning true there would grant the binding globally, so it
		// must be an error.
		{name: "an unbound database fails closed", expr: `resource.database == "hr"`, wantErr: true},
		{name: "an unbound schema fails closed", expr: `resource.schema_name == "public"`, wantErr: true},
		{name: "an unbound table method fails closed", expr: `resource.table_name.startsWith("secret")`, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ok, err := EvalBindingCondition(tc.expr, now)
			if tc.wantErr {
				require.Error(t, err)
				require.False(t, ok, "an undecidable condition must not be reported as satisfied")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, ok)
		})
	}
}
