package openlineage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

func TestParseNamespace(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		wantHost  string
		wantPort  string
		wantDB    string
	}{
		{
			name:      "postgres with database",
			namespace: "postgres://myhost:5432/mydb",
			wantHost:  "myhost",
			wantPort:  "5432",
			wantDB:    "mydb",
		},
		{
			name:      "postgresql scheme",
			namespace: "postgresql://pg-server:5433/analytics",
			wantHost:  "pg-server",
			wantPort:  "5433",
			wantDB:    "analytics",
		},
		{
			name:      "mysql without database",
			namespace: "mysql://myhost:3306",
			wantHost:  "myhost",
			wantPort:  "3306",
			wantDB:    "",
		},
		{
			name:      "mysql with database",
			namespace: "mysql://db-server:3306/orders",
			wantHost:  "db-server",
			wantPort:  "3306",
			wantDB:    "orders",
		},
		{
			name:      "clickhouse",
			namespace: "clickhouse://ch-node:8123/default",
			wantHost:  "ch-node",
			wantPort:  "8123",
			wantDB:    "default",
		},
		{
			name:      "snowflake",
			namespace: "snowflake://account.snowflakecomputing.com:443/mydb",
			wantHost:  "account.snowflakecomputing.com",
			wantPort:  "443",
			wantDB:    "mydb",
		},
		{
			name:      "tidb",
			namespace: "tidb://tidb-server:4000/testdb",
			wantHost:  "tidb-server",
			wantPort:  "4000",
			wantDB:    "testdb",
		},
		{
			name:      "no port",
			namespace: "postgres://myhost/mydb",
			wantHost:  "myhost",
			wantPort:  "",
			wantDB:    "mydb",
		},
		{
			name:      "s3 namespace returns empty",
			namespace: "s3://my-bucket/path",
			wantHost:  "",
			wantPort:  "",
			wantDB:    "",
		},
		{
			name:      "kafka namespace returns empty",
			namespace: "kafka://broker:9092",
			wantHost:  "",
			wantPort:  "",
			wantDB:    "",
		},
		{
			name:      "hdfs namespace returns empty",
			namespace: "hdfs://namenode:9000/data",
			wantHost:  "",
			wantPort:  "",
			wantDB:    "",
		},
		{
			name:      "invalid URL",
			namespace: "://invalid",
			wantHost:  "",
			wantPort:  "",
			wantDB:    "",
		},
		{
			name:      "plain string",
			namespace: "just-a-string",
			wantHost:  "",
			wantPort:  "",
			wantDB:    "",
		},
		{
			name:      "empty string",
			namespace: "",
			wantHost:  "",
			wantPort:  "",
			wantDB:    "",
		},
		// Airflow-specific namespace formats.
		{
			name:      "bigquery bare namespace",
			namespace: "bigquery",
			wantHost:  "bigquery",
			wantPort:  "",
			wantDB:    "",
		},
		{
			name:      "bigquery with project",
			namespace: "bigquery://myproject",
			wantHost:  "myproject",
			wantPort:  "",
			wantDB:    "",
		},
		{
			name:      "bigquery with project and dataset",
			namespace: "bigquery://myproject/mydataset",
			wantHost:  "myproject",
			wantPort:  "",
			wantDB:    "mydataset",
		},
		{
			name:      "hive namespace",
			namespace: "hive://hive-server:10000/mydb",
			wantHost:  "hive-server",
			wantPort:  "10000",
			wantDB:    "mydb",
		},
		{
			name:      "spark namespace",
			namespace: "spark://spark-master:7077/default",
			wantHost:  "spark-master",
			wantPort:  "7077",
			wantDB:    "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, port, db := parseNamespace(tt.namespace)
			assert.Equal(t, tt.wantHost, host)
			assert.Equal(t, tt.wantPort, port)
			assert.Equal(t, tt.wantDB, db)
		})
	}
}

