package dataquality

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

func TestCheckTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		seconds int32
		want    time.Duration
	}{
		{name: "unset reads as the default", seconds: 0, want: 60 * time.Second},
		{name: "an override is used", seconds: 300, want: 300 * time.Second},
		{name: "a negative row reads as the default", seconds: -5, want: 60 * time.Second},
		{name: "an out-of-band row is clamped", seconds: 3600, want: 300 * time.Second},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := &storepb.QualityRuleConfig{TimeoutSeconds: test.seconds}
			require.Equal(t, test.want, CheckTimeout(config))
		})
	}
	require.Equal(t, 60*time.Second, CheckTimeout(nil))
}

func TestValidateCheckTimeout(t *testing.T) {
	t.Parallel()

	require.NoError(t, validateCheckTimeout(0))
	require.NoError(t, validateCheckTimeout(1))
	require.NoError(t, validateCheckTimeout(300))

	for _, seconds := range []int32{-1, 301, 100000} {
		err := validateCheckTimeout(seconds)
		require.Error(t, err, "timeout_seconds %d", seconds)
		require.Equal(t, common.Invalid, common.ErrorCode(err))
		require.Contains(t, err.Error(), "timeout_seconds must be between 1 and 300")
	}
}

// The config gate refuses an out-of-range override before the rule is saved.
func TestValidateConfigRefusesAnOutOfRangeTimeout(t *testing.T) {
	t.Parallel()

	target := compileTestTarget(storepb.Engine_MYSQL)
	config := &storepb.QualityRuleConfig{
		Config:         &storepb.QualityRuleConfig_RowCount{RowCount: &storepb.RowCountConfig{MinRows: ptrInt64(1)}},
		TimeoutSeconds: 301,
	}
	err := ValidateConfig(target, storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, config)
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))
}
