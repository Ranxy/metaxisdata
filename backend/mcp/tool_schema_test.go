package mcp

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestToolSchemasMatchTheirArgumentStructs reconciles the two hand-written halves
// of a tool's input contract: the JSON Schema advertised to the model, and the Go
// struct encoding/json decodes into. They used to drift silently — a key renamed
// in the schema but not the struct is ignored by encoding/json (the tool runs on a
// default), and a struct field renamed but not the schema is never sent. Neither
// produces an error a caller would see.
func TestToolSchemasMatchTheirArgumentStructs(t *testing.T) {
	t.Parallel()

	server := NewServer(Config{})
	definitions := map[string]toolDefinition{}
	for _, definition := range server.toolDefinitions() {
		definitions[definition.Name] = definition
	}

	cases := []struct {
		tool string
		args any
		// nested maps a schema property to the struct that decodes it. An array of
		// objects is described by its items type (analyze_sql's scopes).
		nested map[string]any
	}{
		{tool: "list_instances", args: listInstancesArgs{}},
		{tool: "list_databases", args: listDatabasesArgs{}},
		{tool: "search_metadata", args: searchMetadataArgs{}},
		{tool: "list_metadata", args: listMetadataArgs{}, nested: map[string]any{"parent": objectRef{}}},
		{tool: "get_metadata", args: getMetadataArgs{}, nested: map[string]any{"object": objectRef{}}},
		{tool: "get_ddl", args: getDDLArgs{}, nested: map[string]any{"object": objectRef{}}},
		{tool: "analyze_sql", args: analyzeSQLArgs{}, nested: map[string]any{"scopes": analysisScope{}}},
		{tool: "get_lineage_graph", args: getLineageGraphArgs{}, nested: map[string]any{"object": objectRef{}}},
		{tool: "whoami", args: struct{}{}},
	}

	// A tool added without an entry here would otherwise go unreconciled, which is
	// exactly the silent drift this test exists to stop.
	require.Len(t, cases, len(definitions), "every tool must name the struct that decodes its arguments")

	for _, testCase := range cases {
		definition, ok := definitions[testCase.tool]
		require.True(t, ok, "tool %q is not in the table", testCase.tool)

		properties, required := schemaObject(t, definition.Schema)
		fields := jsonStructFields(reflect.TypeOf(testCase.args))

		require.Equal(t, sortedKeysOfTypes(fields), sortedKeys(properties),
			"tool %s: the schema and %T describe different arguments", testCase.tool, testCase.args)
		for key := range properties {
			require.Containsf(t, fields, key,
				"tool %s: schema advertises %q, which %T does not decode", testCase.tool, key, testCase.args)
		}
		for _, key := range required {
			require.Containsf(t, properties, key,
				"tool %s: required key %q is not one of its schema properties", testCase.tool, key)
		}

		for key, structValue := range testCase.nested {
			property, ok := properties[key].(map[string]any)
			require.Truef(t, ok, "tool %s: %q is not an object schema", testCase.tool, key)
			nestedSchema := property
			if propertyType, _ := property["type"].(string); propertyType == "array" {
				items, ok := property["items"].(map[string]any)
				require.Truef(t, ok, "tool %s: %q is an array without items", testCase.tool, key)
				nestedSchema = items
			}
			nestedProperties, _ := schemaObject(t, nestedSchema)
			nestedFields := jsonStructFields(reflect.TypeOf(structValue))
			require.Equal(t, sortedKeysOfTypes(nestedFields), sortedKeys(nestedProperties),
				"tool %s: the %q schema and %T describe different fields", testCase.tool, key, structValue)
		}
	}
}

// schemaObject returns a schema's properties and required list, failing when the
// schema is not the object this surface's helpers build.
func schemaObject(t *testing.T, schema map[string]any) (map[string]any, []string) {
	t.Helper()

	propertyType, _ := schema["type"].(string)
	require.Equal(t, "object", propertyType, "tool schema must be an object")
	properties, ok := schema["properties"].(map[string]any)
	require.True(t, ok, "tool schema must carry properties")

	var required []string
	if raw, ok := schema["required"]; ok {
		required, ok = raw.([]string)
		require.True(t, ok, "tool schema required must be a list of names")
	}
	return properties, required
}

// jsonStructFields maps a struct's json tag names to their field types. Fields
// without a tag are not decoded from the wire and are not part of the contract.
func jsonStructFields(structType reflect.Type) map[string]reflect.Type {
	fields := map[string]reflect.Type{}
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		fields[name] = field.Type
	}
	return fields
}

func sortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func sortedKeysOfTypes(values map[string]reflect.Type) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
