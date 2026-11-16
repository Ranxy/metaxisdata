package tool

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
)

// Registry holds every tool a process registered. Registration is static: it
// happens once at wiring time, and a validation failure is a boot error rather
// than a runtime surprise.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
	order []string
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register adds one tool. A duplicate name, an incomplete declaration or a
// contradictory one is refused.
func (r *Registry) Register(t Tool) error {
	if strings.TrimSpace(t.Name) == "" {
		return errors.New("a tool needs a name")
	}
	if t.Name != strings.TrimSpace(t.Name) {
		return fmt.Errorf("tool %q has surrounding whitespace in its name", t.Name)
	}
	if strings.TrimSpace(t.Description) == "" {
		return fmt.Errorf("tool %q needs a description", t.Name)
	}
	if t.Parameters == nil {
		return fmt.Errorf("tool %q needs a parameter schema", t.Name)
	}
	if t.Execute == nil {
		return fmt.Errorf("tool %q needs an execute function", t.Name)
	}
	if strings.TrimSpace(t.Meta.Group) == "" {
		return fmt.Errorf("tool %q needs a metadata group", t.Name)
	}
	if err := validateMeta(t); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[t.Name]; exists {
		return fmt.Errorf("tool %q is already registered", t.Name)
	}
	r.tools[t.Name] = t
	r.order = append(r.order, t.Name)
	return nil
}

// validateMeta refuses a declaration the pipeline could not enforce.
func validateMeta(t Tool) error {
	// A switch with every case explicit keeps a future auth mode from silently
	// falling into the permission path.
	switch t.Meta.Authz {
	case AuthzPermission:
		if strings.TrimSpace(t.Meta.Permission) == "" {
			return fmt.Errorf("tool %q is permission-authorized but declares no permission", t.Name)
		}
	case AuthzAction:
		if strings.TrimSpace(t.Meta.Action) == "" {
			return fmt.Errorf("tool %q is action-authorized but declares no action", t.Name)
		}
	case AuthzInternal:
		if t.Meta.Scope != ScopeProductOnly {
			return fmt.Errorf("tool %q is internal but is not product-only", t.Name)
		}
		if t.Meta.Permission != "" || t.Meta.Action != "" {
			return fmt.Errorf("tool %q is internal but declares a permission or action", t.Name)
		}
	default:
		return fmt.Errorf("tool %q has an unknown authorization mode", t.Name)
	}

	switch t.Meta.Scope {
	case ScopeCatalog:
	case ScopeProductOnly:
	default:
		return fmt.Errorf("tool %q has an unknown scope", t.Name)
	}

	if t.Meta.ReadOnly && t.Meta.SideEffect != SideEffectNone {
		return fmt.Errorf("tool %q is read-only but declares a side effect", t.Name)
	}
	if t.Meta.ParallelSafe && !t.Meta.ReadOnly {
		return fmt.Errorf("tool %q is parallel-safe but not read-only", t.Name)
	}
	if t.Meta.MaxResultBytes < 0 {
		return fmt.Errorf("tool %q has a negative result budget", t.Name)
	}
	if t.Meta.Timeout < 0 {
		return fmt.Errorf("tool %q has a negative timeout", t.Name)
	}
	return nil
}

// Get returns one registered tool.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// Names returns every registered name in registration order.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return slices.Clone(r.order)
}

// Len returns how many tools are registered.
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.order)
}

// Catalog returns the shared-catalog tools in registration order. A product-only
// tool is never part of it, so no other front end can reach it through this
// method.
func (r *Registry) Catalog() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tool, 0, len(r.order))
	for _, name := range r.order {
		if t := r.tools[name]; t.Meta.Scope == ScopeCatalog {
			out = append(out, t)
		}
	}
	return out
}

// ReadOnly returns the catalog tools safe to expose over the read-only MCP
// surface.
func (r *Registry) ReadOnly() []Tool {
	all := r.Catalog()
	out := make([]Tool, 0, len(all))
	for _, t := range all {
		if t.Meta.ReadOnly {
			out = append(out, t)
		}
	}
	return out
}

// Set is one product's resolved tool set. Schemas and execution come from the
// same set, so what the model sees and what can run cannot disagree.
type Set struct {
	tools  []Tool
	byName map[string]Tool
}

// Resolve turns a product spec's explicit name whitelist into a Set. An empty
// list, a duplicate and an unknown name are all errors: a typo in a spec must
// fail at boot, not surface as "unknown tool" to the model.
func (r *Registry) Resolve(names []string) (*Set, error) {
	if len(names) == 0 {
		return nil, errors.New("a tool set needs at least one tool")
	}
	seen := make(map[string]bool, len(names))
	set := &Set{
		tools:  make([]Tool, 0, len(names)),
		byName: make(map[string]Tool, len(names)),
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, name := range names {
		if seen[name] {
			return nil, fmt.Errorf("tool %q is listed twice in the same spec", name)
		}
		seen[name] = true
		t, ok := r.tools[name]
		if !ok {
			return nil, fmt.Errorf("the spec names unregistered tool %q", name)
		}
		set.tools = append(set.tools, t)
		set.byName[name] = t
	}
	return set, nil
}

// Len returns how many tools the set exposes.
func (s *Set) Len() int {
	if s == nil {
		return 0
	}
	return len(s.tools)
}

// Names returns the set's tool names in spec order.
func (s *Set) Names() []string {
	if s == nil {
		return nil
	}
	out := make([]string, 0, len(s.tools))
	for _, t := range s.tools {
		out = append(out, t.Name)
	}
	return out
}

// Schemas returns the model-facing declarations in spec order.
func (s *Set) Schemas() []Schema {
	if s == nil {
		return nil
	}
	out := make([]Schema, 0, len(s.tools))
	for _, t := range s.tools {
		out = append(out, Schema{Name: t.Name, Description: t.Description, Parameters: t.Parameters})
	}
	return out
}

// IsParallelSafe reports whether the named tool may run concurrently with the
// other parallel-safe calls of the same model batch.
func (s *Set) IsParallelSafe(name string) bool {
	if s == nil {
		return false
	}
	t, ok := s.byName[name]
	return ok && t.Meta.ParallelSafe
}

// Tool returns one tool of the set.
func (s *Set) Tool(name string) (Tool, bool) {
	if s == nil {
		return Tool{}, false
	}
	t, ok := s.byName[name]
	return t, ok
}
