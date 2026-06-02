package mysql

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

func makeFK(name string, cols, refCols []string, refTable string) *storepb.ForeignKeyMetadata {
	return &storepb.ForeignKeyMetadata{
		Name: name, Columns: cols, ReferencedTable: refTable, ReferencedColumns: refCols,
	}
}

//nolint:unparam
func makeTable(name string, cols []*storepb.ColumnMetadata, indexes []*storepb.IndexMetadata, fks []*storepb.ForeignKeyMetadata, _ []*storepb.CheckConstraintMetadata) *storepb.TableMetadata {
	return &storepb.TableMetadata{Name: name, Columns: cols, Indexes: indexes, ForeignKeys: fks}
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

func makeSchema(tables []*storepb.TableMetadata, views []*storepb.ViewMetadata, funcs []*storepb.FunctionMetadata) *storepb.SchemaMetadata {
	return &storepb.SchemaMetadata{Name: "public", Tables: tables, Views: views, Functions: funcs}
}

func TestGenerateCreateTable(t *testing.T) {
	oldDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema(nil, nil, nil),
	})
	newDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema([]*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "INT", false),
				makeCol("name", "VARCHAR(255)", true),
			}, []*storepb.IndexMetadata{
				makeIndex("PRIMARY", []string{"id"}, true, false),
			}, nil, nil),
		}, nil, nil),
	})

	diff, err := schema.GetDatabaseSchemaDiff(storepb.Engine_MYSQL, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	ddl, err := generateMigration(diff)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(ddl, "CREATE TABLE IF NOT EXISTS `users`") {
		t.Errorf("expected CREATE TABLE, got:\n%s", ddl)
	}
	if !strings.Contains(ddl, "`id` INT NOT NULL") {
		t.Errorf("expected id column, got:\n%s", ddl)
	}
	if !strings.Contains(ddl, "`name` VARCHAR(255)") {
		t.Errorf("expected name column, got:\n%s", ddl)
	}
	if !strings.Contains(ddl, "PRIMARY KEY (`id`)") {
		t.Errorf("expected PRIMARY KEY, got:\n%s", ddl)
	}
}

func TestGenerateDropTable(t *testing.T) {
	oldDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema([]*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "INT", false),
			}, nil, nil, nil),
		}, nil, nil),
	})
	newDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema(nil, nil, nil),
	})

	diff, err := schema.GetDatabaseSchemaDiff(storepb.Engine_MYSQL, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	ddl, err := generateMigration(diff)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(ddl, "DROP TABLE IF EXISTS `users`") {
		t.Errorf("expected DROP TABLE, got:\n%s", ddl)
	}
}

func TestGenerateAddColumn(t *testing.T) {
	oldDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema([]*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "INT", false),
			}, nil, nil, nil),
		}, nil, nil),
	})
	newDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema([]*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "INT", false),
				makeCol("name", "VARCHAR(255)", true),
			}, nil, nil, nil),
		}, nil, nil),
	})

	diff, err := schema.GetDatabaseSchemaDiff(storepb.Engine_MYSQL, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	ddl, err := generateMigration(diff)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(ddl, "ADD COLUMN `name` VARCHAR(255)") {
		t.Errorf("expected ADD COLUMN, got:\n%s", ddl)
	}
}

func TestGenerateDropColumn(t *testing.T) {
	oldDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema([]*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "INT", false),
				makeCol("name", "VARCHAR(255)", true),
			}, nil, nil, nil),
		}, nil, nil),
	})
	newDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema([]*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "INT", false),
			}, nil, nil, nil),
		}, nil, nil),
	})

	diff, err := schema.GetDatabaseSchemaDiff(storepb.Engine_MYSQL, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	ddl, err := generateMigration(diff)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(ddl, "DROP COLUMN `name`") {
		t.Errorf("expected DROP COLUMN, got:\n%s", ddl)
	}
}

func TestGenerateModifyColumn(t *testing.T) {
	oldDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema([]*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "INT", false),
			}, nil, nil, nil),
		}, nil, nil),
	})
	newDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema([]*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "BIGINT", false),
			}, nil, nil, nil),
		}, nil, nil),
	})

	diff, err := schema.GetDatabaseSchemaDiff(storepb.Engine_MYSQL, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	ddl, err := generateMigration(diff)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(ddl, "MODIFY COLUMN `id` BIGINT") {
		t.Errorf("expected MODIFY COLUMN, got:\n%s", ddl)
	}
}

func TestGenerateAddIndex(t *testing.T) {
	oldDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema([]*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "INT", false),
				makeCol("name", "VARCHAR(255)", true),
			}, nil, nil, nil),
		}, nil, nil),
	})
	newDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema([]*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "INT", false),
				makeCol("name", "VARCHAR(255)", true),
			}, []*storepb.IndexMetadata{
				makeIndex("idx_name", []string{"name"}, false, false),
			}, nil, nil),
		}, nil, nil),
	})

	diff, err := schema.GetDatabaseSchemaDiff(storepb.Engine_MYSQL, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	ddl, err := generateMigration(diff)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(ddl, "CREATE INDEX `idx_name` ON `users`") {
		t.Errorf("expected CREATE INDEX, got:\n%s", ddl)
	}
}

