package resource

import (
	"slices"
	"strings"

	"github.com/Ranxy/metaxisdata/backend/common"
)

// Scope is a resource plus its subtree. A metadata scope is a GUID prefix, so a
// database scope covers every schema, table and column under it; a non-metadata
// scope covers exactly its own resource.
type Scope struct {
	Type Type
	ID   string
}

// NewScope builds one scope.
func NewScope(t Type, id string) Scope {
	return Scope{Type: t, ID: id}
}

// Ref names the scoped resource itself.
func (s Scope) Ref() Ref {
	return Ref(s)
}

// Valid reports whether the scope could name a resource subtree.
func (s Scope) Valid() bool {
	return s.Ref().Valid()
}

// Contains reports whether ref is the scoped resource or lives under it.
func (s Scope) Contains(ref Ref) bool {
	if !s.Valid() || !ref.Valid() {
		return false
	}
	if s.Type == TypeWorkspace {
		return true
	}
	if IsMetadataType(s.Type) && IsMetadataType(ref.Type) {
		return metadataPrefixContains(s.ID, ref.ID)
	}
	return s.Type == ref.Type && s.ID == ref.ID
}

// ContainsRefs reports whether any reference in the chain is in the scope. A
// caller that starts from a derived resource passes its ancestor chain.
func (s Scope) ContainsRefs(refs []Ref) bool {
	for _, ref := range refs {
		if s.Contains(ref) {
			return true
		}
	}
	return false
}

// String renders the scope for logs and test failures.
func (s Scope) String() string {
	if s.Type == TypeWorkspace {
		return "workspace"
	}
	return s.Type.String() + ":" + s.ID
}

// ScopeSet is what a principal may act on for one permission. All means a
// workspace-wide grant covers every resource; Self names the principal whose own
// rows a self-scoped list may return; Scopes is the explicit subtree set.
type ScopeSet struct {
	All    bool
	Self   *int32
	Scopes []Scope
}

// IsEmpty reports whether the set allows nothing.
func (s ScopeSet) IsEmpty() bool {
	return !s.All && s.Self == nil && len(s.Scopes) == 0
}

// Contains reports whether ref is inside the set. It checks the reference
// itself; use ContainsRefs with an ancestor chain for a derived resource.
func (s ScopeSet) Contains(ref Ref) bool {
	return s.ContainsRefs([]Ref{ref})
}

// ContainsRefs reports whether any reference in the chain is inside the set.
func (s ScopeSet) ContainsRefs(refs []Ref) bool {
	if s.All {
		return true
	}
	for _, scope := range s.Scopes {
		if scope.ContainsRefs(refs) {
			return true
		}
	}
	return false
}

// Add appends one scope. A workspace scope widens the set to All; an invalid
// scope is ignored.
func (s *ScopeSet) Add(scope Scope) {
	if !scope.Valid() {
		return
	}
	if scope.Type == TypeWorkspace {
		s.All = true
		return
	}
	s.Scopes = append(s.Scopes, scope)
}

// Union adds every scope of other. The self scope is kept once; two different
// self ids would describe two principals and are not a meaningful union.
func (s *ScopeSet) Union(other ScopeSet) {
	if other.All {
		s.All = true
	}
	if s.Self == nil {
		s.Self = other.Self
	}
	s.Scopes = append(s.Scopes, other.Scopes...)
}

// Normalize returns the set with duplicate scopes removed, narrower scopes
// subsumed by a broader one dropped, and the remainder sorted. A workspace scope
// becomes All.
func (s ScopeSet) Normalize() ScopeSet {
	if s.All {
		return ScopeSet{All: true}
	}
	out := ScopeSet{Self: s.Self}
	seen := make(map[string]struct{}, len(s.Scopes))
	scopes := make([]Scope, 0, len(s.Scopes))
	for _, scope := range s.Scopes {
		if !scope.Valid() {
			continue
		}
		if scope.Type == TypeWorkspace {
			return ScopeSet{All: true}
		}
		key := scope.Type.String() + "\x00" + scope.ID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		scopes = append(scopes, scope)
	}
	for i, scope := range scopes {
		subsumed := false
		for j, other := range scopes {
			if i == j {
				continue
			}
			if other.Contains(scope.Ref()) && !scope.Contains(other.Ref()) {
				subsumed = true
				break
			}
		}
		if !subsumed {
			out.Scopes = append(out.Scopes, scope)
		}
	}
	slices.SortFunc(out.Scopes, func(a, b Scope) int {
		if c := strings.Compare(a.Type.String(), b.Type.String()); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out
}

// MetadataScopes returns the normalized metadata scopes, for a list whose filter
// is a metadata GUID prefix. Workspace-wide access is All, not a scope.
func (s ScopeSet) MetadataScopes() []Scope {
	if s.All {
		return nil
	}
	normalized := s.Normalize()
	scopes := make([]Scope, 0, len(normalized.Scopes))
	for _, scope := range normalized.Scopes {
		if IsMetadataType(scope.Type) {
			scopes = append(scopes, scope)
		}
	}
	return scopes
}

// metadataPrefixContains reports whether a metadata GUID is the prefix or lives
// under it. GUID parts are escaped, so the separator cannot appear inside a
// part and a raw prefix match is exact.
func metadataPrefixContains(prefix, guid string) bool {
	if prefix == guid {
		return true
	}
	return strings.HasPrefix(guid, prefix+common.MetaGUIDSplit)
}
