package schema

import (
	"testing"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

func makeCol(name, typ string, nullable bool) *storepb.ColumnMetadata {
	return &storepb.ColumnMetadata{
		Name:     name,
		Type:     typ,
		Nullable: nullable,
	}
}

func makeIndex(name string, cols []string, primary, unique bool) *storepb.IndexMetadata {
	return &storepb.IndexMetadata{
		Name:        name,
		Expressions: cols,
		Primary:     primary,
		Unique:      unique,
	}
}

func makeFK(name string, cols, refCols []string, refTable string) *storepb.ForeignKeyMetadata {
	return &storepb.ForeignKeyMetadata{
		Name:              name,
		Columns:           cols,
		ReferencedTable:   refTable,
		ReferencedColumns: refCols,
	}
}

func makeCheck(name, expr string) *storepb.CheckConstraintMetadata {
	return &storepb.CheckConstraintMetadata{
		Name:       name,
		Expression: expr,
	}
}

func makeTable(name string, cols []*storepb.ColumnMetadata, indexes []*storepb.IndexMetadata, fks []*storepb.ForeignKeyMetadata, checks []*storepb.CheckConstraintMetadata) *storepb.TableMetadata {
	return &storepb.TableMetadata{
		Name:             name,
		Columns:          cols,
		Indexes:          indexes,
		ForeignKeys:      fks,
		CheckConstraints: checks,
	}
}

//nolint:unparam
func makeSchema(name string, tables []*storepb.TableMetadata, views []*storepb.ViewMetadata, funcs []*storepb.FunctionMetadata) *storepb.SchemaMetadata {
	return &storepb.SchemaMetadata{
		Name:      name,
		Tables:    tables,
		Views:     views,
		Functions: funcs,
	}
}

//nolint:unparam
func makeDB(name string, schemas []*storepb.SchemaMetadata) *storepb.DatabaseSchemaMetadata {
	return &storepb.DatabaseSchemaMetadata{
		Name:    name,
		Schemas: schemas,
	}
}

func makeView(name, def string) *storepb.ViewMetadata {
	return &storepb.ViewMetadata{
		Name:       name,
		Definition: def,
	}
}

func makeFunc(name, def string) *storepb.FunctionMetadata {
	return &storepb.FunctionMetadata{
		Name:       name,
		Definition: def,
	}
}

func TestDiffNilSchemas(t *testing.T) {
	diff, err := GetDatabaseSchemaDiff(storepb.Engine_MYSQL, nil, makeDB("test", nil))
	if err != nil {
		t.Fatal(err)
	}
	if diff != nil {
		t.Error("expected nil diff when old schema is nil")
	}

	diff, err = GetDatabaseSchemaDiff(storepb.Engine_MYSQL, makeDB("test", nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	if diff != nil {
		t.Error("expected nil diff when new schema is nil")
	}
}

func TestDiffNoChange(t *testing.T) {
	oldDB := makeDB("test", []*storepb.SchemaMetadata{
		makeSchema("public", []*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "int", false),
				makeCol("name", "varchar(255)", true),
			}, nil, nil, nil),
		}, nil, nil),
	})
	newDB := makeDB("test", []*storepb.SchemaMetadata{
		makeSchema("public", []*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "int", false),
				makeCol("name", "varchar(255)", true),
			}, nil, nil, nil),
		}, nil, nil),
	})

	diff, err := GetDatabaseSchemaDiff(storepb.Engine_MYSQL, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	if diff == nil {
		t.Fatal("expected non-nil diff")
	}
	if len(diff.SchemaChanges) != 0 {
		t.Error("expected no schema changes")
	}
	if len(diff.TableChanges) != 0 {
		t.Error("expected no table changes")
	}
}

