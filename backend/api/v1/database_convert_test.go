package v1

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func TestConvertStoredMetadataMessageColumnMetadata(t *testing.T) {
	t.Parallel()

	converted := convertStoredMetadataMessage(&storepb.StoredMetadata{
		Type: &storepb.StoredMetadata_ColumnMetadata{
			ColumnMetadata: &storepb.ColumnMetadata{
				Name:        "customer_email",
				Comment:     "contains pii",
				UserComment: "sensitive contact channel",
				Type:        "text",
			},
		},
	})

	require.NotNil(t, converted)
	column := converted.GetColumnMetadata()
	require.NotNil(t, column)
	require.Equal(t, "customer_email", column.Name)
	require.Equal(t, "contains pii", column.Comment)
	require.Equal(t, "sensitive contact channel", column.UserComment)
	require.Equal(t, "text", column.Type)
}

// A page of databases is rendered with one instance lookup: the IDs are
// deduplicated so many databases on one instance do not each cost a query.
func TestDistinctInstanceIDs(t *testing.T) {
	t.Parallel()

	require.Empty(t, distinctInstanceIDs(nil))

	ids := distinctInstanceIDs([]*store.DatabaseMessage{
		{InstanceID: "inst-a", DatabaseName: "db1"},
		{InstanceID: "inst-b", DatabaseName: "db2"},
		{InstanceID: "inst-a", DatabaseName: "db3"},
	})
	require.Equal(t, []string{"inst-a", "inst-b"}, ids)
}

// The conversion no longer resolves the instance itself, so it works on the
// instance the caller batched.
func TestConvertToDatabaseUsesTheGivenInstance(t *testing.T) {
	t.Parallel()

	database := convertToDatabase(&store.DatabaseMessage{
		InstanceID:   "inst-a",
		DatabaseName: "db1",
		Metadata:     &storepb.DatabaseMetadata{Version: "1.2.3"},
	}, &store.InstanceMessage{ResourceID: "inst-a", Metadata: &storepb.Instance{Title: "prod", Engine: storepb.Engine_POSTGRES}})

	require.Equal(t, "instances/inst-a/databases/db1", database.Name)
	require.Equal(t, "1.2.3", database.SchemaVersion)
	require.Equal(t, "instances/inst-a", database.InstanceResource.Name)
}

// The leaf metadata converters copy fields by marshalling the store message and
// unmarshalling it into the v1 message, which only works while both messages
// keep identical field numbers, kinds and cardinality. Both errors are discarded
// in those converters, so this guard turns a one-sided proto edit into a failing
// test instead of silently dropped data.
func TestStoredMetadataTypesStayWireCompatible(t *testing.T) {
	t.Parallel()

	pairs := []struct {
		name  string
		store proto.Message
		v1    proto.Message
	}{
		{"TableMetadata", &storepb.TableMetadata{}, &v1pb.TableMetadata{}},
		{"ExternalTableMetadata", &storepb.ExternalTableMetadata{}, &v1pb.ExternalTableMetadata{}},
		{"ViewMetadata", &storepb.ViewMetadata{}, &v1pb.ViewMetadata{}},
		{"MaterializedViewMetadata", &storepb.MaterializedViewMetadata{}, &v1pb.MaterializedViewMetadata{}},
		{"FunctionMetadata", &storepb.FunctionMetadata{}, &v1pb.FunctionMetadata{}},
		{"ProcedureMetadata", &storepb.ProcedureMetadata{}, &v1pb.ProcedureMetadata{}},
		{"SequenceMetadata", &storepb.SequenceMetadata{}, &v1pb.SequenceMetadata{}},
		{"ExtensionMetadata", &storepb.ExtensionMetadata{}, &v1pb.ExtensionMetadata{}},
		{"EventTriggerMetadata", &storepb.EventTriggerMetadata{}, &v1pb.EventTriggerMetadata{}},
		{"EventMetadata", &storepb.EventMetadata{}, &v1pb.EventMetadata{}},
		{"EnumTypeMetadata", &storepb.EnumTypeMetadata{}, &v1pb.EnumTypeMetadata{}},
		{"ColumnMetadata", &storepb.ColumnMetadata{}, &v1pb.ColumnMetadata{}},
	}

	for _, pair := range pairs {
		t.Run(pair.name, func(t *testing.T) {
			t.Parallel()
			requireWireCompatible(t, pair.store.ProtoReflect().Descriptor(), pair.v1.ProtoReflect().Descriptor(), map[string]bool{})
		})
	}
}

