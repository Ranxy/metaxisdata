//nolint:revive
package common

import (
	"strings"
)

func GetInstanceFromGUID(guid string) (string, bool) {
	index := strings.Index(guid, MetaGUIDSplit)
	if index == -1 {
		return "", false
	}
	return guid[:index], true
}

func GetSchemaFromGUID(guid string) (string, bool) {
	list := strings.Split(guid, MetaGUIDSplit)
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
