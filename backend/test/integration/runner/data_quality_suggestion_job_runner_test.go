//go:build integration

package runner

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/llm"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/migrator"
	"github.com/Ranxy/metaxisdata/backend/runner/qualitysuggestionrunner"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// realStoreDraftSuggester is the fake drafting pipeline the runner integration test
// drives: per call it inserts a real PENDING suggestion through the store and
// returns it, so the success path exercises the real suggestion write as well as
// the job bookkeeping.
type realStoreDraftSuggester struct {
	store *store.Store
}

func (*realStoreDraftSuggester) ResolveConfig(context.Context) (llm.ResolvedConfig, dataquality.Settings, error) {
	return llm.ResolvedConfig{
		ProfileName: "profiles/integration",
		ModelName:   "integration-model",
	}, dataquality.Settings{}, nil
}

func (s *realStoreDraftSuggester) Draft(ctx context.Context, config llm.ResolvedConfig, targetGUID string, metaType storepb.MetaType, _ []string, _ string, _ string, createdBy *int32) ([]*store.QualitySuggestionMessage, *storepb.QualityLLMDiagnostic, error) {
	resourceID, err := common.RandomString(12)
	if err != nil {
		return nil, nil, err
	}
	created, err := s.store.CreateQualitySuggestion(ctx, &store.QualitySuggestionMessage{
		ResourceID: resourceID,
		Kind:       storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT,
		Source:     storepb.QualitySuggestionSource_QUALITY_SUGGESTION_SOURCE_NATURAL_LANGUAGE,
		TargetGUID: targetGUID,
		TargetType: metaType,
		Payload: &storepb.QualitySuggestionPayload{
			Payload: &storepb.QualitySuggestionPayload_Rule{
				Rule: &storepb.QualityRuleDraftConfig{
					Title:      "draft for " + targetGUID,
					Rationale:  "created by the runner integration test",
					Confidence: 0.9,
				},
			},
		},
		Status:        storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING,
		Provider:      config.ProfileName,
		Model:         config.ModelName,
		PromptVersion: dataquality.RuleDraftPromptVersion,
		CreatedBy:     createdBy,
	})
	if err != nil {
		return nil, nil, err
	}
	return []*store.QualitySuggestionMessage{created}, nil, nil
}

// newIsolatedMetadataStore creates a fresh metadata database on the shared
// PostgreSQL server and migrates it. The job runner polls the store it is given, so
// an isolated database keeps the test's PENDING job away from the server's own
// registered runner (which shares the default metadata store and would claim it at
// its next tick).
func newIsolatedMetadataStore(ctx context.Context, t *testing.T) *store.Store {
	t.Helper()

	env := sharedPostgresServiceEnvNoReset(t)
	resourceID, err := common.RandomString(8)
	require.NoError(t, err)
	databaseName := "it_dq_job_" + strings.ToLower(resourceID)
	require.NoError(t, env.ExecPostgres(ctx, "postgres",
		fmt.Sprintf("CREATE DATABASE %s;", quotePostgresIdentifier(databaseName))))
	t.Cleanup(func() {
		// An open pool would block DROP DATABASE; terminate it first.
		_ = env.ExecPostgres(context.Background(), "postgres", fmt.Sprintf(`
			SELECT pg_terminate_backend(pid)
			FROM pg_stat_activity
			WHERE datname = '%s' AND pid <> pg_backend_pid();`, quotePostgresStringLiteral(databaseName)))
		_ = env.ExecPostgres(context.Background(), "postgres",
			fmt.Sprintf("DROP DATABASE IF EXISTS %s;", quotePostgresIdentifier(databaseName)))
	})

	metadataURL, err := url.Parse(env.MetadataPGURL)
	require.NoError(t, err)
	metadataURL.Path = "/" + databaseName
	stores, err := store.New(ctx, metadataURL.String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = stores.Close() })
	require.NoError(t, migrator.MigrateSchema(ctx, stores.GetDB()))
	return stores
}

