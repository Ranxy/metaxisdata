package common

import (
	"strings"
)

func GetInstaceFromGuid(guid string) (string, bool) {
	index := strings.Index(guid, MetaGuidSplit)
	if index == -1 {
		return "", false
	}
	return guid[:index], true
}

// GuidPrefix returns the prefix of a GUID up to the last dot.
func GuidPrefix(guid string) string {
	lastDotIndex := strings.LastIndex(guid, ".")
	if lastDotIndex == -1 {
		return ""
	}
	return guid[:lastDotIndex]
}
