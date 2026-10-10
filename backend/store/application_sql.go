package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lib/pq"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// applicationSQLGUIDPrefix marks the last GUID segment of a reported statement, so it
// cannot collide with a schema object of the same name.
const applicationSQLGUIDPrefix = "__application_sql__/"

// applicationSQLIDLength is the length of the generated statement identifier. The
// identifier is the object's GUID's last segment, and it is generated rather than derived
// from the fingerprint so that changing the fingerprint rules can rewrite the fingerprint
// without detaching the object from its lineage and history.
const applicationSQLIDLength = 16

// applicationSQLLabelRunes bounds the display label taken from a statement's first line.
const applicationSQLLabelRunes = 120

// ApplicationSQLKind is the read/write classification of a statement. It is written by the
// lineage runner once it has analyzed the statement, and read by the list page to filter.
// It is never the source of the classification itself.
type ApplicationSQLKind int16

const (
	ApplicationSQLKindUnknown ApplicationSQLKind = 0
	ApplicationSQLKindRead    ApplicationSQLKind = 1
	ApplicationSQLKindWrite   ApplicationSQLKind = 2
)

// ApplicationSQLMessage is the store representation of one distinct reported statement.
type ApplicationSQLMessage struct {
	ID                 int64
	GUID               string
	ApplicationSQLID   string
	InstanceResourceID string
	DatabaseName       string
	SchemaName         string
	Engine             storepb.Engine
	Fingerprint        string
	FingerprintVersion int16
	SQLText            string
	Kind               ApplicationSQLKind
	ExecutionCount     int64
	FirstSeenAt        time.Time
	LastSeenAt         time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
	// Observations is the per-application breakdown. The detail read fills it in; a list row
	// leaves it empty, because resolving every statement's reporters would be a query per row.
	Observations []*ApplicationSQLObservationMessage
}

// ReportApplicationSQLMessage is one statement an application reported. The text fields
// are what sqlsanitize.Sanitize produced: the store persists them and does not decide them.
type ReportApplicationSQLMessage struct {
	// ApplicationID is the public id of the reporting application, which the ingest path
	// takes from the validated key and never from the payload.
	ApplicationID      string
	InstanceResourceID string
	DatabaseName       string
	SchemaName         string
	Engine             storepb.Engine
	// SQLText is the parameterized statement, the only form ever stored.
	SQLText string
	// Canonical and Fingerprint are the deduplication key of SQLText.
	Canonical          string
	Fingerprint        string
	FingerprintVersion int16
	// Count is how many times the application ran the statement in this report.
	Count int64
	// FirstSeenAt and LastSeenAt are the application's own view of when it first and last
	// ran the statement. A zero value falls back to the server's receipt time, which is what
	// an application that keeps no such record will send.
	FirstSeenAt time.Time
	LastSeenAt  time.Time
}

// FindApplicationSQLMessage is the query filter for reported statements.
type FindApplicationSQLMessage struct {
	GUID               *string
	ApplicationID      *string
	InstanceResourceID *string
	DatabaseName       *string
	SchemaName         *string
	Kind               *ApplicationSQLKind
	Query              *string
	Limit              *int
	Offset             *int
}

// applicationSQLColumns is qualified with the table name rather than an alias, because the
// INSERT ... RETURNING that records a statement has no FROM clause for an alias to bind to.
const applicationSQLColumns = `application_sql.id, application_sql.guid, application_sql.application_sql_id,
	application_sql.instance_resource_id, application_sql.database_name, application_sql.schema_name,
	application_sql.engine, application_sql.fingerprint, application_sql.fingerprint_version,
	application_sql.sql_text, application_sql.kind, application_sql.execution_count,
	application_sql.first_seen_at, application_sql.last_seen_at, application_sql.created_at, application_sql.updated_at`

// The four writes below are named because their shape is the invariant: a statement's
// conflict target is its identity, an observation adds to a count rather than replacing it,
// the rollup derives a statement's totals from the observations instead of trusting the
// caller, and the orphan sweep is gated on there being no observation left.
// application_sql_pure_test.go asserts each of those.

