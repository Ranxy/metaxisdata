package openlineage

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// ResolvedDataset holds the result of resolving an OpenLineage dataset to an internal or external entity.
type ResolvedDataset struct {
	GUID     string
	MetaType storepb.MetaType
	// True if this dataset was resolved to an internal instance.
	Internal bool
}

// Resolver maps OpenLineage dataset namespaces and names to internal GUIDs or external datasets.
type Resolver struct {
	store *store.Store
}

// NewResolver creates a new Resolver.
func NewResolver(s *store.Store) *Resolver {
	return &Resolver{store: s}
}

// ResolveDataset resolves an OpenLineage dataset (namespace + name) to a GUID and MetaType.
// Resolution order:
//  1. Manual namespace_mapping table lookup
//  2. Auto-match by parsing namespace URL and matching instance DataSource host:port
//  3. Create/return an external dataset
func (r *Resolver) ResolveDataset(ctx context.Context, namespace, datasetName string) (*ResolvedDataset, error) {
	resolved, err := r.ResolveDatasetPreview(ctx, namespace, datasetName)
	if err != nil {
		return nil, err
	}
	if resolved.Internal {
		return resolved, nil
	}

	if _, err := r.store.GetOrCreateExternalDataset(ctx, namespace, datasetName, datasetTypeFromNamespace(namespace)); err != nil {
		return nil, errors.Wrap(err, "failed to get or create external dataset")
	}

	return resolved, nil
}

// ResolveDatasetPreview resolves an OpenLineage dataset without creating external-dataset rows.
func (r *Resolver) ResolveDatasetPreview(ctx context.Context, namespace, datasetName string) (*ResolvedDataset, error) {
	// 1. Manual mapping
	if resolved, err := r.resolveByManualMapping(ctx, namespace, datasetName); err != nil {
		return nil, err
	} else if resolved != nil {
		return resolved, nil
	}

	// 2. Auto-match by host:port
	if resolved, err := r.resolveByAutoMatch(ctx, namespace, datasetName); err != nil {
		return nil, err
	} else if resolved != nil {
		return resolved, nil
	}

	// 3. External dataset preview
	return &ResolvedDataset{
		GUID:     FormatExternalGUID(namespace, datasetName),
		MetaType: storepb.MetaType_EXTERNAL_DATASET,
		Internal: false,
	}, nil
}

func (r *Resolver) resolveByManualMapping(ctx context.Context, namespace, datasetName string) (*ResolvedDataset, error) {
	mapping, err := r.store.GetNamespaceMapping(ctx, &store.FindNamespaceMappingMessage{Namespace: &namespace})
	if err != nil {
		return nil, errors.Wrap(err, "failed to lookup namespace mapping")
	}
	if mapping == nil {
		return nil, nil
	}

	instance, err := r.store.GetInstanceV2(ctx, &store.FindInstanceMessage{ResourceID: &mapping.InstanceResourceID})
	if err != nil {
		return nil, errors.Wrap(err, "failed to get instance")
	}
	if instance == nil {
		return nil, nil
	}

	guid := buildGUID(instance.ResourceID, instance.Metadata.GetEngine(), mapping.DatabaseName, datasetName)
	return &ResolvedDataset{
		GUID:     guid,
		MetaType: storepb.MetaType_TABLE,
		Internal: true,
	}, nil
}

func (r *Resolver) resolveByAutoMatch(ctx context.Context, namespace, datasetName string) (*ResolvedDataset, error) {
	host, port, dbFromNS := parseNamespace(namespace)
	if host == "" {
		return nil, nil
	}

	instances, err := r.store.ListInstancesV2(ctx, &store.FindInstanceMessage{})
	if err != nil {
		return nil, errors.Wrap(err, "failed to list instances")
	}

	for _, inst := range instances {
		if inst.Deleted || inst.Metadata == nil {
			continue
		}
		for _, ds := range inst.Metadata.GetDataSources() {
			if matchHostPort(ds.GetHost(), ds.GetPort(), host, port) {
				database := dbFromNS
				if database == "" {
					database = ds.GetDatabase()
				}
				guid := buildGUID(inst.ResourceID, inst.Metadata.GetEngine(), database, datasetName)
				return &ResolvedDataset{
					GUID:     guid,
					MetaType: storepb.MetaType_TABLE,
					Internal: true,
				}, nil
			}
		}
	}

	return nil, nil
}

