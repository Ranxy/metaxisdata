package scope

import "github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"

// ColumnRef represents a reference to a column in the query.
type ColumnRef struct {
	Schema string
	Table  string
	Column string
	// Resolved marks a reference that was already resolved against the scope it
	// originated in, so a later lookup does not bind it to a different relation
	// that happens to share the name. Every analyzer sets it where the resolved
	// table lives in a scope a later lookup cannot see: a set-operation arm, a
	// flattened expression subquery, an expanded wildcard.
	Resolved bool
}

// ResolvedColumn is a column reference together with the relation that owns it.
// An unqualified name yields one per relation in scope that exposes it, which is
// more than one when several relations share the name — the shape a coalesced
// USING or NATURAL JOIN column has, where every owner really is a source.
type ResolvedColumn struct {
	Ref      ColumnRef
	Relation *TableRef
}

// RelationKey identifies a relation inside a scope. Qualifier is the database
// (MySQL family, StarRocks) or schema (PostgreSQL) the relation was named with,
// and is empty when the query does not name one. Name is the alias when the
// relation has one, otherwise its table name: the name a SQL reference uses.
type RelationKey struct {
	Qualifier string
	Name      string
}

// Key returns the key a SQL reference addresses the relation by.
func (r *TableRef) Key() RelationKey {
	return RelationKey{Qualifier: r.Schema, Name: r.addressName()}
}

// addressName is the name a SQL reference uses for the relation: its alias when
// it has one, otherwise its table name.
func (r *TableRef) addressName() string {
	if r.Alias != "" {
		return r.Alias
	}
	return r.Table
}

// addresses reports whether a reference with this key can name the relation. The
// name has to be the relation's alias, or its table name when it has no alias; a
// reference qualifier has to agree with the relation's own, except that a
// relation registered without a qualifier accepts any, because the analyzer does
// not know which database or schema an unqualified FROM clause resolves in.
func (r *TableRef) addresses(key RelationKey) bool {
	if key.Name != r.addressName() {
		return false
	}
	return key.Qualifier == "" || r.Schema == "" || key.Qualifier == r.Schema
}

// RelationKeyOf builds the key of a relation endpoint recorded on an edge. The
// qualifier is whichever of the identifier's database or schema is set: the
// MySQL family and StarRocks record a SQL qualifier as a database, PostgreSQL as
// a schema.
func RelationKeyOf(id model.ObjectIdentifier) RelationKey {
	qualifier := id.Database
	if qualifier == "" {
		qualifier = id.Schema
	}
	return RelationKey{Qualifier: qualifier, Name: id.Name}
}

// ColumnLookup reports the column names a relation exposes. A nil result means
// the metadata is unavailable, and the resolver keeps its deterministic ordering
// rule for that relation.
type ColumnLookup func() []string

// TableRef represents a table or table-like source (subquery, CTE).
type TableRef struct {
	Schema string
	Table  string
	Alias  string
	// For subqueries and CTEs
	IsSubquery bool
	IsCTE      bool
	// Lineage edges for subqueries (how output columns relate to source tables)
	Lineage []model.ColumnRelation
	// columnLookup, when set, reports this base table's column names from the
	// catalog. It is consulted lazily, only when an unqualified column has to be
	// disambiguated across several relations.
	columnLookup ColumnLookup
}

// SetColumnLookup attaches the catalog column lookup for this relation. Analyzers
// set it when they register a base table, so every scope — including the CTE,
// derived-table and subquery scopes the analyzers resolve in — disambiguates an
// unqualified column the same way.
func (r *TableRef) SetColumnLookup(lookup ColumnLookup) {
	r.columnLookup = lookup
}

// ColumnNames returns the relation's column names, or nil when they are unknown.
func (r *TableRef) ColumnNames() []string {
	if r == nil || r.columnLookup == nil {
		return nil
	}
	return r.columnLookup()
}

