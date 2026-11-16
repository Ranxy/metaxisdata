package v1

import (
	"github.com/Ranxy/metaxisdata/backend/component/tools/metadata"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// ExplainSQL's prompt context types and builder live in the shared metadata
// package; these aliases keep the service and its tests reading the same names.
type (
	explainSchemaContext = metadata.ExplainSchemaContext
	explainColumnInfo    = metadata.ExplainColumnInfo
)

// buildExplainSchemaContext builds the prompt context from the metadata objects.
func buildExplainSchemaContext(metas []*storepb.StoredMetadata, guids []string) *explainSchemaContext {
	return metadata.BuildExplainSchemaContext(metas, guids)
}
