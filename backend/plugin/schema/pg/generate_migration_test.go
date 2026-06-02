package pg

import (
	"strings"
	"testing"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/schema"
)

func makeCol(name, typ string, nullable bool) *storepb.ColumnMetadata {
	return &storepb.ColumnMetadata{Name: name, Type: typ, Nullable: nullable}
}

func makeIndex(name string, cols []string, primary, unique bool) *storepb.IndexMetadata {
	return &storepb.IndexMetadata{Name: name, Expressions: cols, Primary: primary, Unique: unique}
}

func makeFK(name string, cols, refCols []string, refSchema, refTable string) *storepb.ForeignKeyMetadata {
	return &storepb.ForeignKeyMetadata{
		Name: name, Columns: cols,
		ReferencedSchema: refSchema, ReferencedTable: refTable,
		ReferencedColumns: refCols,
	}
}

//nolint:unparam
func makeTable(name string, cols []*storepb.ColumnMetadata, indexes []*storepb.IndexMetadata, fks []*storepb.ForeignKeyMetadata, checks []*storepb.CheckConstraintMetadata) *storepb.TableMetadata {
	return &storepb.TableMetadata{Name: name, Columns: cols, Indexes: indexes, ForeignKeys: fks, CheckConstraints: checks}
}

func makeView(name, def string) *storepb.ViewMetadata {
	return &storepb.ViewMetadata{Name: name, Definition: def}
}

func makeFunc(name, def string) *storepb.FunctionMetadata {
	return &storepb.FunctionMetadata{Name: name, Definition: def}
}

func makeDB(schemas []*storepb.SchemaMetadata) *storepb.DatabaseSchemaMetadata {
	return &storepb.DatabaseSchemaMetadata{Name: "test", Schemas: schemas}
}

func makeSchema(name string, tables []*storepb.TableMetadata, views []*storepb.ViewMetadata, funcs []*storepb.FunctionMetadata) *storepb.SchemaMetadata {
	return &storepb.SchemaMetadata{Name: name, Tables: tables, Views: views, Functions: funcs}
}

func TestGeneratePGCreateTable(t *testing.T) {
	oldDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema("public", nil, nil, nil),
	})
	newDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema("public", []*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "integer", false),
				makeCol("name", "text", true),
			}, nil, nil, nil),
		}, nil, nil),
	})

	diff, err := schema.GetDatabaseSchemaDiff(storepb.Engine_POSTGRES, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	ddl, err := generateMigration(diff)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(ddl, `"public"."users"`) {
		t.Errorf("expected CREATE TABLE for public.users, got:\n%s", ddl)
	}
	if !strings.Contains(ddl, `"id" integer NOT NULL`) {
		t.Errorf("expected id column, got:\n%s", ddl)
	}
}

func TestGeneratePGDropTable(t *testing.T) {
	oldDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema("public", []*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "integer", false),
			}, nil, nil, nil),
		}, nil, nil),
	})
	newDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema("public", nil, nil, nil),
	})

	diff, err := schema.GetDatabaseSchemaDiff(storepb.Engine_POSTGRES, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	ddl, err := generateMigration(diff)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(ddl, `DROP TABLE IF EXISTS "public"."users"`) {
		t.Errorf("expected DROP TABLE, got:\n%s", ddl)
	}
}

func TestGeneratePGAddColumn(t *testing.T) {
	oldDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema("public", []*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "integer", false),
			}, nil, nil, nil),
		}, nil, nil),
	})
	newDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema("public", []*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "integer", false),
				makeCol("name", "text", true),
			}, nil, nil, nil),
		}, nil, nil),
	})

	diff, err := schema.GetDatabaseSchemaDiff(storepb.Engine_POSTGRES, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	ddl, err := generateMigration(diff)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(ddl, `ADD COLUMN "name" text`) {
		t.Errorf("expected ADD COLUMN, got:\n%s", ddl)
	}
}

func TestGeneratePGDropColumn(t *testing.T) {
	oldDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema("public", []*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "integer", false),
				makeCol("name", "text", true),
			}, nil, nil, nil),
		}, nil, nil),
	})
	newDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema("public", []*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "integer", false),
			}, nil, nil, nil),
		}, nil, nil),
	})

	diff, err := schema.GetDatabaseSchemaDiff(storepb.Engine_POSTGRES, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	ddl, err := generateMigration(diff)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(ddl, `DROP COLUMN "name"`) {
		t.Errorf("expected DROP COLUMN, got:\n%s", ddl)
	}
}

func TestGeneratePGAddIndex(t *testing.T) {
	oldDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema("public", []*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "integer", false),
				makeCol("name", "text", true),
			}, nil, nil, nil),
		}, nil, nil),
	})
	newDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema("public", []*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "integer", false),
				makeCol("name", "text", true),
			}, []*storepb.IndexMetadata{
				makeIndex("idx_name", []string{"name"}, false, false),
			}, nil, nil),
		}, nil, nil),
	})

	diff, err := schema.GetDatabaseSchemaDiff(storepb.Engine_POSTGRES, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	ddl, err := generateMigration(diff)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(ddl, `CREATE INDEX "idx_name"`) {
		t.Errorf("expected CREATE INDEX, got:\n%s", ddl)
	}
}

