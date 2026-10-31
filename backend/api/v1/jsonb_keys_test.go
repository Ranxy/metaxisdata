package v1

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// TestEverySQLJSONKeyNamesAStoredProtoField guards the hand-written half of the
// JSONB contract. A column's stored shape is named in its SQL comment and read
// back through protojson, but the queries that filter on it spell the camelCase
// key as a literal (`metadata->>'engine'`, `profile->>'lastChangePasswordTime'`).
// Renaming a proto field used to leave those predicates matching nothing — a
// filter that silently returns no rows, or a re-authorization check that never
// fires. Every key the SQL touches is listed here and resolved against the
// message the column stores, so a proto rename fails this test instead.
func TestEverySQLJSONKeyNamesAStoredProtoField(t *testing.T) {
	t.Parallel()

	cases := []struct {
		where   string
		message proto.Message
		path    []string
	}{
		// instance.metadata is Instance (LATEST.sql comment).
		{"instance.metadata->>'title'", &storepb.Instance{}, []string{"title"}},
		{"instance.metadata->>'engine'", &storepb.Instance{}, []string{"engine"}},
		{"instance.metadata->>'activation'", &storepb.Instance{}, []string{"activation"}},
		{"instance.metadata->'dataSources'->>'host'", &storepb.Instance{}, []string{"dataSources", "host"}},
		{"instance.metadata->'dataSources'->>'port'", &storepb.Instance{}, []string{"dataSources", "port"}},

		// db.metadata is DatabaseMetadata.
		{"db.metadata->'labels'", &storepb.DatabaseMetadata{}, []string{"labels"}},
		{"db.metadata->>'drifted'", &storepb.DatabaseMetadata{}, []string{"drifted"}},

		// meta_registry_resource.metadata is StoredMetadata.
		{"metadata->'sequenceMetadata'->>'ownerTable'", &storepb.StoredMetadata{}, []string{"sequenceMetadata", "ownerTable"}},

		// audit_log.payload is AuditLog.
		{"payload->>'parent'", &storepb.AuditLog{}, []string{"parent"}},
		{"payload->>'method'", &storepb.AuditLog{}, []string{"method"}},
		{"payload->>'resource'", &storepb.AuditLog{}, []string{"resource"}},
		{"payload->>'user'", &storepb.AuditLog{}, []string{"user"}},
		{"payload->>'severity'", &storepb.AuditLog{}, []string{"severity"}},

		// principal.profile is UserProfile.
		{"profile->>'lastChangePasswordTime'", &storepb.UserProfile{}, []string{"lastChangePasswordTime"}},

		// policy.payload is IamPolicy.
		{"payload->'bindings'", &storepb.IamPolicy{}, []string{"bindings"}},
		{"binding->>'role'", &storepb.IamPolicy{}, []string{"bindings", "role"}},
		{"binding->'members'", &storepb.IamPolicy{}, []string{"bindings", "members"}},

		// user_group.payload is GroupPayload.
		{"user_group.payload->'members'", &storepb.GroupPayload{}, []string{"members"}},
		{"m->>'member'", &storepb.GroupPayload{}, []string{"members", "member"}},

		// quality_contract_revision.snapshot is QualityContractSnapshot.
		{"snapshot->>'fingerprint'", &storepb.QualityContractSnapshot{}, []string{"fingerprint"}},

		// quality_* .target_path is QualityTargetPath.
		{"target_path->>'instance'", &storepb.QualityTargetPath{}, []string{"instance"}},
		{"target_path->>'database'", &storepb.QualityTargetPath{}, []string{"database"}},
		{"target_path->>'schema'", &storepb.QualityTargetPath{}, []string{"schema"}},
		{"target_path->>'object'", &storepb.QualityTargetPath{}, []string{"object"}},
		{"target_path->>'column'", &storepb.QualityTargetPath{}, []string{"column"}},
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.where, func(t *testing.T) {
			t.Parallel()
			requireJSONPath(t, testCase.message, testCase.path...)
		})
	}
}

// requireJSONPath resolves one protojson field path (by JSON name) through the
// message's descriptors, descending into message-typed fields so a nested key
// such as sequenceMetadata.ownerTable is checked against its own message.
func requireJSONPath(t *testing.T, message proto.Message, path ...string) {
	t.Helper()
	descriptor := message.ProtoReflect().Descriptor()
	for i, name := range path {
		field := descriptor.Fields().ByJSONName(name)
		require.NotNilf(t, field,
			"%s has no protojson field %q (path %v); the SQL predicate would silently match nothing",
			descriptor.FullName(), name, path)
		if i == len(path)-1 {
			return
		}
		require.Equalf(t, protoreflect.MessageKind, field.Kind(),
			"field %s of %s is not a message, so %q cannot be read below it",
			field.Name(), descriptor.FullName(), path[i+1])
		descriptor = field.Message()
	}
}
