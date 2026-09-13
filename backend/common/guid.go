//nolint:revive
package common

import (
	"strings"
)

// guidPartEscape replaces the GUID separator inside a single segment. It is a
// no-op for names that do not contain the separator, so GUIDs of such names are
// unchanged by the escaping.
const guidPartEscape = "%3B"

// EscapeGUIDPart escapes the GUID separator inside one segment, so a name that
// contains ";" cannot shift the fields a parser reads out of a GUID.
func EscapeGUIDPart(part string) string {
	if !strings.Contains(part, MetaGUIDSplit) {
		return part
	}
	return strings.ReplaceAll(part, MetaGUIDSplit, guidPartEscape)
}

// UnescapeGUIDPart restores the separator escaped by EscapeGUIDPart.
func UnescapeGUIDPart(part string) string {
	if !strings.Contains(part, guidPartEscape) {
		return part
	}
	return strings.ReplaceAll(part, guidPartEscape, MetaGUIDSplit)
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