// TestQualitySuggestionJobRunnerCompletesAgainstTheRealStore drives the complete
// success path of the job runner against a real *store.Store: Run claims the
// PENDING job, the fake drafting pipeline writes two real suggestion rows, and the
// per-target progress plus provider facts are persisted before the job reaches
// COMPLETED.
func TestQualitySuggestionJobRunnerCompletesAgainstTheRealStore(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stores := newIsolatedMetadataStore(ctx, t)

	targetGUIDs := []string{
		"instances/integration-instance;shop;public;orders",
		"instances/integration-instance;shop;public;customers",
	}
	targets := &storepb.QualitySuggestionJobTargets{}
	for _, guid := range targetGUIDs {
		targets.Targets = append(targets.Targets, &storepb.QualitySuggestionJobTarget{
			TargetGuid: guid,
			TargetType: int32(storepb.MetaType_TABLE),
			Columns:    []string{"email"},
			State:      storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_PENDING,
		})
	}
	resourceID, err := common.RandomString(12)
	require.NoError(t, err)
	_, err = stores.CreateQualitySuggestionJob(ctx, &store.QualitySuggestionJobMessage{
		ResourceID: resourceID,
		Status:     storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_PENDING,
		Intent:     "email must not be empty",
		Language:   "en",
		Targets:    targets,
	})
	require.NoError(t, err)

	runner := qualitysuggestionrunner.NewRunner(stores, &realStoreDraftSuggester{store: stores})
	runCtx, stopRunner := context.WithCancel(ctx)
	runnerDone := make(chan struct{})
	go func() {
		defer close(runnerDone)
		runner.Run(runCtx)
	}()
	t.Cleanup(func() {
		stopRunner()
		select {
		case <-runnerDone:
		case <-time.After(10 * time.Second):
			t.Error("the suggestion job runner did not stop")
		}
	})

	var finished *store.QualitySuggestionJobMessage
	require.Eventually(t, func() bool {
		job, err := stores.GetQualitySuggestionJob(ctx, &store.FindQualitySuggestionJobMessage{ResourceID: &resourceID})
		if err != nil || job == nil {
			return false
		}
		if job.Status != storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_COMPLETED &&
			job.Status != storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_FAILED &&
			job.Status != storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_CANCELED {
			return false
		}
		finished = job
		return true
	}, 15*time.Second, 50*time.Millisecond, "the suggestion job never reached a terminal status")
	require.Equalf(t, storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_COMPLETED, finished.Status,
		"the runner recorded %q", finished.Error)
	require.Empty(t, finished.Error)
	require.NotNil(t, finished.StartedAt)
	require.NotNil(t, finished.FinishedAt)
	require.Equal(t, "profiles/integration", finished.Provider)
	require.Equal(t, "integration-model", finished.Model)
	require.Equal(t, dataquality.RuleDraftPromptVersion, finished.PromptVersion)

	// The persisted progress reports both targets, in submission order, as DONE
	// with the suggestion each one queued and its start/finish stamps.
	require.Len(t, finished.Targets.GetTargets(), 2)
	var suggestionTargets []string
	for index, target := range finished.Targets.GetTargets() {
		require.Equal(t, targetGUIDs[index], target.GetTargetGuid(), "targets keep their submission order")
		require.Equal(t, storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_DONE, target.GetState())
		require.Empty(t, target.GetError())
		require.Greater(t, target.GetStartedAtUnixMs(), int64(0))
		require.GreaterOrEqual(t, target.GetFinishedAtUnixMs(), target.GetStartedAtUnixMs())
		require.Len(t, target.GetSuggestionResourceIds(), 1)
	}
	// Every queued suggestion is a real pending review-queue row, in target order.
	for _, target := range finished.Targets.GetTargets() {
		for _, suggestionID := range target.GetSuggestionResourceIds() {
			suggestion, err := stores.GetQualitySuggestion(ctx, &store.FindQualitySuggestionMessage{ResourceID: &suggestionID})
			require.NoError(t, err)
			require.NotNil(t, suggestion)
			require.Equal(t, storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING, suggestion.Status)
			require.Equal(t, finished.Provider, suggestion.Provider)
			require.Equal(t, finished.Model, suggestion.Model)
			suggestionTargets = append(suggestionTargets, suggestion.TargetGUID)
		}
	}
	require.Equal(t, targetGUIDs, suggestionTargets)

	// suggestion_count is the sum over the persisted target snapshots.
	count := 0
	for _, target := range finished.Targets.GetTargets() {
		count += len(target.GetSuggestionResourceIds())
	}
	require.Equal(t, 2, count)
}
