package openlineage

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"sync"

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

// ResolutionStore is the store surface the resolver reads: the namespace mapping,
// the instances behind it, and the external dataset it creates for a namespace
// that names no instance. It is the seam that lets the resolution rules and their
// per-request memoization be tested without a database.
type ResolutionStore interface {
	GetNamespaceMapping(ctx context.Context, find *store.FindNamespaceMappingMessage) (*store.NamespaceMappingMessage, error)
	GetInstance(ctx context.Context, find *store.FindInstanceMessage) (*store.InstanceMessage, error)
	ListInstances(ctx context.Context, find *store.FindInstanceMessage) ([]*store.InstanceMessage, error)
	GetOrCreateExternalDataset(ctx context.Context, namespace, name, datasetType string) (*store.ExternalDatasetMessage, error)
}

// Resolver maps OpenLineage dataset namespaces and names to internal GUIDs or external datasets.
type Resolver struct {
	store ResolutionStore

	// requestScoped enables per-request memoization of dataset previews, of what
	// one namespace resolves to, and of the instance list. Without it a read
	// endpoint re-resolves every dataset once per run, lists every instance once per
	// distinct dataset namespace, and looks up the namespace mapping once per
	// dataset.
	requestScoped bool
	previews      map[previewKey]*ResolvedDataset
	namespaces    map[string]*namespaceResolution

	instances     []*store.InstanceMessage
	instancesDone bool

	// warnedNamespaces keeps the "did not match any instance" warning to one per
	// namespace per resolver. Ingestion uses one long-lived resolver, so a
	// recurring miss logs once instead of once per event.
	warnedMu         sync.Mutex
	warnedNamespaces map[string]struct{}
}

type previewKey struct {
	namespace string
	name      string
}

// namespaceResolution is what resolving a dataset needs from its namespace alone,
// which every dataset of that namespace shares.
type namespaceResolution struct {
	// database is the database the namespace's path names, if any.
	database string
	// manual is the instance a namespace mapping names, when the namespace has one.
	manual *resolvedInstance
	// auto is the instance whose data source answers the namespace's host and port.
	auto *resolvedInstance
}

// resolvedInstance is the instance a namespace resolves to, with what building one
// dataset's GUID needs.
type resolvedInstance struct {
	resourceID string
	engine     storepb.Engine
	// databaseOverride is the database a namespace mapping names; a mapping that
	// names one has stated the answer and it wins over everything else.
	databaseOverride string
	// database is the instance's own data source database, the last resort.
	database string
}

// NewResolver creates a new Resolver.
func NewResolver(s ResolutionStore) *Resolver {
	return &Resolver{store: s, warnedNamespaces: make(map[string]struct{})}
}

// NewRequestScopedResolver creates a Resolver that memoizes lookups for the
// lifetime of one request. Use NewResolver for ingestion, where a resolver may
// serve many resolutions and cached answers could go stale.
func NewRequestScopedResolver(s ResolutionStore) *Resolver {
	return &Resolver{
		store:            s,
		requestScoped:    true,
		previews:         make(map[previewKey]*ResolvedDataset),
		namespaces:       make(map[string]*namespaceResolution),
		warnedNamespaces: make(map[string]struct{}),
	}
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

	r.warnUnmatchedNamespace(namespace, datasetName)

	if _, err := r.store.GetOrCreateExternalDataset(ctx, namespace, datasetName, datasetTypeFromNamespace(namespace)); err != nil {
		return nil, errors.Wrap(err, "failed to get or create external dataset")
	}

	return resolved, nil
}

// warnUnmatchedNamespace reports, once per namespace, a dataset that fell
// through to an external dataset. The common cause is an instance registered
// with one spelling of an address and the producer using another - a host alias
// or a different port - which used to leave no trace at all: the lineage simply
// pointed at an external dataset that looked deliberate.
func (r *Resolver) warnUnmatchedNamespace(namespace, datasetName string) {
	r.warnedMu.Lock()
	if _, seen := r.warnedNamespaces[namespace]; seen {
		r.warnedMu.Unlock()
		return
	}
	r.warnedNamespaces[namespace] = struct{}{}
	r.warnedMu.Unlock()

	host, port, _ := parseNamespace(namespace)
	slog.Warn("OpenLineage dataset did not match any registered instance and was stored as an external dataset",
		"namespace", namespace,
		"dataset", datasetName,
		"host", host,
		"port", port,
		"hint", "check the instance's data source host and port, or add a namespace mapping to associate them",
	)
}