// applicationSQLUpsertStatement records a statement. On conflict it leaves the generated id
// and the stored text alone: the id is what the object's GUID already points at, and the text
// belongs to whichever reporter came first, since whitespace is not part of the fingerprint.
const applicationSQLUpsertStatement = `
	INSERT INTO application_sql (
		guid, application_sql_id, instance_resource_id, database_name, schema_name, engine,
		fingerprint, fingerprint_version, sql_text, first_seen_at, last_seen_at, content_search, search_vector
	)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $9, to_tsvector('simple', $9))
	ON CONFLICT (instance_resource_id, database_name, schema_name, fingerprint) DO UPDATE SET
		engine = EXCLUDED.engine,
		first_seen_at = LEAST(application_sql.first_seen_at, EXCLUDED.first_seen_at),
		last_seen_at = GREATEST(application_sql.last_seen_at, EXCLUDED.last_seen_at),
		updated_at = NOW()
	RETURNING ` + applicationSQLColumns

// applicationSQLUpsertObservation folds one application's report into its own history with
// the statement.
const applicationSQLUpsertObservation = `
	INSERT INTO application_sql_observation (application_sql_id, application_id, execution_count, first_seen_at, last_seen_at)
	VALUES ($1, $2, $3, $4, $5)
	ON CONFLICT (application_sql_id, application_id) DO UPDATE SET
		execution_count = application_sql_observation.execution_count + EXCLUDED.execution_count,
		first_seen_at = LEAST(application_sql_observation.first_seen_at, EXCLUDED.first_seen_at),
		last_seen_at = GREATEST(application_sql_observation.last_seen_at, EXCLUDED.last_seen_at),
		updated_at = NOW()
`

// applicationSQLRollUpStatement recomputes a statement's totals from its observations. The
// statement row carries them so the list page can sort and page by heat without aggregating
// the observations on every read.
const applicationSQLRollUpStatement = `
	UPDATE application_sql SET
		execution_count = agg.total,
		first_seen_at = agg.first_seen,
		last_seen_at = agg.last_seen,
		updated_at = NOW()
	FROM (
		SELECT COALESCE(SUM(execution_count), 0) AS total,
		       MIN(first_seen_at) AS first_seen,
		       MAX(last_seen_at) AS last_seen
		FROM application_sql_observation WHERE application_sql_id = $1
	) agg
	WHERE application_sql.id = $1
	RETURNING ` + applicationSQLColumns

// applicationSQLDeleteOrphans removes the statements no application reports any more and
// returns their GUIDs, so the caller can clean what hangs off them.
const applicationSQLDeleteOrphans = `
	DELETE FROM application_sql
	WHERE NOT EXISTS (
		SELECT 1 FROM application_sql_observation o WHERE o.application_sql_id = application_sql.id
	)
	RETURNING guid
`

func scanApplicationSQL(row interface{ Scan(...any) error }) (*ApplicationSQLMessage, error) {
	msg := &ApplicationSQLMessage{}
	var engine, kind int16
	if err := row.Scan(
		&msg.ID, &msg.GUID, &msg.ApplicationSQLID, &msg.InstanceResourceID, &msg.DatabaseName,
		&msg.SchemaName, &engine, &msg.Fingerprint, &msg.FingerprintVersion, &msg.SQLText, &kind,
		&msg.ExecutionCount, &msg.FirstSeenAt, &msg.LastSeenAt, &msg.CreatedAt, &msg.UpdatedAt,
	); err != nil {
		return nil, err
	}
	msg.Engine = storepb.Engine(engine)
	msg.Kind = ApplicationSQLKind(kind)
	return msg, nil
}

// buildApplicationSQLGUID names a reported statement in the meta registry. It is built
// from the statement's own identifier rather than its fingerprint so that the object
// survives a change to the fingerprint rules.
func buildApplicationSQLGUID(instanceResourceID, databaseName, schemaName, applicationSQLID string) string {
	return common.BuildMetaGUID(instanceResourceID, databaseName, schemaName, applicationSQLGUIDPrefix+applicationSQLID)
}

