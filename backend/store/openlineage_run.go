package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// OpenLineageRunMessage is the store representation of a persisted COMPLETE OpenLineage run.
type OpenLineageRunMessage struct {
	ID                 int64
	GUID               string
	TaskGUID           string
	RunID              string
	JobNamespace       string
	JobName            string
	JobType            string
	EventType          string
	EventTime          *time.Time
	Producer           string
	Integration        string
	ProcessingType     string
	ParentJobNamespace string
	ParentJobName      string
	ParentRunID        string
	RootJobNamespace   string
	RootJobName        string
	RootRunID          string
	Source             string
	InputCount         int32
	OutputCount        int32
	HasLineage         bool
	RawPayload         []byte
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// FindOpenLineageRunMessage is the query filter for persisted OpenLineage runs.
type FindOpenLineageRunMessage struct {
	GUID         *string
	TaskGUID     *string
	RunID        *string
	JobNamespace *string
	JobName      *string
	JobType      *string
	Limit        *int
	Offset       *int
}

// UpsertOpenLineageRun persists a COMPLETE OpenLineage run and mirrors it into meta_registry_resource.
func (s *Store) UpsertOpenLineageRun(ctx context.Context, run *OpenLineageRunMessage) (*OpenLineageRunMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to begin transaction")
	}
	defer tx.Rollback()

	persisted, err := upsertOpenLineageRunImpl(ctx, tx, run)
	if err != nil {
		return nil, err
	}

	if err := s.upsertOpenLineageTask(ctx, tx, persisted); err != nil {
		return nil, err
	}

	if err := s.upsertOpenLineageRunMetaRegistry(ctx, tx, persisted); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit transaction")
	}

	return persisted, nil
}

