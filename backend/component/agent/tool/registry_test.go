package tool

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// noopExecute is a valid body for registration tests.
func noopExecute(context.Context, Call, *Invocation) (Result, error) {
	return Result{}, nil
}

// validTool is a tool that passes every registration rule.
func validTool(name string) Tool {
	return Tool{
		Name:        name,
		Description: "does something",
		Parameters:  map[string]any{"type": "object"},
		Meta: Meta{
			Group:      "test",
			Scope:      ScopeCatalog,
			Authz:      AuthzPermission,
			Permission: "test.read",
		},
		Execute: noopExecute,
	}
}

func TestRegisterAcceptsACompleteTool(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	require.NoError(t, registry.Register(validTool("one")))
	require.Equal(t, 1, registry.Len())

	registered, ok := registry.Get("one")
	require.True(t, ok)
	require.Equal(t, "does something", registered.Description)
}

func TestRegisterRejectsInvalidDeclarations(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		mutate func(*Tool)
		want   string
	}{
		{name: "missing name", mutate: func(tool *Tool) { tool.Name = "" }, want: "needs a name"},
		{name: "padded name", mutate: func(tool *Tool) { tool.Name = " one " }, want: "whitespace"},
		{name: "missing description", mutate: func(tool *Tool) { tool.Description = "  " }, want: "needs a description"},
		{name: "missing parameters", mutate: func(tool *Tool) { tool.Parameters = nil }, want: "parameter schema"},
		{name: "missing execute", mutate: func(tool *Tool) { tool.Execute = nil }, want: "execute function"},
		{name: "missing group", mutate: func(tool *Tool) { tool.Meta.Group = "" }, want: "metadata group"},
		{name: "permission missing", mutate: func(tool *Tool) { tool.Meta.Permission = "" }, want: "declares no permission"},
		{name: "internal not product only", mutate: func(tool *Tool) {
			tool.Meta.Authz = AuthzInternal
			tool.Meta.Permission = ""
		}, want: "not product-only"},
		{name: "internal with permission", mutate: func(tool *Tool) {
			tool.Meta.Authz = AuthzInternal
			tool.Meta.Scope = ScopeProductOnly
		}, want: "declares a permission"},
		{name: "unknown authz", mutate: func(tool *Tool) { tool.Meta.Authz = AuthzMode(99) }, want: "unknown authorization mode"},
		{name: "unknown scope", mutate: func(tool *Tool) { tool.Meta.Scope = Scope(99) }, want: "unknown scope"},
		{name: "read only with side effect", mutate: func(tool *Tool) {
			tool.Meta.ReadOnly = true
			tool.Meta.SideEffect = SideEffectPropose
		}, want: "read-only but declares a side effect"},
		{name: "parallel write", mutate: func(tool *Tool) { tool.Meta.ParallelSafe = true }, want: "parallel-safe but not read-only"},
		{name: "negative budget", mutate: func(tool *Tool) { tool.Meta.MaxResultBytes = -1 }, want: "negative result budget"},
		{name: "negative timeout", mutate: func(tool *Tool) { tool.Meta.Timeout = -time.Second }, want: "negative timeout"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tool := validTool("one")
			tc.mutate(&tool)
			err := NewRegistry().Register(tool)
			require.Error(t, err)
			require.True(t, strings.Contains(err.Error(), tc.want), "error %q should mention %q", err, tc.want)
		})
	}
}

func TestRegisterRejectsDuplicateName(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	require.NoError(t, registry.Register(validTool("one")))
	require.ErrorContains(t, registry.Register(validTool("one")), "already registered")
}

func TestCatalogExcludesProductOnlyTools(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	catalogRead := validTool("catalog_read")
	catalogRead.Meta.ReadOnly = true
	require.NoError(t, registry.Register(catalogRead))

	catalogWrite := validTool("catalog_write")
	catalogWrite.Meta.Permission = "test.write"
	catalogWrite.Meta.SideEffect = SideEffectPropose
	require.NoError(t, registry.Register(catalogWrite))

	internal := validTool("wake_control")
	internal.Meta.Authz = AuthzInternal
	internal.Meta.Scope = ScopeProductOnly
	internal.Meta.Permission = ""
	require.NoError(t, registry.Register(internal))

	catalog := registry.Catalog()
	require.Equal(t, []string{"catalog_read", "catalog_write"}, namesOf(catalog))

	readOnly := registry.ReadOnly()
	require.Equal(t, []string{"catalog_read"}, namesOf(readOnly))
}

func TestResolveKeepsSpecOrderAndRejectsBadSpecs(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	require.NoError(t, registry.Register(validTool("one")))
	require.NoError(t, registry.Register(validTool("two")))

	set, err := registry.Resolve([]string{"two", "one"})
	require.NoError(t, err)
	require.Equal(t, []string{"two", "one"}, set.Names())

	_, err = registry.Resolve(nil)
	require.ErrorContains(t, err, "at least one tool")

	_, err = registry.Resolve([]string{"two", "two"})
	require.ErrorContains(t, err, "listed twice")

	_, err = registry.Resolve([]string{"two", "missing"})
	require.ErrorContains(t, err, "unregistered tool")
}

func TestSchemasComeFromTheResolvedSet(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	require.NoError(t, registry.Register(validTool("two")))
	require.NoError(t, registry.Register(validTool("one")))
	set, err := registry.Resolve([]string{"one", "two"})
	require.NoError(t, err)

	schemas := set.Schemas()
	require.Len(t, schemas, 2)
	require.Equal(t, "one", schemas[0].Name)
	require.Equal(t, "does something", schemas[0].Description)
	require.NotNil(t, schemas[0].Parameters)

	_, ok := set.Tool("two")
	require.True(t, ok)
	_, ok = set.Tool("missing")
	require.False(t, ok)
}

func namesOf(tools []Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}

func TestSetReportsParallelSafety(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	read := validTool("read")
	read.Meta.ReadOnly = true
	read.Meta.ParallelSafe = true
	require.NoError(t, registry.Register(read))

	write := validTool("write")
	write.Meta.Permission = "test.write"
	write.Meta.SideEffect = SideEffectPropose
	require.NoError(t, registry.Register(write))

	set, err := registry.Resolve([]string{"read", "write"})
	require.NoError(t, err)
	require.True(t, set.IsParallelSafe("read"))
	require.False(t, set.IsParallelSafe("write"))
	require.False(t, set.IsParallelSafe("missing"))
}