func TestDiffCreateTable(t *testing.T) {
	oldDB := makeDB("test", []*storepb.SchemaMetadata{
		makeSchema("public", nil, nil, nil),
	})
	newDB := makeDB("test", []*storepb.SchemaMetadata{
		makeSchema("public", []*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "int", false),
			}, nil, nil, nil),
		}, nil, nil),
	})

	diff, err := GetDatabaseSchemaDiff(storepb.Engine_MYSQL, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	if diff == nil {
		t.Fatal("expected non-nil diff")
	}
	if len(diff.TableChanges) != 1 {
		t.Fatalf("expected 1 table change, got %d", len(diff.TableChanges))
	}
	if diff.TableChanges[0].Action != MetadataDiffActionCreate {
		t.Errorf("expected CREATE action, got %s", diff.TableChanges[0].Action)
	}
	if diff.TableChanges[0].TableName != "users" {
		t.Errorf("expected table name 'users', got %q", diff.TableChanges[0].TableName)
	}
}

func TestDiffDropTable(t *testing.T) {
	oldDB := makeDB("test", []*storepb.SchemaMetadata{
		makeSchema("public", []*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "int", false),
			}, nil, nil, nil),
		}, nil, nil),
	})
	newDB := makeDB("test", []*storepb.SchemaMetadata{
		makeSchema("public", nil, nil, nil),
	})

	diff, err := GetDatabaseSchemaDiff(storepb.Engine_MYSQL, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	if diff == nil {
		t.Fatal("expected non-nil diff")
	}
	if len(diff.TableChanges) != 1 {
		t.Fatalf("expected 1 table change, got %d", len(diff.TableChanges))
	}
	if diff.TableChanges[0].Action != MetadataDiffActionDrop {
		t.Errorf("expected DROP action, got %s", diff.TableChanges[0].Action)
	}
}

func TestDiffAddColumn(t *testing.T) {
	oldDB := makeDB("test", []*storepb.SchemaMetadata{
		makeSchema("public", []*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "int", false),
			}, nil, nil, nil),
		}, nil, nil),
	})
	newDB := makeDB("test", []*storepb.SchemaMetadata{
		makeSchema("public", []*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "int", false),
				makeCol("name", "varchar(255)", true),
			}, nil, nil, nil),
		}, nil, nil),
	})

	diff, err := GetDatabaseSchemaDiff(storepb.Engine_MYSQL, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	if diff == nil {
		t.Fatal("expected non-nil diff")
	}
	if len(diff.TableChanges) != 1 {
		t.Fatalf("expected 1 table change, got %d", len(diff.TableChanges))
	}
	tc := diff.TableChanges[0]
	if tc.Action != MetadataDiffActionAlter {
		t.Errorf("expected ALTER action, got %s", tc.Action)
	}
	if len(tc.ColumnChanges) != 1 {
		t.Fatalf("expected 1 column change, got %d", len(tc.ColumnChanges))
	}
	if tc.ColumnChanges[0].Action != MetadataDiffActionCreate {
		t.Errorf("expected CREATE column action, got %s", tc.ColumnChanges[0].Action)
	}
	if tc.ColumnChanges[0].NewColumn.Name != "name" {
		t.Errorf("expected column name 'name', got %q", tc.ColumnChanges[0].NewColumn.Name)
	}
}

func TestDiffDropColumn(t *testing.T) {
	oldDB := makeDB("test", []*storepb.SchemaMetadata{
		makeSchema("public", []*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "int", false),
				makeCol("name", "varchar(255)", true),
			}, nil, nil, nil),
		}, nil, nil),
	})
	newDB := makeDB("test", []*storepb.SchemaMetadata{
		makeSchema("public", []*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "int", false),
			}, nil, nil, nil),
		}, nil, nil),
	})

	diff, err := GetDatabaseSchemaDiff(storepb.Engine_MYSQL, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	if diff == nil {
		t.Fatal("expected non-nil diff")
	}
	if len(diff.TableChanges) != 1 {
		t.Fatalf("expected 1 table change, got %d", len(diff.TableChanges))
	}
	tc := diff.TableChanges[0]
	if len(tc.ColumnChanges) != 1 {
		t.Fatalf("expected 1 column change, got %d", len(tc.ColumnChanges))
	}
	if tc.ColumnChanges[0].Action != MetadataDiffActionDrop {
		t.Errorf("expected DROP column action, got %s", tc.ColumnChanges[0].Action)
	}
}

