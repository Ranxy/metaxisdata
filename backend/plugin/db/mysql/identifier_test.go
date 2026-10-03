package mysql

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestQualifiedIdentifier guards the sync path's SHOW CREATE statements: a
// catalog object named with a backtick must not be able to close the identifier
// and append another statement.
func TestQualifiedIdentifier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		database string
		object   string
		want     string
	}{
		{
			name:     "plain names",
			database: "db1",
			object:   "t1",
			want:     "`db1`.`t1`",
		},
		{
			name:     "backtick in object name",
			database: "db1",
			object:   "we`ird",
			want:     "`db1`.`we``ird`",
		},
		{
			name:     "backtick in database name",
			database: "d`b",
			object:   "t1",
			want:     "`d``b`.`t1`",
		},
		{
			name:     "statement injection attempt",
			database: "db1",
			object:   "x`; DROP TABLE t; -- ",
			want:     "`db1`.`x``; DROP TABLE t; -- `",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.want, qualifiedIdentifier(test.database, test.object))
		})
	}
}