func TestMatchHostPort(t *testing.T) {
	tests := []struct {
		name   string
		dsHost string
		dsPort string
		nsHost string
		nsPort string
		want   bool
	}{
		{
			name:   "exact match",
			dsHost: "myhost",
			dsPort: "5432",
			nsHost: "myhost",
			nsPort: "5432",
			want:   true,
		},
		{
			name:   "case insensitive host",
			dsHost: "MyHost",
			dsPort: "5432",
			nsHost: "myhost",
			nsPort: "5432",
			want:   true,
		},
		{
			name:   "namespace no port matches any",
			dsHost: "myhost",
			dsPort: "5432",
			nsHost: "myhost",
			nsPort: "",
			want:   true,
		},
		{
			name:   "different host",
			dsHost: "host1",
			dsPort: "5432",
			nsHost: "host2",
			nsPort: "5432",
			want:   false,
		},
		{
			name:   "different port",
			dsHost: "myhost",
			dsPort: "5432",
			nsHost: "myhost",
			nsPort: "5433",
			want:   false,
		},
		{
			// The instance is registered as localhost while the producer builds
			// its namespace from a JDBC URL that says 127.0.0.1. Comparing the
			// spellings literally turned every dataset of that instance into an
			// external dataset.
			name:   "localhost instance against a 127.0.0.1 namespace",
			dsHost: "localhost",
			dsPort: "5432",
			nsHost: "127.0.0.1",
			nsPort: "5432",
			want:   true,
		},
		{
			name:   "127.0.0.1 instance against a localhost namespace",
			dsHost: "127.0.0.1",
			dsPort: "9030",
			nsHost: "localhost",
			nsPort: "9030",
			want:   true,
		},
		{
			name:   "ipv6 loopback",
			dsHost: "localhost",
			dsPort: "5432",
			nsHost: "::1",
			nsPort: "5432",
			want:   true,
		},
		{
			name:   "bracketed ipv6 loopback in the data source",
			dsHost: "[::1]",
			dsPort: "5432",
			nsHost: "127.0.0.1",
			nsPort: "5432",
			want:   true,
		},
		{
			name:   "padded port",
			dsHost: "myhost",
			dsPort: " 5432 ",
			nsHost: "myhost",
			nsPort: "5432",
			want:   true,
		},
		{
			name:   "loopback aliases still respect the port",
			dsHost: "localhost",
			dsPort: "5432",
			nsHost: "127.0.0.1",
			nsPort: "5433",
			want:   false,
		},
		{
			name:   "loopback does not swallow a real host",
			dsHost: "localhost",
			dsPort: "5432",
			nsHost: "db.internal",
			nsPort: "5432",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchHostPort(tt.dsHost, tt.dsPort, tt.nsHost, tt.nsPort)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildGUID(t *testing.T) {
	tests := []struct {
		name       string
		instanceID string
		engine     storepb.Engine
		dbOverride string
		dataset    string
		want       string
	}{
		{
			name:       "pg: schema.table with db override",
			instanceID: "inst-1",
			engine:     storepb.Engine_POSTGRES,
			dbOverride: "mydb",
			dataset:    "public.users",
			want:       "inst-1;mydb;public;users",
		},
		{
			name:       "pg: db.schema.table",
			instanceID: "inst-1",
			engine:     storepb.Engine_POSTGRES,
			dbOverride: "",
			dataset:    "mydb.public.users",
			want:       "inst-1;mydb;public;users",
		},
		{
			name:       "pg: table only with db override",
			instanceID: "inst-1",
			engine:     storepb.Engine_POSTGRES,
			dbOverride: "mydb",
			dataset:    "users",
			want:       "inst-1;mydb;;users",
		},
		{
			name:       "mysql: db.table (2 parts = db.table for mysql)",
			instanceID: "inst-2",
			engine:     storepb.Engine_MYSQL,
			dbOverride: "",
			dataset:    "orders.items",
			want:       "inst-2;orders;;items",
		},
		{
			name:       "mysql: db.table with db override",
			instanceID: "inst-2",
			engine:     storepb.Engine_MYSQL,
			dbOverride: "overridedb",
			dataset:    "orders.items",
			want:       "inst-2;overridedb;;items",
		},
		{
			name:       "mysql: table only",
			instanceID: "inst-2",
			engine:     storepb.Engine_MYSQL,
			dbOverride: "mydb",
			dataset:    "users",
			want:       "inst-2;mydb;;users",
		},
		{
			name:       "tidb: db.table",
			instanceID: "inst-3",
			engine:     storepb.Engine_TIDB,
			dbOverride: "",
			dataset:    "testdb.accounts",
			want:       "inst-3;testdb;;accounts",
		},
		{
			// A synced StarRocks table lives at "<instance>;<database>;;<table>",
			// so the two-part name a DataX StarRocks writer emits is db.table.
			name:       "starrocks: db.table",
			instanceID: "starrocks-dev-1",
			engine:     storepb.Engine_STARROCKS,
			dbOverride: "",
			dataset:    "e2e_ods.dwd_order_fact",
			want:       "starrocks-dev-1;e2e_ods;;dwd_order_fact",
		},
		{
			name:       "doris: db.table",
			instanceID: "doris-dev-1",
			engine:     storepb.Engine_DORIS,
			dbOverride: "",
			dataset:    "e2e_ods.dwd_order_fact",
			want:       "doris-dev-1;e2e_ods;;dwd_order_fact",
		},
		{
			// The three-part form stays database.schema.table on any engine, so a
			// producer that wants to be explicit still is.
			name:       "starrocks: db.empty-schema.table",
			instanceID: "starrocks-dev-1",
			engine:     storepb.Engine_STARROCKS,
			dbOverride: "",
			dataset:    "e2e_ods..dwd_order_fact",
			want:       "starrocks-dev-1;e2e_ods;;dwd_order_fact",
		},
		{
			name:       "pg: schema.table no override",
			instanceID: "inst-1",
			engine:     storepb.Engine_POSTGRES,
			dbOverride: "",
			dataset:    "public.users",
			want:       "inst-1;;public;users",
		},
		{
			name:       "pg: db.schema.table.extra dots",
			instanceID: "inst-1",
			engine:     storepb.Engine_POSTGRES,
			dbOverride: "",
			dataset:    "mydb.public.dotted.table.name",
			want:       "inst-1;mydb;public;dotted.table.name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildGUID(tt.instanceID, tt.engine, tt.dbOverride, tt.dataset)
			assert.Equal(t, tt.want, got)
		})
	}
}

// A dataset name splits the same way wherever it is read, so the GUID a
// producer's name resolves to cannot disagree with the context its SQL is
// analyzed in.
func TestSplitDatasetName(t *testing.T) {
	tests := []struct {
		name       string
		engine     storepb.Engine
		dataset    string
		wantDB     string
		wantSchema string
		wantTable  string
	}{
		{name: "mysql db.table", engine: storepb.Engine_MYSQL, dataset: "e2e_ods.orders", wantDB: "e2e_ods", wantTable: "orders"},
		{name: "starrocks db.table", engine: storepb.Engine_STARROCKS, dataset: "e2e_ods.dwd_order_fact", wantDB: "e2e_ods", wantTable: "dwd_order_fact"},
		{name: "pg schema.table", engine: storepb.Engine_POSTGRES, dataset: "public.orders", wantSchema: "public", wantTable: "orders"},
		{name: "pg db.schema.table", engine: storepb.Engine_POSTGRES, dataset: "e2e.e2e_dwd.dwd_order_fact", wantDB: "e2e", wantSchema: "e2e_dwd", wantTable: "dwd_order_fact"},
		{name: "bare table", engine: storepb.Engine_POSTGRES, dataset: "orders", wantTable: "orders"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			database, schema, table := splitDatasetName(tt.engine, tt.dataset)
			assert.Equal(t, tt.wantDB, database)
			assert.Equal(t, tt.wantSchema, schema)
			assert.Equal(t, tt.wantTable, table)
		})
	}
}

func TestIsMySQLLike(t *testing.T) {
	assert.True(t, isMySQLLike(storepb.Engine_MYSQL))
	assert.True(t, isMySQLLike(storepb.Engine_TIDB))
	assert.True(t, isMySQLLike(storepb.Engine_MARIADB))
	assert.True(t, isMySQLLike(storepb.Engine_OCEANBASE))
	assert.True(t, isMySQLLike(storepb.Engine_STARROCKS))
	assert.True(t, isMySQLLike(storepb.Engine_DORIS))
	assert.False(t, isMySQLLike(storepb.Engine_POSTGRES))
	assert.False(t, isMySQLLike(storepb.Engine_ENGINE_UNSPECIFIED))
}

func TestInferDatasetType(t *testing.T) {
	tests := []struct {
		namespace string
		want      string
	}{
		{"s3://bucket/path", "s3"},
		{"s3a://bucket/path", "s3"},
		{"s3n://bucket/path", "s3"},
		{"gs://bucket/path", "gcs"},
		{"gcs://bucket/path", "gcs"},
		{"hdfs://namenode:9000/data", "hdfs"},
		{"kafka://broker:9092", "kafka"},
		{"file:///tmp/data.csv", "file"},
		{"postgres://host:5432/db", "database"},
		{"postgresql://host:5432/db", "database"},
		{"mysql://host:3306/db", "database"},
		{"snowflake://account/db", "database"},
		{"oracle://host:1521/db", "database"},
		{"mssql://host:1433/db", "database"},
		{"unknown://host", "unknown"},
		{"ftp://host/path", "unknown"},
		{"", "unknown"},
		// Airflow-specific dataset types.
		{"bigquery://project/dataset", "bigquery"},
		{"bigquery", "bigquery"},
		{"hive://host:10000/mydb", "hive"},
		{"spark://master:7077/db", "spark"},
	}

	for _, tt := range tests {
		t.Run(tt.namespace, func(t *testing.T) {
			got := InferDatasetType(tt.namespace)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsExternalGUID(t *testing.T) {
	assert.True(t, IsExternalGUID("external:kafka://broker:9092:topic"))
	assert.True(t, IsExternalGUID("external:s3://bucket:path/file"))
	assert.False(t, IsExternalGUID("inst-1;db;schema;table"))
	assert.False(t, IsExternalGUID(""))
}

func TestFormatExternalGUID(t *testing.T) {
	guid := FormatExternalGUID("kafka://broker:9092", "order_events")
	assert.Equal(t, "external:kafka://broker:9092:order_events", guid)

	guid = FormatExternalGUID("s3://my-bucket", "data/file.parquet")
	assert.Equal(t, "external:s3://my-bucket:data/file.parquet", guid)
}

func TestRequestScopedResolverMemoizesPreview(t *testing.T) {
	// A nil store proves the cached answer is served without any lookup.
	resolver := NewRequestScopedResolver(nil)
	want := &ResolvedDataset{GUID: "inst;db;public;orders", MetaType: storepb.MetaType_TABLE, Internal: true}
	resolver.previews[previewKey{namespace: "postgres://host:5432/db", name: "public.orders"}] = want

	got, err := resolver.ResolveDatasetPreview(context.Background(), "postgres://host:5432/db", "public.orders")
	assert.NoError(t, err)
	assert.Same(t, want, got)
}

func TestNewResolverDoesNotMemoize(t *testing.T) {
	// Ingestion resolvers outlive one resolution, so they must not cache.
	assert.False(t, NewResolver(nil).requestScoped)
	assert.True(t, NewRequestScopedResolver(nil).requestScoped)
}