func TestDiffModifyColumn(t *testing.T) {
	oldDB := makeDB("test", []*storepb.SchemaMetadata{
		makeSchema("public", []*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "int", false),
			}, nil, nil, nil),
		}, nil, nil),
	})
	newDB := makeDB("test", []*storepb.SchemaMetadata{
		makeSchema("public", []*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "bigint", false),
			}, nil, nil, nil),
		}, nil, nil),
	})

	diff, err := GetDatabaseSchemaDiff(storepb.Engine_MYSQL, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	if diff == nil {
		t.Fatal("expected non-nil diff")
	}
	if len(diff.TableChanges) != 1 {
		t.Fatalf("expected 1 table change, got %d", len(diff.TableChanges))
	}
	tc := diff.TableChanges[0]
	if len(tc.ColumnChanges) != 1 {
		t.Fatalf("expected 1 column change, got %d", len(tc.ColumnChanges))
	}
	if tc.ColumnChanges[0].Action != MetadataDiffActionAlter {
		t.Errorf("expected ALTER column action, got %s", tc.ColumnChanges[0].Action)
	}
}

func TestDiffAddIndex(t *testing.T) {
	oldDB := makeDB("test", []*storepb.SchemaMetadata{
		makeSchema("public", []*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "int", false),
			}, nil, nil, nil),
		}, nil, nil),
	})
	newDB := makeDB("test", []*storepb.SchemaMetadata{
		makeSchema("public", []*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "int", false),
			}, []*storepb.IndexMetadata{
				makeIndex("idx_id", []string{"id"}, false, false),
			}, nil, nil),
		}, nil, nil),
	})

	diff, err := GetDatabaseSchemaDiff(storepb.Engine_MYSQL, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	if diff == nil {
		t.Fatal("expected non-nil diff")
	}
	tc := diff.TableChanges[0]
	if len(tc.IndexChanges) != 1 {
		t.Fatalf("expected 1 index change, got %d", len(tc.IndexChanges))
	}
	if tc.IndexChanges[0].Action != MetadataDiffActionCreate {
		t.Errorf("expected CREATE index action, got %s", tc.IndexChanges[0].Action)
	}
}

func TestDiffDropIndex(t *testing.T) {
	oldDB := makeDB("test", []*storepb.SchemaMetadata{
		makeSchema("public", []*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "int", false),
			}, []*storepb.IndexMetadata{
				makeIndex("idx_id", []string{"id"}, false, false),
			}, nil, nil),
		}, nil, nil),
	})
	newDB := makeDB("test", []*storepb.SchemaMetadata{
		makeSchema("public", []*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "int", false),
			}, nil, nil, nil),
		}, nil, nil),
	})

	diff, err := GetDatabaseSchemaDiff(storepb.Engine_MYSQL, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	if diff == nil {
		t.Fatal("expected non-nil diff")
	}
	tc := diff.TableChanges[0]
	if len(tc.IndexChanges) != 1 {
		t.Fatalf("expected 1 index change, got %d", len(tc.IndexChanges))
	}
	if tc.IndexChanges[0].Action != MetadataDiffActionDrop {
		t.Errorf("expected DROP index action, got %s", tc.IndexChanges[0].Action)
	}
}