// statementLabel is the display name of a statement: its first line, bounded. The full text
// is in SQLText.
func statementLabel(text string) string {
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = text[:i]
	}
	runes := []rune(strings.TrimSpace(text))
	if len(runes) > applicationSQLLabelRunes {
		return string(runes[:applicationSQLLabelRunes]) + "…"
	}
	return string(runes)
}

// ReportApplicationSQL records one reported statement: it upserts the statement by its
// scope and fingerprint, folds the report into that application's observation, recomputes
// the statement's rollups, and mirrors the statement into the meta registry.
//
// The statements a report creates are mirrored whether or not they turn out to be writes.
// The mirror is what makes an object addressable by GUID — GetMetadata, history and Explain
// SQL all read it — while the graph node set is derived from lineage edges, which only a
// write gets. See .agents/docs/application-sql-ingestion.md section 3.5.
func (s *Store) ReportApplicationSQL(ctx context.Context, report *ReportApplicationSQLMessage) (*ApplicationSQLMessage, error) {
	if report == nil {
		return nil, common.Errorf(common.Invalid, "a report is required")
	}
	if strings.TrimSpace(report.ApplicationID) == "" {
		return nil, common.Errorf(common.Invalid, "an application is required")
	}
	if strings.TrimSpace(report.Fingerprint) == "" || strings.TrimSpace(report.SQLText) == "" {
		return nil, common.Errorf(common.Invalid, "a sanitized statement with a fingerprint is required")
	}
	observedAt := time.Now().UTC()
	count := report.Count
	if count < 0 {
		count = 0
	}
	firstSeenAt, lastSeenAt := report.FirstSeenAt, report.LastSeenAt
	if firstSeenAt.IsZero() {
		firstSeenAt = observedAt
	}
	if lastSeenAt.IsZero() {
		lastSeenAt = observedAt
	}
	if lastSeenAt.Before(firstSeenAt) {
		firstSeenAt, lastSeenAt = lastSeenAt, firstSeenAt
	}

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to begin transaction")
	}
	defer tx.Rollback()

	applicationSQLID, err := common.RandomString(applicationSQLIDLength)
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate a statement id")
	}
	guid := buildApplicationSQLGUID(report.InstanceResourceID, report.DatabaseName, report.SchemaName, applicationSQLID)

	statement, err := scanApplicationSQL(tx.QueryRowContext(ctx, applicationSQLUpsertStatement,
		guid, applicationSQLID, report.InstanceResourceID, report.DatabaseName, report.SchemaName, int16(report.Engine),
		report.Fingerprint, report.FingerprintVersion, report.SQLText, firstSeenAt, lastSeenAt))
	if err != nil {
		return nil, errors.Wrap(err, "failed to record the reported statement")
	}
	if _, err := tx.ExecContext(ctx, applicationSQLUpsertObservation,
		statement.ID, report.ApplicationID, count, firstSeenAt, lastSeenAt); err != nil {
		if isForeignKeyViolation(err) {
			return nil, common.Errorf(common.NotFound, "application %q not found", report.ApplicationID)
		}
		return nil, errors.Wrap(err, "failed to record the report observation")
	}

	statement, err = scanApplicationSQL(tx.QueryRowContext(ctx, applicationSQLRollUpStatement, statement.ID))
	if err != nil {
		return nil, errors.Wrap(err, "failed to roll the report up onto the statement")
	}

	if err := s.upsertApplicationSQLMetaRegistryAt(ctx, tx, statement, observedAt); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit transaction")
	}
	return statement, nil
}

// GetApplicationSQL returns one reported statement or nil.
func (s *Store) GetApplicationSQL(ctx context.Context, find *FindApplicationSQLMessage) (*ApplicationSQLMessage, error) {
	list, err := s.ListApplicationSQLs(ctx, find)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	if len(list) > 1 {
		return nil, errors.Errorf("found %d application SQL records with filter %+v, expect 1", len(list), find)
	}
	return list[0], nil
}

