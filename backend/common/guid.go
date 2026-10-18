//nolint:revive
package common

import (
	"strings"
)

const (
	// guidPartEscape replaces the GUID separator inside a single segment.
	guidPartEscape = "%3B"
	// guidPartPercentEscape replaces the escape character itself. Escaping it
	// first is what makes the encoding injective: a name with a literal "%3B"
	// becomes "%253B" and cannot collide with a name that contains ";".
	guidPartPercentEscape = "%25"
)

// EscapeGUIDPart escapes the two characters with a meaning inside a GUID (the
// separator and the escape character) so an escaped name cannot shift the fields
// a parser reads out of a GUID. Percent signs are escaped first, then the
// separator; names without either character keep the exact same GUID.
func EscapeGUIDPart(part string) string {
	part = strings.ReplaceAll(part, "%", guidPartPercentEscape)
	return strings.ReplaceAll(part, MetaGUIDSplit, guidPartEscape)
}

// UnescapeGUIDPart reverses EscapeGUIDPart. The escapes are decoded in the
// reverse order they were written (separator first, then the percent sign), so a
// literal "%3B" in a name comes back as "%3B" rather than as ";".
func UnescapeGUIDPart(part string) string {
	part = strings.ReplaceAll(part, guidPartEscape, MetaGUIDSplit)
	return strings.ReplaceAll(part, guidPartPercentEscape, "%")
}

// BuildMetaGUID joins segments into a meta GUID. Every builder must use it (and
// every parser SplitMetaGUID) so the escaping stays symmetric.
func BuildMetaGUID(parts ...string) string {
	escaped := make([]string, 0, len(parts))
	for _, part := range parts {
		escaped = append(escaped, EscapeGUIDPart(part))
	}
	return strings.Join(escaped, MetaGUIDSplit)
}

// SplitMetaGUID splits a meta GUID into its segments and restores the separator
// inside each one, so callers get the original names back.
func SplitMetaGUID(guid string) []string {
	parts := strings.Split(guid, MetaGUIDSplit)
	for i := range parts {
		parts[i] = UnescapeGUIDPart(parts[i])
	}
	return parts
}

func GetInstanceFromGUID(guid string) (string, bool) {
	index := strings.Index(guid, MetaGUIDSplit)
	if index == -1 {
		return "", false
	}
	return UnescapeGUIDPart(guid[:index]), true
}

func GetSchemaFromGUID(guid string) (string, bool) {
	list := SplitMetaGUID(guid)
	if len(list) < 3 {
		return "", false
	}
	return list[2], true
}

// GUIDPrefix returns the parent prefix of a GUID: the GUID without its last
// MetaGUIDSplit-separated segment. It returns "" when the GUID has no parent
// segment.
func GUIDPrefix(guid string) string {
	index := strings.LastIndex(guid, MetaGUIDSplit)
	if index == -1 {
		return ""
	}
	return guid[:index]
}