// ResolveDatasetPreview resolves an OpenLineage dataset without creating external-dataset rows.
func (r *Resolver) ResolveDatasetPreview(ctx context.Context, namespace, datasetName string) (*ResolvedDataset, error) {
	key := previewKey{namespace: namespace, name: datasetName}
	if r.requestScoped {
		if v, ok := r.previews[key]; ok {
			return v, nil
		}
	}

	resolved, err := r.resolveDatasetPreviewUncached(ctx, namespace, datasetName)
	if err != nil {
		return nil, err
	}
	if r.requestScoped {
		r.previews[key] = resolved
	}
	return resolved, nil
}

func (r *Resolver) resolveDatasetPreviewUncached(ctx context.Context, namespace, datasetName string) (*ResolvedDataset, error) {
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
	resolution, err := r.namespaceResolution(ctx, namespace)
	if err != nil {
		return nil, err
	}
	if resolution.manual == nil {
		return nil, nil
	}

	instance := resolution.manual
	database := mappingDatabase(instance.engine, datasetName, namespace, instance.databaseOverride)
	guid := buildGUID(instance.resourceID, instance.engine, database, datasetName)
	return &ResolvedDataset{
		GUID:     guid,
		MetaType: storepb.MetaType_TABLE,
		Internal: true,
	}, nil
}

// namespaceResolution returns what the namespace resolves to, looking it up once
// per request: a page resolves thousands of datasets, usually many of them in one
// namespace, and the lookups behind them are queries.
func (r *Resolver) namespaceResolution(ctx context.Context, namespace string) (*namespaceResolution, error) {
	if r.requestScoped {
		if resolution, ok := r.namespaces[namespace]; ok {
			return resolution, nil
		}
	}

	resolution, err := r.lookupNamespace(ctx, namespace)
	if err != nil {
		return nil, err
	}
	if r.requestScoped {
		r.namespaces[namespace] = resolution
	}
	return resolution, nil
}

// lookupNamespace answers the two ways a namespace names an instance: the manual
// mapping, which wins, and the instance whose data source carries the host and
// port the namespace parses to.
func (r *Resolver) lookupNamespace(ctx context.Context, namespace string) (*namespaceResolution, error) {
	host, port, database := parseNamespace(namespace)
	resolution := &namespaceResolution{database: database}

	mapping, err := r.store.GetNamespaceMapping(ctx, &store.FindNamespaceMappingMessage{Namespace: &namespace})
	if err != nil {
		return nil, errors.Wrap(err, "failed to lookup namespace mapping")
	}
	if mapping != nil {
		instance, err := r.store.GetInstance(ctx, &store.FindInstanceMessage{ResourceID: &mapping.InstanceResourceID})
		if err != nil {
			return nil, errors.Wrap(err, "failed to get instance")
		}
		if instance != nil {
			resolution.manual = &resolvedInstance{
				resourceID:       instance.ResourceID,
				engine:           instance.Metadata.GetEngine(),
				databaseOverride: mapping.DatabaseName,
			}
		}
	}

	if host == "" || resolution.manual != nil {
		// A mapping answers the namespace, and an unparsable namespace names no
		// instance; neither needs the instance list.
		return resolution, nil
	}
	instances, err := r.listInstances(ctx)
	if err != nil {
		return nil, err
	}
	for _, instance := range instances {
		if instance.Deleted || instance.Metadata == nil {
			continue
		}
		for _, dataSource := range instance.Metadata.GetDataSources() {
			if matchHostPort(dataSource.GetHost(), dataSource.GetPort(), host, port) {
				resolution.auto = &resolvedInstance{
					resourceID: instance.ResourceID,
					engine:     instance.Metadata.GetEngine(),
					database:   dataSource.GetDatabase(),
				}
				return resolution, nil
			}
		}
	}
	return resolution, nil
}

// mappingDatabase returns the database a namespace mapping resolves a dataset to.
// A mapping that names one has stated the answer, so it wins; a mapping that names
// only the instance leaves the database to the same evidence auto-match uses, so a
// dataset whose name carries no database still resolves to the namespace's.
func mappingDatabase(engine storepb.Engine, datasetName, namespace, databaseFromMapping string) string {
	if databaseFromMapping != "" {
		return databaseFromMapping
	}
	_, _, databaseFromNamespace := parseNamespace(namespace)
	return datasetDatabase(engine, datasetName, databaseFromNamespace, "")
}

func (r *Resolver) resolveByAutoMatch(ctx context.Context, namespace, datasetName string) (*ResolvedDataset, error) {
	resolution, err := r.namespaceResolution(ctx, namespace)
	if err != nil {
		return nil, err
	}
	if resolution.auto == nil {
		return nil, nil
	}

	instance := resolution.auto
	database := datasetDatabase(instance.engine, datasetName, resolution.database, instance.database)
	guid := buildGUID(instance.resourceID, instance.engine, database, datasetName)
	return &ResolvedDataset{
		GUID:     guid,
		MetaType: storepb.MetaType_TABLE,
		Internal: true,
	}, nil
}

