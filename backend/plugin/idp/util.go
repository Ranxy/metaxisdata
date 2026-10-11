// Package idp is the plugin for Identity Provider.
package idp

import "strings"

// GetValueWithKey returns the value of the key in the data. The key is a
// dot-separated path, so a nested claim such as "realm_access.roles" resolves.
func GetValueWithKey(data map[string]any, key string) any {
	keys := strings.Split(key, ".")
	value := data[keys[0]]

	if len(keys) > 1 {
		if subData, ok := value.(map[string]any); ok {
			return GetValueWithKey(subData, strings.Join(keys[1:], "."))
		}
		return nil
	}

	return value
}

// GroupValues turns a provider's group claim into a list of group names. It
// reports whether the claim's shape was understood.
//
// A JSON claim carrying groups is normally an array of strings. Some providers
// emit one delimited string instead — the `scope` convention, or a comma-joined
// list — so both are accepted. An unparsable shape is reported as not understood
// rather than as an empty list: an empty list is a provider saying "no groups",
// and acting on that would remove every membership of an account whose provider
// merely changed the claim's type.
func GroupValues(claim any) ([]string, bool) {
	switch value := claim.(type) {
	case nil:
		return nil, false
	case []any:
		groups := make([]string, 0, len(value))
		for _, entry := range value {
			if name, ok := entry.(string); ok && name != "" {
				groups = append(groups, name)
			}
		}
		return groups, true
	case []string:
		return value, true
	case string:
		return strings.FieldsFunc(value, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t' || r == '\n'
		}), true
	default:
		return nil, false
	}
}