// ListApplicationSQLs lists reported statements. It is ordered by most recently seen,
// which is the order the page shows by default.
func (s *Store) ListApplicationSQLs(ctx context.Context, find *FindApplicationSQLMessage) ([]*ApplicationSQLMessage, error) {
	if find == nil {
		find = &FindApplicationSQLMessage{}
	}
	where, args := []string{"TRUE"}, []any{}
	if find.GUID != nil {
		where = append(where, fmt.Sprintf("application_sql.guid = $%d", len(args)+1))
		args = append(args, strings.TrimSpace(*find.GUID))
	}
	if find.InstanceResourceID != nil {
		where = append(where, fmt.Sprintf("application_sql.instance_resource_id = $%d", len(args)+1))
		args = append(args, strings.TrimSpace(*find.InstanceResourceID))
	}
	if find.DatabaseName != nil {
		where = append(where, fmt.Sprintf("application_sql.database_name = $%d", len(args)+1))
		args = append(args, strings.TrimSpace(*find.DatabaseName))
	}
	if find.SchemaName != nil {
		where = append(where, fmt.Sprintf("application_sql.schema_name = $%d", len(args)+1))
		args = append(args, strings.TrimSpace(*find.SchemaName))
	}
	if find.Kind != nil {
		where = append(where, fmt.Sprintf("application_sql.kind = $%d", len(args)+1))
		args = append(args, int16(*find.Kind))
	}
	if find.ApplicationID != nil {
		where = append(where, fmt.Sprintf(`EXISTS (
			SELECT 1 FROM application_sql_observation o
			WHERE o.application_sql_id = application_sql.id AND o.application_id = $%d
		)`, len(args)+1))
		args = append(args, strings.TrimSpace(*find.ApplicationID))
	}
	if find.Query != nil {
		if queryText := strings.TrimSpace(*find.Query); queryText != "" {
			where = append(where, fmt.Sprintf("application_sql.search_vector @@ plainto_tsquery('simple', $%d)", len(args)+1))
			args = append(args, queryText)
		}
	}

	query := `SELECT ` + applicationSQLColumns + ` FROM application_sql
		WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY application_sql.last_seen_at DESC, application_sql.id DESC`
	if find.Limit != nil {
		args = append(args, max(*find.Limit, 0))
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if find.Offset != nil {
		args = append(args, max(*find.Offset, 0))
		query += fmt.Sprintf(" OFFSET $%d", len(args))
	}

	rows, err := s.GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query reported statements")
	}
	defer rows.Close()

	var result []*ApplicationSQLMessage
	for rows.Next() {
		msg, err := scanApplicationSQL(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan a reported statement")
		}
		result = append(result, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows iteration error for reported statements")
	}
	return result, nil
}

// SetApplicationSQLKind records the read/write classification the runner derived. It is
// written after the lineage is written, so a statement's kind and its edges agree.
func (s *Store) SetApplicationSQLKind(ctx context.Context, guid string, kind ApplicationSQLKind) error {
	if _, err := s.GetDB().ExecContext(ctx, `
		UPDATE application_sql SET kind = $2, updated_at = NOW() WHERE guid = $1
	`, strings.TrimSpace(guid), int16(kind)); err != nil {
		return errors.Wrap(err, "failed to record the statement kind")
	}
	return nil
}

// DeleteApplicationSQL removes one reported statement and everything hanging off it: its
// observations, its registry mirror, its lineage and its analysis version.
func (s *Store) DeleteApplicationSQL(ctx context.Context, guid string) error {
	observedAt := time.Now().UTC()

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "failed to begin transaction")
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `DELETE FROM application_sql WHERE guid = $1`, strings.TrimSpace(guid))
	if err != nil {
		return errors.Wrap(err, "failed to delete the reported statement")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected for the statement delete")
	}
	if affected == 0 {
		return common.Errorf(common.NotFound, "application SQL %q not found", guid)
	}

	if err := s.deleteApplicationSQLMetaRegistryTx(ctx, tx, guid, observedAt); err != nil {
		return err
	}
	if err := deleteColumnLineageByMetaTx(ctx, tx, guid); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return errors.Wrap(err, "failed to commit transaction")
	}
	return nil
}

