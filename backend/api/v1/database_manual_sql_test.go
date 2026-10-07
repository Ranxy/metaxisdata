package v1

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// Every tag and every attribute of a manual SQL entry is its own row, and
// create/update is in the member baseline, so the count is what keeps one 4 MiB
// request from writing hundreds of thousands of rows. The boundary is asserted
// both ways so a silent change to the limit fails here.
func TestValidateManualSQLCollections(t *testing.T) {
	t.Parallel()

	tags := make([]string, maxManualSQLTags)
	attributes := make(map[string]string, maxManualSQLAttributes)
	for i := range maxManualSQLTags {
		tags[i] = fmt.Sprintf("tag-%d", i)
	}
	for i := range maxManualSQLAttributes {
		attributes[fmt.Sprintf("key-%d", i)] = "value"
	}

	require.NoError(t, validateManualSQLCollections(tags, attributes), "the limits themselves are allowed")
	require.NoError(t, validateManualSQLCollections(nil, nil))

	tooManyTags := append(append([]string{}, tags...), "one-more")
	err := validateManualSQLCollections(tooManyTags, attributes)
	require.Error(t, err)
	require.Contains(t, err.Error(), fmt.Sprintf("at most %d tags", maxManualSQLTags))

	tooManyAttributes := make(map[string]string, len(attributes)+1)
	for key, value := range attributes {
		tooManyAttributes[key] = value
	}
	tooManyAttributes["one-more"] = "value"
	err = validateManualSQLCollections(tags, tooManyAttributes)
	require.Error(t, err)
	require.Contains(t, err.Error(), fmt.Sprintf("at most %d attributes", maxManualSQLAttributes))
}
