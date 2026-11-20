package dataquality

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// maxEnumLabelsNamed bounds how many labels one validation error names. An enum
// can declare many labels, while the stored error text must stay readable and
// bounded.
const maxEnumLabelsNamed = 32

// enumValuesFromType returns the labels of an inline MySQL-family ENUM column
// type, and nil for every other spelling. The registry stores the engine's
// COLUMN_TYPE text verbatim, so the declared labels are recoverable from the type
// without opening the target instance. The parser follows the engine's quoting:
// labels are single-quoted, an embedded quote is doubled, and the characters the
// engine cannot print are backslash-escaped.
func enumValuesFromType(columnType string) []string {
	trimmed := strings.TrimSpace(columnType)
	prefix, suffix := "enum(", ")"
	if len(trimmed) <= len(prefix)+len(suffix) ||
		!strings.EqualFold(trimmed[:len(prefix)], prefix) ||
		!strings.HasSuffix(trimmed, suffix) {
		return nil
	}
	body := trimmed[len(prefix) : len(trimmed)-len(suffix)]
	values := make([]string, 0, 4)
	for index := 0; index < len(body); {
		for index < len(body) && (body[index] == ' ' || body[index] == ',') {
			index++
		}
		if index >= len(body) {
			break
		}
		if body[index] != '\'' {
			return nil
		}
		index++
		var label strings.Builder
		closed := false
		for index < len(body) && !closed {
			switch body[index] {
			case '\'':
				if index+1 < len(body) && body[index+1] == '\'' {
					_ = label.WriteByte('\'')
					index += 2
					continue
				}
				index++
				closed = true
			case '\\':
				if index+1 >= len(body) {
					_ = label.WriteByte('\\')
					index++
					continue
				}
				switch body[index+1] {
				case '0':
					_ = label.WriteByte(0)
				case 'n':
					_ = label.WriteByte('\n')
				case 'r':
					_ = label.WriteByte('\r')
				case 'Z':
					_ = label.WriteByte(0x1a)
				default:
					_ = label.WriteByte(body[index+1])
				}
				index += 2
			default:
				_ = label.WriteByte(body[index])
				index++
			}
		}
		if !closed {
			return nil
		}
		values = append(values, label.String())
	}
	if len(values) == 0 {
		return nil
	}
	return values
}

// postgresUserDefinedType splits a PostgreSQL user-defined type spelling into the
// schema and the type name. The sync stores a user-defined type schema-qualified
// ("public.product_status"); a built-in type, an array or a parameterized type
// ("numeric(10,2)") never carries exactly one schema separator.
func postgresUserDefinedType(columnType string) (schema, name string, ok bool) {
	trimmed := strings.TrimSpace(columnType)
	if trimmed == "" || strings.HasSuffix(trimmed, "[]") {
		return "", "", false
	}
	dot := strings.LastIndexByte(trimmed, '.')
	if dot <= 0 || dot == len(trimmed)-1 {
		return "", "", false
	}
	schema, name = trimmed[:dot], trimmed[dot+1:]
	if strings.ContainsAny(schema, "() ") || strings.ContainsAny(name, "() ") {
		return "", "", false
	}
	return schema, name, true
}

// schemaEnumLabels reads the enum types one schema declares from the registry. A
// schema whose row is missing, was never synced, or cannot be read yields no
// labels: enum validation is best effort and must not turn a metadata read into a
// rule failure.
func schemaEnumLabels(ctx context.Context, s *store.Store, instance, database, schema string) map[string][]string {
	guid := common.BuildMetaGUID(instance, database, schema)
	objectType := storepb.MetaType_SCHEMA
	resource, err := s.GetMetaRegistry(ctx, &store.FindMetaRegistryResourceMessage{
		GUID:       &guid,
		ObjectType: &objectType,
	})
	if err != nil {
		slog.Debug("failed to read the schema's enum types",
			slog.String("guid", guid), slog.Any("error", err))
		return nil
	}
	if resource == nil || resource.Metadata == nil {
		return nil
	}
	enums := resource.Metadata.GetSchemaMetadata().GetEnumTypes()
	if len(enums) == 0 {
		return nil
	}
	labels := make(map[string][]string, len(enums))
	for _, enum := range enums {
		if enum == nil || len(enum.GetValues()) == 0 {
			continue
		}
		labels[enum.GetName()] = enum.GetValues()
	}
	return labels
}

// enrichColumnEnumValues attaches the labels the target declares to its enum
// columns. PostgreSQL keeps enum types in the schema's registry metadata and the
// column names the type schema-qualified; the MySQL family keeps the labels
// inline in the column type. Any other engine has no enum type.
func enrichColumnEnumValues(ctx context.Context, s *store.Store, target *ResolvedTarget) {
	if target == nil || len(target.Columns) == 0 {
		return
	}
	switch {
	case target.Engine == storepb.Engine_POSTGRES:
		if s == nil {
			return
		}
		labelsBySchema := make(map[string]map[string][]string, 1)
		for index := range target.Columns {
			column := &target.Columns[index]
			if len(column.EnumValues) > 0 {
				continue
			}
			schema, name, ok := postgresUserDefinedType(column.Type)
			if !ok {
				continue
			}
			labels, cached := labelsBySchema[schema]
			if !cached {
				labels = schemaEnumLabels(ctx, s, target.InstanceResourceID, target.Database, schema)
				labelsBySchema[schema] = labels
			}
			if values := labels[name]; len(values) > 0 {
				column.EnumValues = values
			}
		}
	case isMySQLWireEngine(target.Engine):
		for index := range target.Columns {
			column := &target.Columns[index]
			if len(column.EnumValues) > 0 {
				continue
			}
			column.EnumValues = enumValuesFromType(column.Type)
		}
	default:
		// Every other engine has no enum type, so there is nothing to resolve.
	}
}

// enumValueError rejects a value the target's enum type cannot hold. It returns
// nil when the column is not an enum or the registry did not name its labels, so
// a best-effort lookup can never reject a value it knows nothing about.
func enumValueError(column ColumnInfo, value string) error {
	if len(column.EnumValues) == 0 {
		return nil
	}
	for _, label := range column.EnumValues {
		if label == value {
			return nil
		}
	}
	return invalidConfig("value %q is not a label of the enum type %q; valid labels: %s",
		value, column.Type, enumLabelList(column.EnumValues))
}

// enumLabelList renders the labels one error message may name, quoted so a label
// with spaces or punctuation stays distinguishable.
func enumLabelList(values []string) string {
	named := values
	if len(named) > maxEnumLabelsNamed {
		named = named[:maxEnumLabelsNamed]
	}
	quoted := make([]string, 0, len(named))
	for _, value := range named {
		quoted = append(quoted, fmt.Sprintf("%q", value))
	}
	list := strings.Join(quoted, ", ")
	if len(values) > len(named) {
		list += fmt.Sprintf(", ... (%d more)", len(values)-len(named))
	}
	return list
}
