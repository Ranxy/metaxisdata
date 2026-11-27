package store

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSubagentNameBase(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		target string
		want   string
	}{
		{"object segment", "i;d;public;orders", "orders"},
		{"hyphens and case", "i;d;public;Order_Items", "order-items"},
		{"long name truncates", "i;d;public;this_is_a_very_long_table_name_that_exceeds_the_limit", "this-is-a-very-long-tabl"},
		{"non ascii falls back", "i;d;public;订单", "target"},
		{"leading digit falls back", "i;d;public;123_orders", "target"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, subagentNameBase(tc.target))
		})
	}
}
