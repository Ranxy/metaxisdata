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

// GUIDPrefix returns the prefix of a GUID up to the last dot.
func GUIDPrefix(guid string) string {
	lastDotIndex := strings.LastIndex(guid, ".")
	if lastDotIndex == -1 {
		return ""
	}
	return guid[:lastDotIndex]
}
