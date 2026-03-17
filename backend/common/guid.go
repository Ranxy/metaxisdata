//nolint:revive
package common

import (
	"strings"
)

func GetInstaceFromGUID(guid string) (string, bool) {
	index := strings.Index(guid, MetaGUIDSplit)
	if index == -1 {
		return "", false
	}
	return guid[:index], true
}

func GetDatabaseFromGUID(guid string) (string, bool) {
	list := strings.Split(guid, MetaGUIDSplit)
	if len(list) < 2 {
		return "", false
	}
	return list[1], true
}

func GetSchemaFromGUID(guid string) (string, bool) {
	list := strings.Split(guid, MetaGUIDSplit)
	if len(list) < 3 {
		return "", false
	}
	return list[2], true
}

// GUIDPrefix returns the prefix of a GUID up to the last dot.
func GUIDPrefix(guid string) string {
	lastDotIndex := strings.LastIndex(guid, ".")
	if lastDotIndex == -1 {
		return ""
	}
	return guid[:lastDotIndex]
}