// parseNamespace extracts host, port, and optional database from an OpenLineage namespace URL.
// Examples:
//
//	"postgres://myhost:5432/mydb" -> ("myhost", "5432", "mydb")
//	"mysql://myhost:3306"        -> ("myhost", "3306", "")
//	"bigquery"                   -> ("bigquery", "", "")
//	"s3://bucket"                -> ("", "", "")
func parseNamespace(namespace string) (host, port, database string) {
	u, err := url.Parse(namespace)
	if err != nil {
		return "", "", ""
	}

	scheme := strings.ToLower(u.Scheme)
	switch scheme {
	case "postgres", "postgresql", "mysql", "mssql", "clickhouse", "snowflake",
		"oracle", "tidb", "mariadb", "redshift", "starrocks", "doris",
		"cockroachdb", "trino", "hive", "spark":
		// Database connection namespace — fall through.
	case "bigquery":
		// BigQuery uses "bigquery" scheme. The host portion (if present) may be
		// a project ID; the path may encode the dataset.
		bqHost := u.Hostname()
		if bqHost == "" {
			bqHost = "bigquery"
		}
		bqDB := ""
		if u.Path != "" {
			bqDB = strings.TrimPrefix(u.Path, "/")
		}
		return bqHost, "", bqDB
	case "":
		// Bare namespace like "bigquery" or "default" (common in Airflow).
		if strings.EqualFold(namespace, "bigquery") {
			return "bigquery", "", ""
		}
		return "", "", ""
	default:
		return "", "", ""
	}

	host = u.Hostname()
	port = u.Port()
	if u.Path != "" {
		database = strings.TrimPrefix(u.Path, "/")
	}
	return host, port, database
}

// matchHostPort compares instance DataSource host:port with parsed namespace host:port.
func matchHostPort(dsHost, dsPort, nsHost, nsPort string) bool {
	if !strings.EqualFold(dsHost, nsHost) {
		return false
	}
	if nsPort == "" {
		// If namespace doesn't specify port, match any port.
		return true
	}
	return dsPort == nsPort
}

// buildGUID constructs an internal GUID from instance + dataset name.
// Dataset name may be "database.schema.table", "schema.table", or "table".
func buildGUID(instanceResourceID string, engine storepb.Engine, databaseOverride, datasetName string) string {
	parts := strings.Split(datasetName, ".")

	var database, schema, table string
	switch len(parts) {
	case 1:
		table = parts[0]
	case 2:
		// For MySQL-like engines: db.table. For others: schema.table.
		if isMySQLLike(engine) {
			database, table = parts[0], parts[1]
		} else {
			schema, table = parts[0], parts[1]
		}
	default: // 3+
		database, schema, table = parts[0], parts[1], strings.Join(parts[2:], ".")
	}

	if databaseOverride != "" {
		database = databaseOverride
	}

	return strings.Join([]string{instanceResourceID, database, schema, table}, common.MetaGUIDSplit)
}

func isMySQLLike(engine storepb.Engine) bool {
	switch engine {
	case storepb.Engine_MYSQL, storepb.Engine_TIDB, storepb.Engine_MARIADB:
		return true
	default:
		return false
	}
}

// inferDatasetType guesses the dataset type from the namespace scheme.
func datasetTypeFromNamespace(namespace string) string {
	u, err := url.Parse(namespace)
	if err != nil {
		return "unknown"
	}
	scheme := strings.ToLower(u.Scheme)
	switch {
	case scheme == "s3" || scheme == "s3a" || scheme == "s3n":
		return "s3"
	case scheme == "gs" || scheme == "gcs":
		return "gcs"
	case scheme == "hdfs":
		return "hdfs"
	case scheme == "kafka":
		return "kafka"
	case scheme == "file":
		return "file"
	case scheme == "bigquery":
		return "bigquery"
	case scheme == "hive":
		return "hive"
	case scheme == "spark":
		return "spark"
	case strings.Contains(scheme, "postgres") || strings.Contains(scheme, "mysql") ||
		strings.Contains(scheme, "mssql") || strings.Contains(scheme, "oracle") ||
		strings.Contains(scheme, "snowflake"):
		return "database"
	default:
		// Bare namespace like "bigquery" (no scheme).
		if strings.EqualFold(namespace, "bigquery") {
			return "bigquery"
		}
		return "unknown"
	}
}

// InferDatasetType guesses the dataset type from the namespace scheme.
func InferDatasetType(namespace string) string {
	return datasetTypeFromNamespace(namespace)
}

// ExternalDatasetGUIDPrefix is the prefix for external dataset GUIDs.
const ExternalDatasetGUIDPrefix = "external:"

// IsExternalGUID returns true if the GUID belongs to an external dataset.
func IsExternalGUID(guid string) bool {
	return strings.HasPrefix(guid, ExternalDatasetGUIDPrefix)
}

// FormatExternalGUID creates an external dataset GUID.
func FormatExternalGUID(namespace, name string) string {
	return fmt.Sprintf("%s%s:%s", ExternalDatasetGUIDPrefix, namespace, name)
}
