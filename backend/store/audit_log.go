package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

type FindAuditLogMessage struct {
	Parent *string
	Limit  *int
	Offset *int
	Filter *ListResourceFilter
}

func (s *Store) CreateAuditLog(ctx context.Context, auditLog *storepb.AuditLog) (*storepb.AuditLog, error) {
	if auditLog == nil {
		return nil, errors.New("audit log is required")
	}

	cloned, ok := proto.Clone(auditLog).(*storepb.AuditLog)
	if !ok {
		return nil, errors.New("failed to clone audit log")
	}

	createTime := time.Now().UTC()
	if cloned.CreateTime != nil {
		createTime = cloned.CreateTime.AsTime().UTC()
	}
	cloned.CreateTime = timestamppb.New(createTime)

	payload, err := protojson.Marshal(cloned)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal audit log payload")
	}

	var id int64
	var createdAt time.Time
	if err := s.GetDB().QueryRowContext(ctx, `
		INSERT INTO audit_log (created_at, payload)
		VALUES ($1, $2)
		RETURNING id, created_at
	`, createTime, payload).Scan(&id, &createdAt); err != nil {
		return nil, errors.Wrap(err, "failed to create audit log")
	}

	cloned.Id = id
	cloned.CreateTime = timestamppb.New(createdAt)
	return cloned, nil
}

func (s *Store) SearchAuditLogs(ctx context.Context, find *FindAuditLogMessage) ([]*storepb.AuditLog, error) {
	where, args := []string{"TRUE"}, []any{}
	if find != nil {
		if find.Filter != nil {
			where = append(where, find.Filter.Where)
			args = append(args, find.Filter.Args...)
		}
		if find.Parent != nil && strings.TrimSpace(*find.Parent) != "" {
			where = append(where, fmt.Sprintf("payload->>'parent' = $%d", len(args)+1))
			args = append(args, *find.Parent)
		}
	}

	query := `
		SELECT id, created_at, payload
		FROM audit_log
		WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY created_at DESC, id DESC`

	if find != nil && find.Limit != nil {
		query += fmt.Sprintf(" LIMIT $%d", len(args)+1)
		args = append(args, *find.Limit)
	}
	if find != nil && find.Offset != nil {
		query += fmt.Sprintf(" OFFSET $%d", len(args)+1)
		args = append(args, *find.Offset)
	}

	rows, err := s.GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to search audit logs")
	}
	defer rows.Close()

	var auditLogs []*storepb.AuditLog
	for rows.Next() {
		auditLog, err := scanAuditLog(rows)
		if err != nil {
			return nil, err
		}
		auditLogs = append(auditLogs, auditLog)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate audit logs")
	}

	return auditLogs, nil
}

func scanAuditLog(scanner interface {
	Scan(dest ...any) error
}) (*storepb.AuditLog, error) {
	var (
		id        int64
		createdAt time.Time
		payload   []byte
	)
	if err := scanner.Scan(&id, &createdAt, &payload); err != nil {
		return nil, errors.Wrap(err, "failed to scan audit log")
	}

	auditLog := &storepb.AuditLog{}
	if err := common.ProtojsonUnmarshaler.Unmarshal(payload, auditLog); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal audit log payload")
	}
	auditLog.Id = id
	auditLog.CreateTime = timestamppb.New(createdAt)
	return auditLog, nil
}
