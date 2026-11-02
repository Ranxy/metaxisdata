package dataquality

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/Ranxy/metaxisdata/backend/common"
)

const (
	// maxCodeTableValues bounds one workspace code table. The list is stored as a
	// JSONB array and loaded whole for every execution of a referencing rule, so it
	// is a bounded working set rather than an arbitrary import.
	maxCodeTableValues = 5000
	// maxCodeTableValueBytes bounds one accepted code. A code table holds codes, not
	// free text; the limit keeps a typo from storing a whole row's worth of text.
	maxCodeTableValueBytes = 256
)

// NormalizeCodeTableValues turns a caller's code list into the stored form: each
// value is trimmed, empty values are dropped, duplicates collapse and the result
// is sorted. Trimming and de-duplication make an import idempotent, and sorting
// makes the stored payload deterministic.
//
// A value that is too long or not valid UTF-8 is refused rather than truncated:
// silently shortening a code would make it match nothing, and JSONB cannot carry
// invalid UTF-8 at all.
func NormalizeCodeTableValues(values []string) ([]string, error) {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		// JSONB strings must be valid UTF-8; protojson refuses anything else, so the
		// save path must refuse it first with a message about the value.
		if !utf8.ValidString(value) {
			return nil, invalidConfig("a code table value is not valid UTF-8")
		}
		// JSONB cannot represent U+0000: PostgreSQL rejects the escape and the whole
		// write fails. Refuse the value here, where the message can name the problem,
		// instead of letting it surface as an internal write error.
		if strings.ContainsRune(value, '\x00') {
			return nil, invalidConfig("a code table value must not contain a NUL character")
		}
		if len(value) > maxCodeTableValueBytes {
			return nil, invalidConfig("a code table value is %d bytes, above the maximum of %d", len(value), maxCodeTableValueBytes)
		}
		normalized = append(normalized, value)
	}
	// Deduplicate before the count check: repeated entries do not enlarge the table
	// a rule has to load.
	slices.Sort(normalized)
	normalized = slices.Compact(normalized)
	if len(normalized) > maxCodeTableValues {
		return nil, common.Errorf(common.Invalid,
			"a code table allows at most %d values, got %d", maxCodeTableValues, len(normalized))
	}
	return normalized, nil
}