func upsertOpenLineageRunImpl(ctx context.Context, tx *sql.Tx, run *OpenLineageRunMessage) (*OpenLineageRunMessage, error) {
	var eventTime any
	if run.EventTime != nil {
		eventTime = *run.EventTime
	}

	var persisted OpenLineageRunMessage
	var rawPayload []byte
	var persistedEventTime sql.NullTime
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO openlineage_run (
			guid,
			task_guid,
			run_id,
			job_namespace,
			job_name,
			job_type,
			event_type,
			event_time,
			producer,
			integration,
			processing_type,
			parent_job_namespace,
			parent_job_name,
			parent_run_id,
			root_job_namespace,
			root_job_name,
			root_run_id,
			source,
			input_count,
			output_count,
			has_lineage,
			raw_payload
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22)
		ON CONFLICT (job_namespace, job_name, job_type, run_id) DO UPDATE SET
			guid = EXCLUDED.guid,
			task_guid = EXCLUDED.task_guid,
			event_type = EXCLUDED.event_type,
			event_time = EXCLUDED.event_time,
			producer = EXCLUDED.producer,
			integration = EXCLUDED.integration,
			processing_type = EXCLUDED.processing_type,
			parent_job_namespace = EXCLUDED.parent_job_namespace,
			parent_job_name = EXCLUDED.parent_job_name,
			parent_run_id = EXCLUDED.parent_run_id,
			root_job_namespace = EXCLUDED.root_job_namespace,
			root_job_name = EXCLUDED.root_job_name,
			root_run_id = EXCLUDED.root_run_id,
			source = EXCLUDED.source,
			input_count = EXCLUDED.input_count,
			output_count = EXCLUDED.output_count,
			has_lineage = EXCLUDED.has_lineage,
			raw_payload = EXCLUDED.raw_payload,
			updated_at = NOW()
		RETURNING
			id,
			guid,
			task_guid,
			run_id,
			job_namespace,
			job_name,
			job_type,
			event_type,
			event_time,
			producer,
			integration,
			processing_type,
			parent_job_namespace,
			parent_job_name,
			parent_run_id,
			root_job_namespace,
			root_job_name,
			root_run_id,
			source,
			input_count,
			output_count,
			has_lineage,
			raw_payload,
			created_at,
			updated_at
	`, run.GUID, run.TaskGUID, run.RunID, run.JobNamespace, run.JobName, run.JobType, run.EventType, eventTime, run.Producer, run.Integration, run.ProcessingType, run.ParentJobNamespace, run.ParentJobName, run.ParentRunID, run.RootJobNamespace, run.RootJobName, run.RootRunID, run.Source, run.InputCount, run.OutputCount, run.HasLineage, run.RawPayload).Scan(
		&persisted.ID,
		&persisted.GUID,
		&persisted.TaskGUID,
		&persisted.RunID,
		&persisted.JobNamespace,
		&persisted.JobName,
		&persisted.JobType,
		&persisted.EventType,
		&persistedEventTime,
		&persisted.Producer,
		&persisted.Integration,
		&persisted.ProcessingType,
		&persisted.ParentJobNamespace,
		&persisted.ParentJobName,
		&persisted.ParentRunID,
		&persisted.RootJobNamespace,
		&persisted.RootJobName,
		&persisted.RootRunID,
		&persisted.Source,
		&persisted.InputCount,
		&persisted.OutputCount,
		&persisted.HasLineage,
		&rawPayload,
		&persisted.CreatedAt,
		&persisted.UpdatedAt,
	); err != nil {
		return nil, errors.Wrap(err, "failed to upsert openlineage run")
	}
	if persistedEventTime.Valid {
		t := persistedEventTime.Time
		persisted.EventTime = &t
	}
	persisted.RawPayload = rawPayload
	return &persisted, nil
}

func (s *Store) upsertOpenLineageRunMetaRegistry(ctx context.Context, tx *sql.Tx, run *OpenLineageRunMessage) error {
	storedMetadata := buildOpenLineageRunStoredMetadata(run)
	metadataBytes, metaHash, err := CalcStoreMetaHash(storedMetadata)
	if err != nil {
		return errors.Wrap(err, "failed to calculate openlineage run metadata hash")
	}

	_, err = s.BatchCreateMetaRegistryResource(ctx, tx, []*CreateMetaRegistryResourceMessage{
		{
			MetaRegistryResource: MetaRegistryResource{
				GUID:       run.GUID,
				ObjectType: storepb.MetaType_OPENLINEAGE,
				Metadata:   storedMetadata,
				MetaHash:   metaHash,
			},
			MetadataBytes: metadataBytes,
		},
	})
	if err != nil {
		return errors.Wrap(err, "failed to mirror openlineage run into meta registry")
	}

	return nil
}

// GetOpenLineageRun returns one persisted run or nil if none exists.
func (s *Store) GetOpenLineageRun(ctx context.Context, find *FindOpenLineageRunMessage) (*OpenLineageRunMessage, error) {
	list, err := s.ListOpenLineageRun(ctx, find)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	return list[0], nil
}

// ListOpenLineageRun lists persisted OpenLineage runs matching the filter.
func (s *Store) ListOpenLineageRun(ctx context.Context, find *FindOpenLineageRunMessage) ([]*OpenLineageRunMessage, error) {
	where, args := []string{"TRUE"}, []any{}
	if v := find.GUID; v != nil {
		where, args = append(where, fmt.Sprintf("guid = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.TaskGUID; v != nil {
		where, args = append(where, fmt.Sprintf("task_guid = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.RunID; v != nil {
		where, args = append(where, fmt.Sprintf("run_id = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.JobNamespace; v != nil {
		where, args = append(where, fmt.Sprintf("job_namespace = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.JobName; v != nil {
		where, args = append(where, fmt.Sprintf("job_name = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.JobType; v != nil {
		where, args = append(where, fmt.Sprintf("job_type = $%d", len(args)+1)), append(args, *v)
	}

	query := `
		SELECT
			id,
			guid,
			task_guid,
			run_id,
			job_namespace,
			job_name,
			job_type,
			event_type,
			event_time,
			producer,
			integration,
			processing_type,
			parent_job_namespace,
			parent_job_name,
			parent_run_id,
			root_job_namespace,
			root_job_name,
			root_run_id,
			source,
			input_count,
			output_count,
			has_lineage,
			raw_payload,
			created_at,
			updated_at
		FROM openlineage_run
		WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY event_time DESC NULLS LAST, id DESC`

	if v := find.Limit; v != nil {
		query += fmt.Sprintf(" LIMIT %d", *v)
	}
	if v := find.Offset; v != nil {
		query += fmt.Sprintf(" OFFSET %d", *v)
	}

	rows, err := s.GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query openlineage runs")
	}
	defer rows.Close()

	var result []*OpenLineageRunMessage
	for rows.Next() {
		var msg OpenLineageRunMessage
		var eventTime sql.NullTime
		var rawPayload []byte
		if err := rows.Scan(
			&msg.ID,
			&msg.GUID,
			&msg.TaskGUID,
			&msg.RunID,
			&msg.JobNamespace,
			&msg.JobName,
			&msg.JobType,
			&msg.EventType,
			&eventTime,
			&msg.Producer,
			&msg.Integration,
			&msg.ProcessingType,
			&msg.ParentJobNamespace,
			&msg.ParentJobName,
			&msg.ParentRunID,
			&msg.RootJobNamespace,
			&msg.RootJobName,
			&msg.RootRunID,
			&msg.Source,
			&msg.InputCount,
			&msg.OutputCount,
			&msg.HasLineage,
			&rawPayload,
			&msg.CreatedAt,
			&msg.UpdatedAt,
		); err != nil {
			return nil, errors.Wrap(err, "failed to scan openlineage run")
		}
		if eventTime.Valid {
			t := eventTime.Time
			msg.EventTime = &t
		}
		msg.RawPayload = rawPayload
		result = append(result, &msg)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows iteration error")
	}
	return result, nil
}

func buildOpenLineageRunStoredMetadata(run *OpenLineageRunMessage) *storepb.StoredMetadata {
	summary := &storepb.OpenLineageRunSummary{
		Guid:               run.GUID,
		TaskGuid:           run.TaskGUID,
		RunId:              run.RunID,
		JobNamespace:       run.JobNamespace,
		JobName:            run.JobName,
		JobType:            run.JobType,
		EventType:          run.EventType,
		Producer:           run.Producer,
		Source:             run.Source,
		Integration:        run.Integration,
		ProcessingType:     run.ProcessingType,
		ParentJobNamespace: run.ParentJobNamespace,
		ParentJobName:      run.ParentJobName,
		ParentRunId:        run.ParentRunID,
		RootJobNamespace:   run.RootJobNamespace,
		RootJobName:        run.RootJobName,
		RootRunId:          run.RootRunID,
		InputCount:         run.InputCount,
		OutputCount:        run.OutputCount,
		HasLineage:         run.HasLineage,
		CreatedAt:          timestamppb.New(run.CreatedAt),
		UpdatedAt:          timestamppb.New(run.UpdatedAt),
	}
	if run.EventTime != nil {
		summary.EventTime = timestamppb.New(*run.EventTime)
	}

	return &storepb.StoredMetadata{
		Type: &storepb.StoredMetadata_OpenlineageRunSummary{
			OpenlineageRunSummary: summary,
		},
	}
}

// MarshalOpenLineageRunPayload normalizes the stored raw payload for API responses.
func MarshalOpenLineageRunPayload(run *OpenLineageRunMessage) ([]byte, error) {
	message := &storepb.OpenLineageRun{
		Id:         run.ID,
		Summary:    buildOpenLineageRunStoredMetadata(run).GetOpenlineageRunSummary(),
		RawPayload: run.RawPayload,
	}
	b, err := protojson.Marshal(message)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal openlineage run payload")
	}
	return b, nil
}