// buildApplicationSQLStoredMetadata is the registry mirror of one statement.
func buildApplicationSQLStoredMetadata(msg *ApplicationSQLMessage) *storepb.StoredMetadata {
	return &storepb.StoredMetadata{
		Type: &storepb.StoredMetadata_ApplicationSqlMetadata{
			ApplicationSqlMetadata: &storepb.ApplicationSQLMetadata{
				ApplicationSqlId: msg.ApplicationSQLID,
				Name:             statementLabel(msg.SQLText),
				SqlText:          msg.SQLText,
				InstanceResource: common.FormatInstance(msg.InstanceResourceID),
				DatabaseName:     msg.DatabaseName,
				SchemaName:       msg.SchemaName,
			},
		},
	}
}

func (s *Store) upsertApplicationSQLMetaRegistryAt(ctx context.Context, tx *sql.Tx, msg *ApplicationSQLMessage, observedAt time.Time) error {
	storedMetadata := buildApplicationSQLStoredMetadata(msg)
	metadataBytes, metaHash, err := CalcStoreMetaHash(storedMetadata)
	if err != nil {
		return errors.Wrap(err, "failed to calculate the application SQL metadata hash")
	}
	_, err = s.BatchCreateMetaRegistryResourceAt(ctx, tx, []*CreateMetaRegistryResourceMessage{{
		MetaRegistryResource: MetaRegistryResource{
			GUID:       msg.GUID,
			ObjectType: storepb.MetaType_APPLICATION_SQL,
			Metadata:   storedMetadata,
			MetaHash:   metaHash,
		},
		MetadataBytes: metadataBytes,
	}}, observedAt)
	if err != nil {
		return errors.Wrap(err, "failed to mirror the reported statement into the meta registry")
	}
	return nil
}

func (s *Store) deleteApplicationSQLMetaRegistryTx(ctx context.Context, tx *sql.Tx, guid string, observedAt time.Time) error {
	list, err := s.listMetaRegistryResourceImpl(ctx, tx, &FindMetaRegistryResourceMessage{GUIDPrefix: &guid}, false)
	if err != nil {
		return errors.Wrap(err, "failed to list the application SQL meta registry entries")
	}
	if err := s.BatchDeleteMetaRegistryAt(ctx, tx, list, observedAt); err != nil {
		return errors.Wrap(err, "failed to delete the application SQL meta registry entry")
	}
	return nil
}

// isForeignKeyViolation reports the SQLSTATE a report gets when it names an application
// that does not exist. It mirrors isUniqueViolation beside it.
func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23503"
	}
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23503"
}

// ApplicationSQLObservationMessage is one application's own history with one statement.
type ApplicationSQLObservationMessage struct {
	ApplicationID string
	// ApplicationName is the application's display name, read with the observation so the
	// detail page does not have to resolve each one.
	ApplicationName string
	ExecutionCount  int64
	FirstSeenAt     time.Time
	LastSeenAt      time.Time
}

// ListApplicationSQLObservations returns who reports one statement, and how much. It is read
// on the statement's detail view rather than with every list row.
func (s *Store) ListApplicationSQLObservations(ctx context.Context, applicationSQLID int64) ([]*ApplicationSQLObservationMessage, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT o.application_id, a.name, o.execution_count, o.first_seen_at, o.last_seen_at
		FROM application_sql_observation o
		JOIN application a ON a.application_id = o.application_id
		WHERE o.application_sql_id = $1
		ORDER BY o.execution_count DESC, o.application_id ASC
	`, applicationSQLID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query the statement observations")
	}
	defer rows.Close()

	var result []*ApplicationSQLObservationMessage
	for rows.Next() {
		msg := &ApplicationSQLObservationMessage{}
		if err := rows.Scan(&msg.ApplicationID, &msg.ApplicationName, &msg.ExecutionCount, &msg.FirstSeenAt, &msg.LastSeenAt); err != nil {
			return nil, errors.Wrap(err, "failed to scan a statement observation")
		}
		result = append(result, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows iteration error for the statement observations")
	}
	return result, nil
}