func requireWireCompatible(t *testing.T, storeDesc, v1Desc protoreflect.MessageDescriptor, visited map[string]bool) {
	t.Helper()

	key := string(storeDesc.FullName()) + "|" + string(v1Desc.FullName())
	if visited[key] {
		return
	}
	visited[key] = true

	storeFields := storeDesc.Fields()
	byNumber := make(map[protoreflect.FieldNumber]protoreflect.FieldDescriptor, storeFields.Len())
	for i := 0; i < storeFields.Len(); i++ {
		field := storeFields.Get(i)
		byNumber[field.Number()] = field
	}

	v1Fields := v1Desc.Fields()
	require.Equalf(t, storeFields.Len(), v1Fields.Len(), "%s: store and v1 field counts must match", v1Desc.Name())
	for i := 0; i < v1Fields.Len(); i++ {
		v1Field := v1Fields.Get(i)
		storeField, ok := byNumber[v1Field.Number()]
		require.Truef(t, ok, "%s: v1 field %q (%d) has no store counterpart", v1Desc.Name(), v1Field.Name(), v1Field.Number())
		require.Equalf(t, storeField.Kind(), v1Field.Kind(), "%s field %d: kind", v1Desc.Name(), v1Field.Number())
		require.Equalf(t, storeField.Cardinality(), v1Field.Cardinality(), "%s field %d: cardinality", v1Desc.Name(), v1Field.Number())
		require.Equalf(t, storeField.IsMap(), v1Field.IsMap(), "%s field %d: map-ness", v1Desc.Name(), v1Field.Number())

		switch storeField.Kind() {
		case protoreflect.MessageKind, protoreflect.GroupKind:
			requireWireCompatible(t, storeField.Message(), v1Field.Message(), visited)
		case protoreflect.EnumKind:
			requireEnumWireCompatible(t, storeField.Enum(), v1Field.Enum())
		default:
			// Scalars and bytes carry their value directly on the wire.
		}
	}
}

func requireEnumWireCompatible(t *testing.T, storeEnum, v1Enum protoreflect.EnumDescriptor) {
	t.Helper()

	storeValues := make(map[protoreflect.EnumNumber]struct{}, storeEnum.Values().Len())
	for i := 0; i < storeEnum.Values().Len(); i++ {
		storeValues[storeEnum.Values().Get(i).Number()] = struct{}{}
	}
	for i := 0; i < v1Enum.Values().Len(); i++ {
		number := v1Enum.Values().Get(i).Number()
		_, ok := storeValues[number]
		require.Truef(t, ok, "%s: enum value %d has no store counterpart", v1Enum.Name(), number)
	}
}

// The two packages declare the same enums and rely on their numeric identity:
// metadata crosses the wire via proto.Marshal/Unmarshal and the converters
// switch on these values. A one-sided edit must fail here instead of silently
// mapping to the wrong value or to UNSPECIFIED.
func TestSharedEnumsStayValueCompatible(t *testing.T) {
	t.Parallel()

	pairs := []struct {
		name  string
		store map[int32]string
		v1    map[int32]string
	}{
		{"Engine", storepb.Engine_name, v1pb.Engine_name},
		{"MetaType", storepb.MetaType_name, v1pb.MetaType_name},
		{"DataSourceType", storepb.DataSourceType_name, v1pb.DataSourceType_name},
	}
	for _, pair := range pairs {
		t.Run(pair.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, pair.store, pair.v1)
		})
	}
}