func TestGenerateDropIndex(t *testing.T) {
	oldDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema([]*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "INT", false),
			}, []*storepb.IndexMetadata{
				makeIndex("idx_id", []string{"id"}, false, false),
			}, nil, nil),
		}, nil, nil),
	})
	newDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema([]*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "INT", false),
			}, nil, nil, nil),
		}, nil, nil),
	})

	diff, err := schema.GetDatabaseSchemaDiff(storepb.Engine_MYSQL, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	ddl, err := generateMigration(diff)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(ddl, "DROP INDEX `idx_id` ON `users`") {
		t.Errorf("expected DROP INDEX, got:\n%s", ddl)
	}
}

func TestGenerateCreateView(t *testing.T) {
	oldDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema(nil, nil, nil),
	})
	newDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema(nil, []*storepb.ViewMetadata{
			makeView("active_users", "SELECT * FROM users WHERE active = 1"),
		}, nil),
	})

	diff, err := schema.GetDatabaseSchemaDiff(storepb.Engine_MYSQL, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	ddl, err := generateMigration(diff)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(ddl, "CREATE OR REPLACE VIEW `active_users`") {
		t.Errorf("expected CREATE OR REPLACE VIEW, got:\n%s", ddl)
	}
}

func TestGenerateDropView(t *testing.T) {
	oldDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema(nil, []*storepb.ViewMetadata{
			makeView("active_users", "SELECT * FROM users WHERE active = 1"),
		}, nil),
	})
	newDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema(nil, nil, nil),
	})

	diff, err := schema.GetDatabaseSchemaDiff(storepb.Engine_MYSQL, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	ddl, err := generateMigration(diff)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(ddl, "DROP VIEW IF EXISTS `active_users`") {
		t.Errorf("expected DROP VIEW, got:\n%s", ddl)
	}
}

func TestGenerateAddFK(t *testing.T) {
	oldDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema([]*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "INT", false),
				makeCol("dept_id", "INT", true),
			}, nil, nil, nil),
			makeTable("departments", []*storepb.ColumnMetadata{
				makeCol("id", "INT", false),
			}, nil, nil, nil),
		}, nil, nil),
	})
	newDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema([]*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "INT", false),
				makeCol("dept_id", "INT", true),
			}, nil, []*storepb.ForeignKeyMetadata{
				makeFK("fk_dept", []string{"dept_id"}, []string{"id"}, "departments"),
			}, nil),
			makeTable("departments", []*storepb.ColumnMetadata{
				makeCol("id", "INT", false),
			}, nil, nil, nil),
		}, nil, nil),
	})

	diff, err := schema.GetDatabaseSchemaDiff(storepb.Engine_MYSQL, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	ddl, err := generateMigration(diff)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(ddl, "ADD CONSTRAINT `fk_dept` FOREIGN KEY (`dept_id`)") {
		t.Errorf("expected ADD CONSTRAINT FOREIGN KEY, got:\n%s", ddl)
	}
	if !strings.Contains(ddl, "REFERENCES `departments` (`id`)") {
		t.Errorf("expected REFERENCES, got:\n%s", ddl)
	}
}

func TestGenerateDropFK(t *testing.T) {
	oldDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema([]*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "INT", false),
				makeCol("dept_id", "INT", true),
			}, nil, []*storepb.ForeignKeyMetadata{
				makeFK("fk_dept", []string{"dept_id"}, []string{"id"}, "departments"),
			}, nil),
			makeTable("departments", []*storepb.ColumnMetadata{
				makeCol("id", "INT", false),
			}, nil, nil, nil),
		}, nil, nil),
	})
	newDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema([]*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "INT", false),
				makeCol("dept_id", "INT", true),
			}, nil, nil, nil),
			makeTable("departments", []*storepb.ColumnMetadata{
				makeCol("id", "INT", false),
			}, nil, nil, nil),
		}, nil, nil),
	})

	diff, err := schema.GetDatabaseSchemaDiff(storepb.Engine_MYSQL, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	ddl, err := generateMigration(diff)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(ddl, "DROP FOREIGN KEY `fk_dept`") {
		t.Errorf("expected DROP FOREIGN KEY, got:\n%s", ddl)
	}
}

func TestGenerateNoChangeBlank(t *testing.T) {
	db := makeDB([]*storepb.SchemaMetadata{
		makeSchema([]*storepb.TableMetadata{
			makeTable("users", []*storepb.ColumnMetadata{
				makeCol("id", "INT", false),
			}, nil, nil, nil),
		}, nil, nil),
	})

	diff, err := schema.GetDatabaseSchemaDiff(storepb.Engine_MYSQL, db, db)
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

func TestGenerateCreateFunction(t *testing.T) {
	oldDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema(nil, nil, nil),
	})
	newDB := makeDB([]*storepb.SchemaMetadata{
		makeSchema(nil, nil, []*storepb.FunctionMetadata{
			makeFunc("get_total", "CREATE FUNCTION get_total() RETURNS INT\nBEGIN\n  RETURN 42;\nEND"),
		}),
	})

	diff, err := schema.GetDatabaseSchemaDiff(storepb.Engine_MYSQL, oldDB, newDB)
	if err != nil {
		t.Fatal(err)
	}
	ddl, err := generateMigration(diff)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(ddl, "CREATE FUNCTION get_total() RETURNS INT") {
		t.Errorf("expected CREATE FUNCTION body, got:\n%s", ddl)
	}
}