func TestGeneratePGDropIndex(t *testing.T) {
	oldDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema("public", []*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "integer", false),
			}, []*storepb.IndexMetadata{
				makeIndex("idx_id", []string{"id"}, false, false),
			}, nil, nil),
		}, nil, nil),
	})
	newDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema("public", []*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "integer", false),
			}, nil, nil, nil),
		}, nil, nil),
	})

	diff, err := schema.GetDatabaseSchemaDiff(storepb.Engine_POSTGRES, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	ddl, err := generateMigration(diff)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(ddl, `DROP INDEX IF EXISTS "public"."idx_id"`) {
		t.Errorf("expected DROP INDEX, got:\n%s", ddl)
	}
}

func TestGeneratePGCreateSchema(t *testing.T) {
	oldDB := makeDB(nil)
	newDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema("app", nil, nil, nil),
	})

	diff, err := schema.GetDatabaseSchemaDiff(storepb.Engine_POSTGRES, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	ddl, err := generateMigration(diff)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(ddl, `CREATE SCHEMA IF NOT EXISTS "app"`) {
		t.Errorf("expected CREATE SCHEMA, got:\n%s", ddl)
	}
}

func TestGeneratePGCreateView(t *testing.T) {
	oldDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema("public", nil, nil, nil),
	})
	newDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema("public", nil, []*storepb.ViewMetadata{
			makeView("active_users", "SELECT * FROM users WHERE active = true"),
		}, nil),
	})

	diff, err := schema.GetDatabaseSchemaDiff(storepb.Engine_POSTGRES, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	ddl, err := generateMigration(diff)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(ddl, `CREATE VIEW "public"."active_users"`) {
		t.Errorf("expected CREATE VIEW, got:\n%s", ddl)
	}
}

func TestGeneratePGCreateFunction(t *testing.T) {
	oldDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema("public", nil, nil, nil),
	})
	newDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema("public", nil, nil, []*storepb.FunctionMetadata{
			makeFunc("get_name", "CREATE OR REPLACE FUNCTION public.get_name()\nRETURNS text AS $$\nBEGIN\n  RETURN 'hello';\nEND;\n$$ LANGUAGE plpgsql"),
		}),
	})

	diff, err := schema.GetDatabaseSchemaDiff(storepb.Engine_POSTGRES, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	ddl, err := generateMigration(diff)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(ddl, "CREATE OR REPLACE FUNCTION public.get_name()") {
		t.Errorf("expected CREATE FUNCTION body, got:\n%s", ddl)
	}
}

func TestGeneratePGAddFK(t *testing.T) {
	oldDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema("public", []*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "integer", false),
				makeCol("dept_id", "integer", true),
			}, nil, nil, nil),
			makeTable("departments", []*storepb.ColumnMetadata{
				makeCol("id", "integer", false),
			}, nil, nil, nil),
		}, nil, nil),
	})
	newDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema("public", []*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "integer", false),
				makeCol("dept_id", "integer", true),
			}, nil, []*storepb.ForeignKeyMetadata{
				makeFK("fk_dept", []string{"dept_id"}, []string{"id"}, "public", "departments"),
			}, nil),
			makeTable("departments", []*storepb.ColumnMetadata{
				makeCol("id", "integer", false),
			}, nil, nil, nil),
		}, nil, nil),
	})

	diff, err := schema.GetDatabaseSchemaDiff(storepb.Engine_POSTGRES, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	ddl, err := generateMigration(diff)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(ddl, `ADD CONSTRAINT "fk_dept" FOREIGN KEY`) {
		t.Errorf("expected ADD CONSTRAINT FOREIGN KEY, got:\n%s", ddl)
	}
	if !strings.Contains(ddl, `REFERENCES "public"."departments"`) {
		t.Errorf("expected REFERENCES, got:\n%s", ddl)
	}
}

func TestGeneratePGCommentChange(t *testing.T) {
	oldDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema("public", []*storepb.TableMetadata{
			{Name: "users", Columns: []*storepb.ColumnMetadata{makeCol("id", "integer", false)}, Comment: "Old comment"},
		}, nil, nil),
	})
	newDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema("public", []*storepb.TableMetadata{
			{Name: "users", Columns: []*storepb.ColumnMetadata{makeCol("id", "integer", false)}, Comment: "New comment"},
		}, nil, nil),
	})

	diff, err := schema.GetDatabaseSchemaDiff(storepb.Engine_POSTGRES, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	ddl, err := generateMigration(diff)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(ddl, `COMMENT ON TABLE "public"."users" IS 'New comment'`) {
		t.Errorf("expected COMMENT ON TABLE, got:\n%s", ddl)
	}
}

func TestGeneratePGNoChangeBlank(t *testing.T) {
	db := makeDB([]*storepb.SchemaMetadata{
		makeSchema("public", []*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "integer", false),
			}, nil, nil, nil),
		}, nil, nil),
	})

	diff, err := schema.GetDatabaseSchemaDiff(storepb.Engine_POSTGRES, db, db)
	if err != nil {
		t.Fatal(err)
	}
	ddl, err := generateMigration(diff)
	if err != nil {
		t.Fatal(err)
	}

	if ddl != "" {
		t.Errorf("expected empty DDL for no changes, got:\n%s", ddl)
	}
}