func TestDiffAddFK(t *testing.T) {
	oldDB := makeDB("test", []*storepb.SchemaMetadata{
		makeSchema("public", []*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "int", false),
				makeCol("dept_id", "int", true),
			}, nil, nil, nil),
			makeTable("departments", []*storepb.ColumnMetadata{
				makeCol("id", "int", false),
			}, nil, nil, nil),
		}, nil, nil),
	})

	newDB := makeDB("test", []*storepb.SchemaMetadata{
		makeSchema("public", []*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "int", false),
				makeCol("dept_id", "int", true),
			}, nil, []*storepb.ForeignKeyMetadata{
				makeFK("fk_dept", []string{"dept_id"}, []string{"id"}, "departments"),
			}, nil),
			makeTable("departments", []*storepb.ColumnMetadata{
				makeCol("id", "int", false),
			}, nil, nil, nil),
		}, nil, nil),
	})

	diff, err := GetDatabaseSchemaDiff(storepb.Engine_MYSQL, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	if diff == nil {
		t.Fatal("expected non-nil diff")
	}

	var usersDiff *TableDiff
	for _, tc := range diff.TableChanges {
		if tc.TableName == "users" {
			usersDiff = tc
			break
		}
	}
	if usersDiff == nil {
		t.Fatal("expected a table change for 'users'")
	}
	if len(usersDiff.ForeignKeyChanges) != 1 {
		t.Fatalf("expected 1 FK change, got %d", len(usersDiff.ForeignKeyChanges))
	}
	if usersDiff.ForeignKeyChanges[0].Action != MetadataDiffActionCreate {
		t.Errorf("expected CREATE FK action, got %s", usersDiff.ForeignKeyChanges[0].Action)
	}
}

func TestDiffDropFK(t *testing.T) {
	oldDB := makeDB("test", []*storepb.SchemaMetadata{
		makeSchema("public", []*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "int", false),
				makeCol("dept_id", "int", true),
			}, nil, []*storepb.ForeignKeyMetadata{
				makeFK("fk_dept", []string{"dept_id"}, []string{"id"}, "departments"),
			}, nil),
			makeTable("departments", []*storepb.ColumnMetadata{
				makeCol("id", "int", false),
			}, nil, nil, nil),
		}, nil, nil),
	})

	newDB := makeDB("test", []*storepb.SchemaMetadata{
		makeSchema("public", []*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "int", false),
				makeCol("dept_id", "int", true),
			}, nil, nil, nil),
			makeTable("departments", []*storepb.ColumnMetadata{
				makeCol("id", "int", false),
			}, nil, nil, nil),
		}, nil, nil),
	})

	diff, err := GetDatabaseSchemaDiff(storepb.Engine_MYSQL, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	if diff == nil {
		t.Fatal("expected non-nil diff")
	}

	var usersDiff *TableDiff
	for _, tc := range diff.TableChanges {
		if tc.TableName == "users" {
			usersDiff = tc
			break
		}
	}
	if usersDiff == nil {
		t.Fatal("expected a table change for 'users'")
	}
	if len(usersDiff.ForeignKeyChanges) != 1 {
		t.Fatalf("expected 1 FK change, got %d", len(usersDiff.ForeignKeyChanges))
	}
	if usersDiff.ForeignKeyChanges[0].Action != MetadataDiffActionDrop {
		t.Errorf("expected DROP FK action, got %s", usersDiff.ForeignKeyChanges[0].Action)
	}
}