// CTEDefinition represents a Common Table Expression.
type CTEDefinition struct {
	Name    string
	Columns []string
	// Lineage edges within the CTE
	Lineage []model.ColumnRelation
}

// ColumnSource is a source column of an output column together with the
// transformations that produced the output from it. The transformation travels
// with the source rather than with the output column because a set operation
// merges arms that compute the same output column in different ways: one
// transformation for the whole column would attribute the first arm's expression
// to every other arm's sources, which is lineage that does not exist.
type ColumnSource struct {
	Ref       ColumnRef
	Transform []model.Transformation
}

// NewColumnSources pairs every reference with the same transformation, the shape
// an output column built from a single expression has.
func NewColumnSources(refs []ColumnRef, transform []model.Transformation) []ColumnSource {
	if len(refs) == 0 {
		return nil
	}
	out := make([]ColumnSource, 0, len(refs))
	for _, ref := range refs {
		out = append(out, ColumnSource{Ref: ref, Transform: transform})
	}
	return out
}

// Refs returns the source references without their transformations, for the
// callers that only need to resolve or count them.
func Refs(sources []ColumnSource) []ColumnRef {
	if len(sources) == 0 {
		return nil
	}
	out := make([]ColumnRef, 0, len(sources))
	for _, source := range sources {
		out = append(out, source.Ref)
	}
	return out
}

// OutputColumn represents a column in the SELECT output.
type OutputColumn struct {
	Alias string // The alias given to this column (AS clause)
	// Sources are the columns this output depends on, each carrying the
	// transformation that produced it.
	Sources []ColumnSource
	// Whether this is a derived/transformed column
	IsDerived bool
}

// SetTransform gives every source the same transformation list, the shape an
// output column built from a single expression has. The list is shared rather
// than copied, so callers must treat it as read-only afterwards.
func (c *OutputColumn) SetTransform(transform []model.Transformation) {
	for i := range c.Sources {
		c.Sources[i].Transform = transform
	}
}

// NewLineageEdge creates a ColumnRelation from field-edge parameters, recording
// the qualifier of each endpoint as a database name. That is what a MySQL-family
// or StarRocks reference names; PostgreSQL names a schema and uses
// NewSchemaLineageEdge.
func NewLineageEdge(fromQualifier, fromTable, fromField, toQualifier, toTable, toField string, transform []model.Transformation, isTemp bool) model.ColumnRelation {
	return buildLineageEdge(fromQualifier, fromTable, fromField, toQualifier, toTable, toField, transform, isTemp, false)
}

// NewSchemaLineageEdge is NewLineageEdge with the qualifier recorded as a schema
// name. The distinction is not cosmetic: database and schema are separate fields
// of an object identifier, so an edge built with the wrong one names a different
// object than the analyzer resolved.
func NewSchemaLineageEdge(fromQualifier, fromTable, fromField, toQualifier, toTable, toField string, transform []model.Transformation, isTemp bool) model.ColumnRelation {
	return buildLineageEdge(fromQualifier, fromTable, fromField, toQualifier, toTable, toField, transform, isTemp, true)
}

func buildLineageEdge(fromQualifier, fromTable, fromField, toQualifier, toTable, toField string, transform []model.Transformation, isTemp, qualifierIsSchema bool) model.ColumnRelation {
	identifier := func(qualifier, name string) model.ObjectIdentifier {
		if qualifierIsSchema {
			return model.ObjectIdentifier{Schema: qualifier, Name: name}
		}
		return model.ObjectIdentifier{Database: qualifier, Name: name}
	}
	return model.ColumnRelation{
		Source: model.Column{
			Table: identifier(fromQualifier, fromTable),
			Name:  fromField,
		},
		Target: model.Column{
			Table: identifier(toQualifier, toTable),
			Name:  toField,
		},
		Transformation: transform,
		RelationType:   model.RelationTypeOf(transform),
		IsTemp:         isTemp,
	}
}
