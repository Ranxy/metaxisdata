package dataquality

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// The checked column is read from the config arm, not from the target path
// snapshot (which may be empty) and not from the referenced-column list (which
// also carries filter columns and drift snapshots).
func TestConfigColumn(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		config *storepb.QualityRuleConfig
		want   string
	}{
		{name: "nil", config: nil, want: ""},
		{
			name: "null percent",
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_NullPercent{
				NullPercent: &storepb.NullPercentConfig{Column: " email "},
			}},
			want: "email",
		},
		{
			name: "unique",
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Unique{
				Unique: &storepb.UniqueConfig{Column: "id"},
			}},
			want: "id",
		},
		{
			name: "accepted values",
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_AcceptedValues{
				AcceptedValues: &storepb.AcceptedValuesConfig{Column: "status"},
			}},
			want: "status",
		},
		{
			name: "range",
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Range{
				Range: &storepb.RangeConfig{Column: "amount"},
			}},
			want: "amount",
		},
		{
			name: "regex",
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Regex{
				Regex: &storepb.RegexConfig{Column: "email"},
			}},
			want: "email",
		},
		{
			name: "string length",
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_StringLength{
				StringLength: &storepb.StringLengthConfig{Column: "name"},
			}},
			want: "name",
		},
		{
			name: "referential integrity",
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_ReferentialIntegrity{
				ReferentialIntegrity: &storepb.ReferentialIntegrityConfig{ChildColumn: "customer_id"},
			}},
			want: "customer_id",
		},
		{
			name: "freshness",
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Freshness{
				Freshness: &storepb.FreshnessConfig{TimestampColumn: "updated_at"},
			}},
			want: "updated_at",
		},
		{
			name: "aggregate",
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Aggregate{
				Aggregate: &storepb.AggregateConfig{Column: "amount"},
			}},
			want: "amount",
		},
		{
			name: "row count is table-level",
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCount{
				RowCount: &storepb.RowCountConfig{},
			}},
			want: "",
		},
		{
			name: "unique combination names several columns",
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_UniqueCombination{
				UniqueCombination: &storepb.UniqueCombinationConfig{Columns: []string{"a", "b"}},
			}},
			want: "",
		},
		{
			name: "cross column is not a single column",
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_CrossColumn{
				CrossColumn: &storepb.CrossColumnConfig{LeftColumn: "a", RightColumn: "b"},
			}},
			want: "",
		},
		{
			name: "schema drift is table-level",
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_SchemaDrift{
				SchemaDrift: &storepb.SchemaDriftConfig{},
			}},
			want: "",
		},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, ConfigColumn(tc.config), tc.name)
	}
}
