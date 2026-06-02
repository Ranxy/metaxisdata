package schema

import (
	"fmt"
	"sync"

	"github.com/pkg/errors"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

var (
	mux                            sync.Mutex
	getTableDefinitions            = make(map[storepb.Engine]getTableDefinition)
	getViewDefinitions             = make(map[storepb.Engine]getViewDefinition)
	getMaterializedViewDefinitions = make(map[storepb.Engine]getMaterializedViewDefinition)
	getFunctionDefinitions         = make(map[storepb.Engine]getFunctionDefinition)
	getProcedureDefinitions        = make(map[storepb.Engine]getProcedureDefinition)
	getSequenceDefinitions         = make(map[storepb.Engine]getSequenceDefinition)
)

type getTableDefinition func(string, *storepb.TableMetadata, []*storepb.SequenceMetadata) (string, error)
type getViewDefinition func(string, *storepb.ViewMetadata) (string, error)
type getMaterializedViewDefinition func(string, *storepb.MaterializedViewMetadata) (string, error)
type getFunctionDefinition func(string, *storepb.FunctionMetadata) (string, error)
type getProcedureDefinition func(string, *storepb.ProcedureMetadata) (string, error)
type getSequenceDefinition func(string, *storepb.SequenceMetadata) (string, error)

type GetDefinitionContext struct {
	SkipBackupSchema bool
	PrintHeader      bool
	SDLFormat        bool
	// MultiFileFormat indicates whether to generate multi-file SDL output.
	// When true, the result should be organized as multiple files.
	MultiFileFormat bool
}

// generateMigration is the function type for generating DDL migration from a diff.
type generateMigration func(*MetadataDiff) (string, error)

var generateMigrations = make(map[storepb.Engine]generateMigration)

// RegisterGenerateMigration registers a generate migration function for an engine.
func RegisterGenerateMigration(engine storepb.Engine, f generateMigration) {
	mux.Lock()
	defer mux.Unlock()
	if _, dup := generateMigrations[engine]; dup {
		panic(fmt.Sprintf("Register called twice %s", engine))
	}
	generateMigrations[engine] = f
}

// GenerateMigration generates DDL migration SQL from a MetadataDiff for the given engine.
func GenerateMigration(engine storepb.Engine, diff *MetadataDiff) (string, error) {
	f, ok := generateMigrations[engine]
	if !ok {
		return "", errors.Errorf("engine %s is not supported", engine)
	}
	return f(diff)
}

// File represents a single file in a multi-file schema output.
type File struct {
	// Name is the file path or name (e.g., "schemas/public/tables/users.sql")
	Name string
	// Content is the file content
	Content string
}

// MultiFileSchemaResult represents the result of multi-file schema generation.
type MultiFileSchemaResult struct {
	// Files is the list of schema files organized by type
	Files []File
}

func RegisterGetSequenceDefinition(engine storepb.Engine, f getSequenceDefinition) {
	mux.Lock()
	defer mux.Unlock()
	if _, dup := getSequenceDefinitions[engine]; dup {
		panic(fmt.Sprintf("Register called twice %s", engine))
	}
	getSequenceDefinitions[engine] = f
}

func GetSequenceDefinition(engine storepb.Engine, schemaName string, sequence *storepb.SequenceMetadata) (string, error) {
	f, ok := getSequenceDefinitions[engine]
	if !ok {
		return "", errors.Errorf("engine %s is not supported", engine)
	}
	return f(schemaName, sequence)
}

func RegisterGetFunctionDefinition(engine storepb.Engine, f getFunctionDefinition) {
	mux.Lock()
	defer mux.Unlock()
	if _, dup := getFunctionDefinitions[engine]; dup {
		panic(fmt.Sprintf("Register called twice %s", engine))
	}
	getFunctionDefinitions[engine] = f
}

func GetFunctionDefinition(engine storepb.Engine, schemaName string, function *storepb.FunctionMetadata) (string, error) {
	f, ok := getFunctionDefinitions[engine]
	if !ok {
		return "", errors.Errorf("engine %s is not supported", engine)
	}
	return f(schemaName, function)
}

func RegisterGetProcedureDefinition(engine storepb.Engine, f getProcedureDefinition) {
	mux.Lock()
	defer mux.Unlock()
	if _, dup := getProcedureDefinitions[engine]; dup {
		panic(fmt.Sprintf("Register called twice %s", engine))
	}
	getProcedureDefinitions[engine] = f
}

func GetProcedureDefinition(engine storepb.Engine, schemaName string, procedure *storepb.ProcedureMetadata) (string, error) {
	f, ok := getProcedureDefinitions[engine]
	if !ok {
		return "", errors.Errorf("engine %s is not supported", engine)
	}
	return f(schemaName, procedure)
}

func RegisterGetMaterializedViewDefinition(engine storepb.Engine, f getMaterializedViewDefinition) {
	mux.Lock()
	defer mux.Unlock()
	if _, dup := getMaterializedViewDefinitions[engine]; dup {
		panic(fmt.Sprintf("Register called twice %s", engine))
	}
	getMaterializedViewDefinitions[engine] = f
}

func GetMaterializedViewDefinition(engine storepb.Engine, schemaName string, view *storepb.MaterializedViewMetadata) (string, error) {
	f, ok := getMaterializedViewDefinitions[engine]
	if !ok {
		return "", errors.Errorf("engine %s is not supported", engine)
	}
	return f(schemaName, view)
}

func RegisterGetViewDefinition(engine storepb.Engine, f getViewDefinition) {
	mux.Lock()
	defer mux.Unlock()
	if _, dup := getViewDefinitions[engine]; dup {
		panic(fmt.Sprintf("Register called twice %s", engine))
	}
	getViewDefinitions[engine] = f
}

func GetViewDefinition(engine storepb.Engine, schemaName string, view *storepb.ViewMetadata) (string, error) {
	f, ok := getViewDefinitions[engine]
	if !ok {
		return "", errors.Errorf("engine %s is not supported", engine)
	}
	return f(schemaName, view)
}

func RegisterGetTableDefinition(engine storepb.Engine, f getTableDefinition) {
	mux.Lock()
	defer mux.Unlock()
	if _, dup := getTableDefinitions[engine]; dup {
		panic(fmt.Sprintf("Register called twice %s", engine))
	}
	getTableDefinitions[engine] = f
}

func GetTableDefinition(engine storepb.Engine, schemaName string, table *storepb.TableMetadata, sequences []*storepb.SequenceMetadata) (string, error) {
	f, ok := getTableDefinitions[engine]
	if !ok {
		return "", errors.Errorf("engine %s is not supported", engine)
	}
	return f(schemaName, table, sequences)
}