func TestDiffCreateView(t *testing.T) {
	oldDB := makeDB("test", []*storepb.SchemaMetadata{
		makeSchema("public", nil, nil, nil),
	})
	newDB := makeDB("test", []*storepb.SchemaMetadata{
		makeSchema("public", nil, []*storepb.ViewMetadata{
			makeView("active_users", "SELECT * FROM users WHERE active = 1"),
		}, nil),
	})

	diff, err := GetDatabaseSchemaDiff(storepb.Engine_MYSQL, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	if diff == nil {
		t.Fatal("expected non-nil diff")
	}
	if len(diff.ViewChanges) != 1 {
		t.Fatalf("expected 1 view change, got %d", len(diff.ViewChanges))
	}
	if diff.ViewChanges[0].Action != MetadataDiffActionCreate {
		t.Errorf("expected CREATE view action, got %s", diff.ViewChanges[0].Action)
	}
}

func TestDiffDropView(t *testing.T) {
	oldDB := makeDB("test", []*storepb.SchemaMetadata{
		makeSchema("public", nil, []*storepb.ViewMetadata{
			makeView("active_users", "SELECT * FROM users WHERE active = 1"),
		}, nil),
	})
	newDB := makeDB("test", []*storepb.SchemaMetadata{
		makeSchema("public", nil, nil, nil),
	})

	diff, err := GetDatabaseSchemaDiff(storepb.Engine_MYSQL, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	if diff == nil {
		t.Fatal("expected non-nil diff")
	}
	if len(diff.ViewChanges) != 1 {
		t.Fatalf("expected 1 view change, got %d", len(diff.ViewChanges))
	}
	if diff.ViewChanges[0].Action != MetadataDiffActionDrop {
		t.Errorf("expected DROP view action, got %s", diff.ViewChanges[0].Action)
	}
}

func TestDiffCreateFunction(t *testing.T) {
	oldDB := makeDB("test", []*storepb.SchemaMetadata{
		makeSchema("public", nil, nil, nil),
	})
	newDB := makeDB("test", []*storepb.SchemaMetadata{
		makeSchema("public", nil, nil, []*storepb.FunctionMetadata{
			makeFunc("get_name", "BEGIN RETURN 'hello'; END"),
		}),
	})

	diff, err := GetDatabaseSchemaDiff(storepb.Engine_MYSQL, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	if diff == nil {
		t.Fatal("expected non-nil diff")
	}
	if len(diff.FunctionChanges) != 1 {
		t.Fatalf("expected 1 function change, got %d", len(diff.FunctionChanges))
	}
	if diff.FunctionChanges[0].Action != MetadataDiffActionCreate {
		t.Errorf("expected CREATE function action, got %s", diff.FunctionChanges[0].Action)
	}
}

func TestDiffDropFunction(t *testing.T) {
	oldDB := makeDB("test", []*storepb.SchemaMetadata{
		makeSchema("public", nil, nil, []*storepb.FunctionMetadata{
			makeFunc("get_name", "BEGIN RETURN 'hello'; END"),
		}),
	})
	newDB := makeDB("test", []*storepb.SchemaMetadata{
		makeSchema("public", nil, nil, nil),
	})

	diff, err := GetDatabaseSchemaDiff(storepb.Engine_MYSQL, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	if diff == nil {
		t.Fatal("expected non-nil diff")
	}
	if len(diff.FunctionChanges) != 1 {
		t.Fatalf("expected 1 function change, got %d", len(diff.FunctionChanges))
	}
	if diff.FunctionChanges[0].Action != MetadataDiffActionDrop {
		t.Errorf("expected DROP function action, got %s", diff.FunctionChanges[0].Action)
	}
}

func TestDiffCreateSchema(t *testing.T) {
	oldDB := makeDB("test", nil)
	newDB := makeDB("test", []*storepb.SchemaMetadata{
		makeSchema("public", nil, nil, nil),
	})

	diff, err := GetDatabaseSchemaDiff(storepb.Engine_POSTGRES, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	if diff == nil {
		t.Fatal("expected non-nil diff")
	}
	if len(diff.SchemaChanges) != 1 {
		t.Fatalf("expected 1 schema change, got %d", len(diff.SchemaChanges))
	}
	if diff.SchemaChanges[0].Action != MetadataDiffActionCreate {
		t.Errorf("expected CREATE schema action, got %s", diff.SchemaChanges[0].Action)
	}
}

func TestDiffDropSchema(t *testing.T) {
	oldDB := makeDB("test", []*storepb.SchemaMetadata{
		makeSchema("public", nil, nil, nil),
	})
	newDB := makeDB("test", nil)

	diff, err := GetDatabaseSchemaDiff(storepb.Engine_POSTGRES, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	if diff == nil {
		t.Fatal("expected non-nil diff")
	}
	if len(diff.SchemaChanges) != 1 {
		t.Fatalf("expected 1 schema change, got %d", len(diff.SchemaChanges))
	}
	if diff.SchemaChanges[0].Action != MetadataDiffActionDrop {
		t.Errorf("expected DROP schema action, got %s", diff.SchemaChanges[0].Action)
	}
}

func TestDiffCommentChange(t *testing.T) {
	oldDB := makeDB("test", []*storepb.SchemaMetadata{
		makeSchema("public", []*storepb.TableMetadata{
			{Name: "users", Columns: []*storepb.ColumnMetadata{makeCol("id", "int", false)}, Comment: "Old comment"},
		}, nil, nil),
	})
	newDB := makeDB("test", []*storepb.SchemaMetadata{
		makeSchema("public", []*storepb.TableMetadata{
			{Name: "users", Columns: []*storepb.ColumnMetadata{makeCol("id", "int", false)}, Comment: "New comment"},
		}, nil, nil),
	})

	diff, err := GetDatabaseSchemaDiff(storepb.Engine_MYSQL, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	if diff == nil {
		t.Fatal("expected non-nil diff")
	}
	found := false
	for _, cc := range diff.CommentChanges {
		if cc.ObjectType == CommentObjectTypeTable && cc.ObjectName == "users" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected comment change for table 'users'")
	}
}

func TestDiffAddCheckConstraint(t *testing.T) {
	oldDB := makeDB("test", []*storepb.SchemaMetadata{
		makeSchema("public", []*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("age", "int", true),
			}, nil, nil, nil),
		}, nil, nil),
	})
	newDB := makeDB("test", []*storepb.SchemaMetadata{
		makeSchema("public", []*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("age", "int", true),
			}, nil, nil, []*storepb.CheckConstraintMetadata{
				makeCheck("chk_age", "age > 0"),
			}),
		}, nil, nil),
	})

	diff, err := GetDatabaseSchemaDiff(storepb.Engine_MYSQL, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	if diff == nil {
		t.Fatal("expected non-nil diff")
	}
	tc := diff.TableChanges[0]
	if len(tc.CheckConstraintChanges) != 1 {
		t.Fatalf("expected 1 check constraint change, got %d", len(tc.CheckConstraintChanges))
	}
	if tc.CheckConstraintChanges[0].Action != MetadataDiffActionCreate {
		t.Errorf("expected CREATE check constraint action, got %s", tc.CheckConstraintChanges[0].Action)
	}
}

func TestDiffMultipleChanges(t *testing.T) {
	oldDB := makeDB("test", []*storepb.SchemaMetadata{
		makeSchema("public", []*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "int", false),
			}, nil, nil, nil),
		}, nil, nil),
	})
	newDB := makeDB("test", []*storepb.SchemaMetadata{
		makeSchema("public", []*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "bigint", false),
				makeCol("name", "varchar(255)", true),
			}, []*storepb.IndexMetadata{
				makeIndex("idx_name", []string{"name"}, false, false),
			}, nil, nil),
			makeTable("orders", []*storepb.ColumnMetadata{
				makeCol("id", "int", false),
				makeCol("user_id", "int", false),
			}, nil, nil, nil),
		}, nil, nil),
	})

	diff, err := GetDatabaseSchemaDiff(storepb.Engine_MYSQL, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	if diff == nil {
		t.Fatal("expected non-nil diff")
	}

	createTables := 0
	alterTables := 0
	for _, tc := range diff.TableChanges {
		switch tc.Action {
		case MetadataDiffActionCreate:
			createTables++
		case MetadataDiffActionAlter:
			alterTables++
		default:
		}
	}

	if createTables != 1 {
		t.Errorf("expected 1 CREATE table, got %d", createTables)
	}
	if alterTables != 1 {
		t.Errorf("expected 1 ALTER table, got %d", alterTables)
	}
}
