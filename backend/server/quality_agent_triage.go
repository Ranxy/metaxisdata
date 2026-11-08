package server

import (
	"context"
	"fmt"
	"time"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality/agentrunner"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// qualityTriageContext reads the bounded table context a triage wake needs. It is
// wired into the agent engine so the engine package does not depend on the whole store.
type qualityTriageContext struct {
	store        *store.Store
	now          func() time.Time
	loadSettings func(ctx context.Context) (dataquality.Settings, error)
}

// ReadTableContext loads the registry schema, stored profiles, accepted semantic
// labels and lineage degree of one target. It never reads sample rows.
func (c *qualityTriageContext) ReadTableContext(ctx context.Context, targetGUID string, targetType storepb.MetaType) (*agentrunner.TableContext, error) {
	resolution, err := dataquality.ResolveTarget(ctx, c.store, targetGUID, targetType)
	if err != nil {
		return nil, err
	}
	settings, err := c.loadSettings(ctx)
	if err != nil {
		return nil, err
	}
	target := resolution.Target
	context := &agentrunner.TableContext{
		TargetGUID: targetGUID,
		TargetType: targetType.String(),
		Engine:     target.Engine.String(),
		Instance:   target.InstanceResourceID,
		Database:   target.Database,
		Schema:     target.Schema,
		Object:     target.Table,
	}
	keyColumns := make(map[string]bool, len(target.KeyColumns))
	for _, column := range target.KeyColumns {
		keyColumns[column] = true
	}
	for _, column := range target.Columns {
		if len(context.Columns) >= maxTriageContextColumns {
			break
		}
		context.Columns = append(context.Columns, agentrunner.TableContextColumn{
			Name:     column.Name,
			Type:     column.Type,
			Nullable: column.Nullable,
			Key:      keyColumns[column.Name],
		})
	}
	profiles, err := c.store.ListColumnProfiles(ctx, &store.FindColumnProfileMessage{
		TableGUID: &targetGUID,
		TableType: &targetType,
	})
	if err != nil {
		return nil, err
	}
	for _, profile := range profiles {
		if len(context.Profiles) >= maxTriageContextProfiles {
			break
		}
		summary := agentrunner.TableContextProfile{
			Column:        profile.ColumnName,
			DistinctCount: profile.Profile.GetDistinctCount(),
			ProfiledAt:    profile.ProfiledAt.UTC().Format(time.RFC3339),
		}
		if total := profile.Profile.GetTotalRows(); total > 0 {
			summary.NullFraction = float32(float64(profile.Profile.GetNullCount()) / float64(total))
		}
		// Raw extremes and frequent values can be personal data. Only the statistics are
		// sent by default; the workspace's existing opt-in gates the values, exactly as the
		// other LLM analysis paths do.
		if settings.SendSampleValuesToLLM {
			summary.Min = profile.Profile.GetMinValue()
			summary.Max = profile.Profile.GetMaxValue()
			if values := profile.Profile.GetTopValues(); len(values) > 0 {
				top := ""
				for index, value := range values {
					if index >= 5 {
						break
					}
					if index > 0 {
						top += ", "
					}
					top += value.GetValue()
				}
				summary.TopValues = top
			}
		}
		context.Profiles = append(context.Profiles, summary)
	}
	labels, err := c.store.ListColumnSemanticLabels(ctx, &store.FindColumnSemanticLabelMessage{
		TableGUID: &targetGUID,
		TableType: &targetType,
	})
	if err != nil {
		return nil, err
	}
	for _, label := range labels {
		if label.Status != storepb.QualityLabelStatus_QUALITY_LABEL_STATUS_ACCEPTED {
			continue
		}
		if context.Labels == nil {
			context.Labels = map[string]string{}
		}
		context.Labels[label.ColumnName] = label.Label.String()
	}
	counts, err := c.store.CountColumnLineageByGUID(ctx, []string{targetGUID})
	if err != nil {
		return nil, err
	}
	if count := counts[targetGUID]; count != nil {
		context.Upstream = count.Upstream
		context.Downstream = count.Downstream
	}
	now := c.now()
	changes, err := c.store.ListMetaRegistrySchemaChangeTimes(ctx,
		[]store.MetaGUIDKey{{GUID: targetGUID, ObjectType: targetType}},
		now.Add(-7*24*time.Hour), now)
	if err != nil {
		return nil, err
	}
	for _, changedAt := range changes {
		context.SchemaStale = !changedAt.IsZero()
	}
	return context, nil
}

// Bounds of the triage context: enough to reason about a table, small enough not to
// flood the model context or the tool result.
const (
	maxTriageContextColumns  = 100
	maxTriageContextProfiles = 100
)

// qualityDraftChecks drafts checks through the shared Suggester, so the drafts land in
// the suggestion review queue exactly like a person's request would.
type qualityDraftChecks struct {
	suggester *dataquality.Suggester
}

// DraftChecks drafts checks for one target from a natural-language intent.
func (d *qualityDraftChecks) DraftChecks(ctx context.Context, targetGUID string, targetType storepb.MetaType, columns []string, intent string, createdBy int32) ([]string, error) {
	config, settings, err := d.suggester.ResolveConfig(ctx)
	if err != nil {
		return nil, err
	}
	createdByPointer := createdBy
	queued, _, diagnostic, err := d.suggester.Draft(ctx, config, dataquality.LimitsFromSettings(settings),
		targetGUID, targetType, columns, intent, "", false, &createdByPointer)
	if err != nil {
		if diagnostic != nil && diagnostic.GetErrorCode() != "" {
			return nil, fmt.Errorf("%w (%s)", err, diagnostic.GetErrorCode())
		}
		return nil, err
	}
	titles := make([]string, 0, len(queued))
	for _, suggestion := range queued {
		titles = append(titles, suggestion.Payload.GetRule().GetTitle())
	}
	return titles, nil
}