// datasetDatabase decides which database a dataset belongs to, from the three
// places one can be named.
//
// The dataset name is the most specific evidence - one dataset, one database -
// and it wins whenever it carries one ("db.schema.table" on PostgreSQL,
// "db.table" on the MySQL-wire engines). A name that carries none
// ("schema.table", "table") falls back to the namespace the producer sent, and
// only then to the instance's own data source. That last one is a registration
// detail rather than a statement about this dataset: an instance serves every
// database its schema sync enumerates, so its data source usually names the
// server's default database. Letting it outrank the name is how an ingested edge
// came to point at a relation the registry does not have, silently, because the
// resolver still answered "internal".
func datasetDatabase(engine storepb.Engine, datasetName, databaseFromNamespace, databaseFromDataSource string) string {
	if database, _, _ := splitDatasetName(engine, datasetName); database != "" {
		return database
	}
	if databaseFromNamespace != "" {
		return databaseFromNamespace
	}
	return databaseFromDataSource
}

// listInstances returns the instance list, memoized for the request when the
// resolver is request-scoped.
func (r *Resolver) listInstances(ctx context.Context) ([]*store.InstanceMessage, error) {
	if r.requestScoped && r.instancesDone {
		return r.instances, nil
	}
	instances, err := r.store.ListInstances(ctx, &store.FindInstanceMessage{})
	if err != nil {
		return nil, errors.Wrap(err, "failed to list instances")
	}
	if r.requestScoped {
		r.instances, r.instancesDone = instances, true
	}
	return instances, nil
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
	if normalizeHost(dsHost) != normalizeHost(nsHost) {
		return false
	}
	if nsPort == "" {
		// If namespace doesn't specify port, match any port.
		return true
	}
	return normalizePort(dsPort) == normalizePort(nsPort)
}

// normalizeHost folds the host spellings that name the same server onto one
// token. Instances are registered by hand and producers build their namespaces
// from JDBC URLs, so `localhost` and `127.0.0.1` (or `::1`) are routinely used
// for the same database; comparing them literally turned every dataset of that
// instance into an external dataset.
func normalizeHost(host string) string {
	h := strings.ToLower(strings.TrimSpace(host))
	h = strings.TrimPrefix(strings.TrimSuffix(h, "]"), "[")
	switch h {
	case "localhost", "127.0.0.1", "::1", "0:0:0:0:0:0:0:1":
		return "loopback"
	default:
		return h
	}
}

// normalizePort compares ports numerically so an int-valued string with
// whitespace or a leading zero still matches.
func normalizePort(port string) string {
	p := strings.TrimSpace(port)
	if n, err := strconv.Atoi(p); err == nil {
		return strconv.Itoa(n)
	}
	return p
}

// buildGUID constructs an internal GUID from instance + dataset name.
func buildGUID(instanceResourceID string, engine storepb.Engine, databaseOverride, datasetName string) string {
	database, schema, table := splitDatasetName(engine, datasetName)
	if databaseOverride != "" {
		database = databaseOverride
	}
	return common.BuildMetaGUID(instanceResourceID, database, schema, table)
}

// splitDatasetName splits a producer's dataset name into the parts a GUID is
// built from. A name may be "database.schema.table", "schema.table" or "table",
// and the two-part form means database.table on an engine that has no schema
// segment to name. Every reader of a dataset name splits it here, so the GUID a
// producer's name resolves to and the context its SQL is analyzed in cannot
// disagree about which part is the database.
func splitDatasetName(engine storepb.Engine, datasetName string) (database, schema, table string) {
	parts := strings.Split(datasetName, ".")
	switch len(parts) {
	case 1:
		return "", "", parts[0]
	case 2:
		if isMySQLLike(engine) {
			return parts[0], "", parts[1]
		}
		return "", parts[0], parts[1]
	default:
		return parts[0], parts[1], strings.Join(parts[2:], ".")
	}
}

// isMySQLLike reports whether the engine addresses objects as database.table
// (rather than schema.table). Every MySQL-wire engine behaves that way,
// including OceanBase, StarRocks and Doris: a synced StarRocks table lives at
// "<instance>;<database>;;<table>", so a two-part producer name is a database
// and a table, never a schema and a table.
func isMySQLLike(engine storepb.Engine) bool {
	switch engine {
	case storepb.Engine_MYSQL,
		storepb.Engine_TIDB,
		storepb.Engine_MARIADB,
		storepb.Engine_OCEANBASE,
		storepb.Engine_STARROCKS,
		storepb.Engine_DORIS:
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
