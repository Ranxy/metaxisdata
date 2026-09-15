// Package env reads the METAXISDATA_* environment, which is where a project's
// analysis scopes come from.
//
// Scopes are deliberately never persisted. Several agents can share a machine
// while serving different projects, and anything remembered between runs would
// be silently overwritten by whichever agent ran last. Keeping the scopes in the
// process environment makes the isolation the caller's, which is where the
// decision belongs.
package env

import (
	"strings"

	"github.com/pkg/errors"
)

// The environment variables the CLI reads.
const (
	// ServerEnv is the server address. It is saved on the first `auth login
	// --server`, so later commands do not need it.
	ServerEnv = "METAXISDATA_SERVER"
	// TokenEnv injects a bearer token directly, for CI.
	TokenEnv = "METAXISDATA_TOKEN"
	// ScopesEnv lists the analysis scopes: comma separated entries, each either
	// a GUID or "name=guid".
	ScopesEnv = "METAXISDATA_SCOPES"
	// ConfigEnv points at the credentials file to use, skipping the default
	// location. This is how one machine holds several identities.
	ConfigEnv = "METAXISDATA_CONFIG"
	// ServiceKeyEnv carries a service account's key for the service-account
	// login path.
	ServiceKeyEnv = "METAXISDATA_SERVICE_KEY"
)

// Scope is one named analysis scope. The name is an opaque label used to
// attribute results; the GUID is what the server resolves the statement
// against.
type Scope struct {
	Name string
	GUID string
}

// ParseScopes reads the METAXISDATA_SCOPES format: comma separated entries,
// each a bare GUID or "name=guid". GUIDs use ";" as their own separator, so the
// two never collide.
func ParseScopes(raw string) ([]Scope, error) {
	var scopes []Scope
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		name, guid := "", entry
		if before, after, found := strings.Cut(entry, "="); found {
			name, guid = strings.TrimSpace(before), strings.TrimSpace(after)
			if name == "" {
				return nil, errors.Errorf("scope %q has an empty name", entry)
			}
		}
		if guid == "" {
			return nil, errors.Errorf("scope %q has an empty GUID", entry)
		}
		scopes = append(scopes, Scope{Name: name, GUID: guid})
	}
	return scopes, nil
}

// Lookup returns the scope with the given name, and whether it exists. A bare
// GUID is not a name, so a typo cannot silently resolve to something else.
func Lookup(scopes []Scope, name string) (Scope, bool) {
	for _, scope := range scopes {
		if scope.Name == name {
			return scope, true
		}
	}
	return Scope{}, false
}

// Select resolves the values of --scope against the configured scopes. A value
// that is a GUID is taken as-is, since a caller who pastes one knows what it
// is; anything else has to name a configured scope. "all" selects every
// configured scope.
func Select(configured []Scope, values []string) ([]Scope, error) {
	if len(values) == 0 {
		return configured, nil
	}

	var selected []Scope
	seen := map[string]bool{}
	add := func(scope Scope) {
		key := scope.GUID
		if key == "" {
			key = scope.Name
		}
		if seen[key] {
			return
		}
		seen[key] = true
		selected = append(selected, scope)
	}

	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if part == "all" {
				for _, scope := range configured {
					add(scope)
				}
				continue
			}
			if strings.Contains(part, ";") {
				add(Scope{GUID: part})
				continue
			}
			scope, ok := Lookup(configured, part)
			if !ok {
				return nil, errors.Errorf("unknown scope %q: set %s to define it, or pass the GUID directly", part, ScopesEnv)
			}
			add(scope)
		}
	}
	return selected, nil
}
