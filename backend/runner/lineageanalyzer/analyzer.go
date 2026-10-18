// Package lineageanalyzer is a runner that analyzes SQL definitions to produce column-level lineage.
package lineageanalyzer

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/pkg/errors"
	"github.com/sourcegraph/conc/pool"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/dmltarget"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/sqlsanitize"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	lineageAnalysisInterval = 1 * time.Hour
	analyzeCheckerInterval  = 10 * time.Second
	// MaxGoroutines is the max number of concurrent analysis jobs.
	MaxGoroutines = 10
)

// analyzeKey uniquely identifies an object to analyze.
type analyzeKey struct {
	MetaGUID string
	MetaType storepb.MetaType
}

// Analyzer is the column-level lineage analysis runner.
type Analyzer struct {
	store *store.Store
	// lineage resolves a statement against the analyzers the process assembled.
	// The runner does not build or own them: which engines exist is a startup
	// decision, and a runner that looked them up from package state could
	// disagree with the AnalyzeSQL handler about what is registered.
	lineage    *lineage.Analyzer
	analyzeMap sync.Map // map[analyzeKey]struct{}
	// retryMap tracks how many times a failed analysis was retried and when it
	// is due again, so a transient failure does not wait for the hourly scan.
	retryMap sync.Map // map[analyzeKey]analysisRetry
}

// analysisRetry is the backoff state of a failed analysis.
type analysisRetry struct {
	attempts int
	nextAt   time.Time
}

// maxAnalysisRetries bounds the retries before the object waits for the next
// full scan.
const maxAnalysisRetries = 3

// analysisRetryBackoff returns the delay before the next attempt.
func analysisRetryBackoff(attempts int) time.Duration {
	switch attempts {
	case 1:
		return 30 * time.Second
	case 2:
		return 2 * time.Minute
	default:
		return 5 * time.Minute
	}
}

// NewAnalyzer creates a new lineage Analyzer that resolves statements through
// lineage, the process's assembled set of engine analyzers.
func NewAnalyzer(stores *store.Store, lineageAnalyzer *lineage.Analyzer) *Analyzer {
	return &Analyzer{
		store:   stores,
		lineage: lineageAnalyzer,
	}
}

// QueueAnalysis enqueues an object for lineage analysis.
func (a *Analyzer) QueueAnalysis(metaGUID string, metaType storepb.MetaType) {
	key := analyzeKey{MetaGUID: metaGUID, MetaType: metaType}
	// A fresh queue request supersedes any pending backoff.
	a.retryMap.Delete(key)
	a.analyzeMap.Store(key, struct{}{})
}

// scheduleRetry re-queues a failed analysis with a bounded backoff. After the
// last attempt the object is left to the next full scan.
func (a *Analyzer) scheduleRetry(key analyzeKey) {
	attempts := 1
	if v, ok := a.retryMap.Load(key); ok {
		if entry, ok := v.(analysisRetry); ok {
			attempts = entry.attempts + 1
		}
	}
	if attempts > maxAnalysisRetries {
		// Drop the key entirely: leaving it queued would restart the backoff on
		// the very next tick, which is the hourly retry loop this replaces.
		a.retryMap.Delete(key)
		a.analyzeMap.Delete(key)
		slog.Error("Lineage analysis gave up after repeated failures",
			slog.String("guid", key.MetaGUID),
			slog.String("type", key.MetaType.String()))
		return
	}
	a.retryMap.Store(key, analysisRetry{attempts: attempts, nextAt: time.Now().Add(analysisRetryBackoff(attempts))})
	a.analyzeMap.Store(key, struct{}{})
}

// Name identifies the runner in the server's lifecycle logs.
func (*Analyzer) Name() string { return "lineageanalyzer" }

// Run starts the analyzer. It blocks until ctx is cancelled.
func (a *Analyzer) Run(ctx context.Context) {
	sp := pool.New()

	// Goroutine 1: periodic full scan.
	sp.Go(func() {
		slog.Debug(fmt.Sprintf("Lineage analyzer started and will run every %v", lineageAnalysisInterval))
		a.queueAll(ctx)
		ticker := time.NewTicker(lineageAnalysisInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				a.queueAll(ctx)
			case <-ctx.Done():
				return
			}
		}
	})

	// Goroutine 2: queue consumer.
	sp.Go(func() {
		ticker := time.NewTicker(analyzeCheckerInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				a.drainAndAnalyze(ctx)
			case <-ctx.Done():
				return
			}
		}
	})

	sp.Wait()
}

// queueAll scans all lineage-analyzable objects and queues those whose metahash
// or analyzer version differs from the stored analysis state. Each object type
// costs two queries (objects and their analysis state) instead of one query per
// object.
func (a *Analyzer) queueAll(ctx context.Context) {
	viewType := storepb.MetaType_VIEW
	mvType := storepb.MetaType_MATERIALIZED_VIEW
	manualSQLType := storepb.MetaType_MANUAL_SQL
	applicationSQLType := storepb.MetaType_APPLICATION_SQL

	for _, objType := range []storepb.MetaType{viewType, mvType, manualSQLType, applicationSQLType} {
		t := objType
		list, err := a.store.ListMetaRegistryResourceDigest(ctx, &store.FindMetaRegistryResourceMessage{
			ObjectType: &t,
		})
		if err != nil {
			slog.Error("Lineage analyzer failed to list meta registry", slog.String("type", t.String()), log.WithError(err))
			continue
		}
		versions, err := a.store.ListColumnLineageVersions(ctx, t)
		if err != nil {
			slog.Error("Lineage analyzer failed to list analysis versions", slog.String("type", t.String()), log.WithError(err))
			continue
		}
		for _, res := range list {
			if needsAnalysis(res, versions[res.GUID]) {
				a.QueueAnalysis(res.GUID, res.ObjectType)
			}
		}
	}
}

// needsAnalysis reports whether an object must be (re)analyzed: it has no stored
// result, its metadata hash changed, or the stored result came from different
// analyzer rules. The last condition is what makes an analyzer upgrade or a
// rollback refresh the lineage by itself.
func needsAnalysis(res *store.MetaRegistryResource, version *store.ColumnLineageVersion) bool {
	if version == nil {
		return true
	}
	if !bytes.Equal(version.MetaHash, res.MetaHash) {
		return true
	}
	return version.AnalyzerVersion != lineage.AnalyzerVersion
}

// drainAndAnalyze drains the analyzeMap and runs analysis for each object.
func (a *Analyzer) drainAndAnalyze(ctx context.Context) {
	now := time.Now()
	var keys []analyzeKey
	a.analyzeMap.Range(func(k, _ any) bool {
		key, ok := k.(analyzeKey)
		if !ok {
			return true
		}
		// A failed object stays queued until its backoff elapses.
		if v, ok := a.retryMap.Load(key); ok {
			if entry, ok := v.(analysisRetry); ok && now.Before(entry.nextAt) {
				return true
			}
		}
		keys = append(keys, key)
		a.analyzeMap.Delete(k)
		return true
	})
	if len(keys) == 0 {
		return
	}

	wp := pool.New().WithMaxGoroutines(MaxGoroutines)
	for _, key := range keys {
		k := key
		wp.Go(func() {
			// A panic in one object must not take the process down or stop the
			// other objects in this batch.
			defer func() {
				if r := recover(); r != nil {
					slog.Error("Lineage analysis panicked",
						slog.String("guid", k.MetaGUID),
						slog.String("type", k.MetaType.String()),
						slog.Any("panic", r))
					a.scheduleRetry(k)
				}
			}()
			if err := a.analyzeObject(ctx, k.MetaGUID, k.MetaType); err != nil {
				if errors.Is(err, errAnalysisStale) {
					// The text changed under the analysis. Queue the new text now
					// instead of waiting for the hourly scan; the write was dropped,
					// so nothing stale was persisted.
					a.QueueAnalysis(k.MetaGUID, k.MetaType)
					return
				}
				slog.Error("Lineage analysis failed",
					slog.String("guid", k.MetaGUID),
					slog.String("type", k.MetaType.String()),
					log.WithError(err))
				a.scheduleRetry(k)
				return
			}
			a.retryMap.Delete(k)
		})
	}
	wp.Wait()
}

// analyzeObject runs lineage analysis for a single lineage-analyzable object.
func (a *Analyzer) analyzeObject(ctx context.Context, metaGUID string, metaType storepb.MetaType) error {
	// Extract context from GUID: instanceID;database;schema;name
	parts := common.SplitMetaGUID(metaGUID)
	if len(parts) != 4 {
		return storeError(ctx, a.store, metaGUID, metaType, nil,
			fmt.Sprintf("invalid GUID format %q", metaGUID))
	}
	instanceID, database, schema, name := parts[0], parts[1], parts[2], parts[3]

	// Look up instance to get engine type.
	instance, err := a.store.GetInstance(ctx, &store.FindInstanceMessage{ResourceID: &instanceID})
	if err != nil {
		return storeError(ctx, a.store, metaGUID, metaType, err, "failed to get instance")
	}
	if instance == nil {
		return storeError(ctx, a.store, metaGUID, metaType, nil,
			fmt.Sprintf("instance %q not found", instanceID))
	}
	engine := instance.Metadata.GetEngine()

	// Load the stored metadata.
	res, err := a.store.GetMetaRegistry(ctx, &store.FindMetaRegistryResourceMessage{GUID: &metaGUID})
	if err != nil {
		return storeError(ctx, a.store, metaGUID, metaType, err, "failed to get meta registry")
	}
	if res == nil {
		return storeError(ctx, a.store, metaGUID, metaType, nil,
			fmt.Sprintf("meta registry entry not found for GUID %q", metaGUID))
	}

	// Get SQL definition and build the full CREATE VIEW statement.
	definition, wrappedSQL, err := buildSQL(name, engine, metaType, res)
	if err != nil {
		return storeError(ctx, a.store, metaGUID, metaType, err, "failed to build SQL")
	}
	if definition == "" {
		slog.Debug("Lineage analyzer skipping object with empty definition", slog.String("guid", metaGUID))
		return markAnalyzed(ctx, a.store, metaGUID, metaType, res.MetaHash, "")
	}

	// Run lineage analysis with context so unqualified names resolve correctly.
	ac := catalog.AnalysisContext{InstanceID: instanceID, Database: database, Schema: schema}
	analysisCtx := catalog.WithAnalysisContext(ctx, ac)
	relations, err := a.lineage.Analyze(analysisCtx, engine, wrappedSQL)

	// What an analysis could not represent does not condemn the rest of it: the
	// analyzers return the edges they did find together with the gap, so the
	// lineage is stored and the gap is recorded beside it.
	var unsupported *lineage.UnsupportedStatementError
	switch {
	case err == nil:
	case errors.As(err, &unsupported):
	case errors.Is(err, lineage.ErrorEngineNotSupported):
		// No analyzer is registered for this engine. Record the skip together
		// with the current hash so the hourly scan stops re-queueing the
		// object, and keep the reason visible in error_message.
		slog.Warn("Lineage analysis skipped: the engine has no lineage analyzer",
			slog.String("guid", metaGUID), slog.String("engine", engine.String()))
		return markAnalyzed(ctx, a.store, metaGUID, metaType, res.MetaHash,
			fmt.Sprintf("engine %s has no lineage analyzer; analysis skipped", engine))
	default:
		// A parse error is deterministic — the text does not change between
		// retries — so the object is recorded as analyzed at the current hash
		// instead of being re-queued forever by the hourly scan. The analyzers do
		// no other failing I/O: a catalog lookup that fails degrades one relation's
		// columns to a wildcard and is reported as a gap on the version row, which
		// is what keeps a degraded analysis distinguishable from a complete one.
		slog.Warn("Lineage analysis failed on an unsupported statement",
			slog.String("guid", metaGUID), slog.String("engine", engine.String()), log.WithError(err))
		return markAnalysisFailed(ctx, a.store, metaGUID, metaType, res.MetaHash, err)
	}

	// What a partial analysis could not model is stored on the version row
	// alongside the lineage written below.
	partialMessage := ""
	if unsupported != nil {
		partialMessage = unsupported.Error()
	}

	// Convert relations to ColumnLineage rows and collect GUIDs whose meta types
	// should be resolved from the registry.
	guidTypeMap := map[string]storepb.MetaType{metaGUID: metaType}
	guidLookupSet := make(map[string]struct{})
	var lineages []*store.ColumnLineage
	for _, rel := range relations {
		// A statement only names an object down to database/schema level, so the
		// rest comes from the analysis context. AnalyzeSQL resolves its scopes
		// through the same helper.
		sourceID := ac.Complete(rel.Source.Table)
		targetID := ac.Complete(rel.Target.Table)

		if rel.Transformation == nil {
			rel.Transformation = make([]model.Transformation, 0)
		}
		srcGUID := sourceID.GUID()

		if metaType == storepb.MetaType_MANUAL_SQL || metaType == storepb.MetaType_APPLICATION_SQL {
			guidLookupSet[srcGUID] = struct{}{}
			lineages = append(lineages, &store.ColumnLineage{
				MetaGUID:       metaGUID,
				MetaType:       metaType,
				SourceGUID:     srcGUID,
				SourceColumn:   rel.Source.Name,
				TargetGUID:     metaGUID,
				TargetColumn:   rel.Target.Name,
				TargetType:     metaType,
				RelationType:   rel.RelationType,
				Transformation: rel.Transformation,
			})

			if !rel.IsTemp {
				targetGUID := targetID.GUID()
				guidLookupSet[targetGUID] = struct{}{}
				lineages = append(lineages, &store.ColumnLineage{
					MetaGUID:       metaGUID,
					MetaType:       metaType,
					SourceGUID:     metaGUID,
					SourceColumn:   rel.Target.Name,
					SourceType:     metaType,
					TargetGUID:     targetGUID,
					TargetColumn:   rel.Target.Name,
					RelationType:   rel.RelationType,
					Transformation: rel.Transformation,
				})
			}
			continue
		}

		if rel.IsTemp {
			continue
		}

		targetGUID := targetID.GUID()
		guidLookupSet[srcGUID] = struct{}{}
		lineages = append(lineages, &store.ColumnLineage{
			MetaGUID:       metaGUID,
			MetaType:       metaType,
			SourceGUID:     srcGUID,
			SourceColumn:   rel.Source.Name,
			TargetGUID:     targetGUID,
			TargetColumn:   rel.Target.Name,
			TargetType:     metaType,
			RelationType:   rel.RelationType,
			Transformation: rel.Transformation,
		})
	}

	// A reported statement is classified here and nowhere else: ingestion stores the
	// statement without deciding, and this is the one place the read/write rule lives.
	if metaType == storepb.MetaType_APPLICATION_SQL {
		kind, targetEdges := a.classifyApplicationSQL(ctx, ac, engine, definition, relations, metaGUID)
		lineages = append(lineages, targetEdges...)
		// The edge names its target by GUID alone, so the lookup below is what
		// gives the target its type. Without it the relation reaches the API with
		// an unspecified target type, and every client that routes by type — the
		// metadata browser's lineage table, for one — loses the link.
		for _, edge := range targetEdges {
			guidLookupSet[edge.TargetGUID] = struct{}{}
		}
		if err := a.store.SetApplicationSQLKind(ctx, metaGUID, kind); err != nil {
			// The kind is a filter cache for the list page; the edges are the point, and a
			// failure to write the cache must not discard them.
			slog.Warn("Lineage analyzer failed to record the statement kind",
				slog.String("guid", metaGUID), log.WithError(err))
		}
	}

	for guid := range guidLookupSet {
		if guid == metaGUID {
			continue
		}
		g := guid
		srcMeta, err := a.store.GetMetaRegistry(ctx, &store.FindMetaRegistryResourceMessage{GUID: &g})
		if err != nil {
			slog.Warn("Lineage analyzer failed to look up source meta type", slog.String("guid", g), log.WithError(err))
			continue
		}
		if srcMeta != nil {
			guidTypeMap[g] = srcMeta.ObjectType
		}
	}
	for _, l := range lineages {
		if t, ok := guidTypeMap[l.SourceGUID]; ok {
			l.SourceType = t
		}
		if t, ok := guidTypeMap[l.TargetGUID]; ok {
			l.TargetType = t
		}
	}

	if len(lineages) == 0 {
		slog.Debug("Lineage analyzer found no lineage relations (after filtering temp targets)", slog.String("guid", metaGUID))
	}

	// Persist the result only if the object still holds the text this analysis
	// read. The edges, the version row and any gap message commit together, so a
	// rolled-back write cannot leave the object marked as analyzed, and a result
	// computed for a deleted object is dropped instead of re-creating its edges.
	var partialMessagePtr *string
	if partialMessage != "" {
		partialMessagePtr = &partialMessage
	}
	if err := a.store.ReplaceColumnLineageIfCurrent(ctx, metaGUID, metaType, res.MetaHash, lineage.AnalyzerVersion, lineages, partialMessagePtr); err != nil {
		return analysisWriteError(ctx, a.store, metaGUID, metaType, err, "failed to replace column lineage")
	}
	return nil
}

// buildSQL extracts the object definition and wraps it as needed for lineage parsing.
func buildSQL(name string, engine storepb.Engine, metaType storepb.MetaType, res *store.MetaRegistryResource) (definition string, wrapped string, err error) {
	switch metaType {
	case storepb.MetaType_VIEW:
		definition = res.Metadata.GetViewMetadata().GetDefinition()
	case storepb.MetaType_MATERIALIZED_VIEW:
		definition = res.Metadata.GetMaterializedViewMetadata().GetDefinition()
	case storepb.MetaType_MANUAL_SQL:
		definition = res.Metadata.GetManualSqlMetadata().GetSqlText()
	case storepb.MetaType_APPLICATION_SQL:
		definition = res.Metadata.GetApplicationSqlMetadata().GetSqlText()
	default:
		return "", "", errors.Errorf("unsupported meta type %v", metaType)
	}
	if definition == "" {
		return "", "", nil
	}
	if metaType == storepb.MetaType_MANUAL_SQL {
		return definition, definition, nil
	}
	if metaType == storepb.MetaType_APPLICATION_SQL {
		// The stored text is the parameterized one, and the MySQL family's grammar has no
		// production that accepts a placeholder, so the analyzer is handed the substituted
		// form. The stored text stays the definition: it is what the object is.
		return definition, sqlsanitize.Analyzable(engine, definition), nil
	}
	// A stored definition that is already a complete statement is used as-is.
	// StarRocks keeps the whole SHOW CREATE MATERIALIZED VIEW output in
	// MaterializedViewMetadata.Definition, so wrapping it in
	// "CREATE MATERIALIZED VIEW <name> AS ..." would produce
	// "... AS CREATE MATERIALIZED VIEW ...", which no parser accepts.
	if isCompleteStatement(definition) {
		return definition, definition, nil
	}

	keyword := "CREATE VIEW"
	if metaType == storepb.MetaType_MATERIALIZED_VIEW {
		keyword = "CREATE MATERIALIZED VIEW"
	}

	var identifier string
	switch engine {
	// The MySQL family, including StarRocks and Doris, quotes identifiers with
	// backticks. MariaDB shares the MySQL driver and its views keep a bare query
	// body, so it needs the same quoting as MySQL.
	case storepb.Engine_MYSQL, storepb.Engine_TIDB, storepb.Engine_MARIADB, storepb.Engine_DORIS, storepb.Engine_STARROCKS:
		identifier = "`" + strings.ReplaceAll(name, "`", "``") + "`"
	case storepb.Engine_POSTGRES:
		identifier = `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
	case storepb.Engine_MSSQL:
		identifier = "[" + strings.ReplaceAll(name, "]", "]]") + "]"
	default:
		// Engines this build has no analyzer for keep the object name as written:
		// they are skipped before the wrapper's identifier could matter, and the
		// fallback keeps that skip visible instead of turning it into a build error.
		identifier = name
	}

	return definition, fmt.Sprintf("%s %s AS %s", keyword, identifier, definition), nil
}

// isCompleteStatement reports whether a stored definition is already a whole
// CREATE statement rather than a bare query body. Leading whitespace and
// comments are skipped first: a complete DDL commonly opens with a `--` or
// `/* */` note, and wrapping such a statement in "CREATE VIEW ... AS" makes it
// unparseable. A bare query body can never begin with CREATE, so the check is
// safe for every engine.
func isCompleteStatement(definition string) bool {
	rest := strings.TrimSpace(definition)
	for {
		switch {
		case strings.HasPrefix(rest, "--"):
			lineEnd := strings.IndexByte(rest, '\n')
			if lineEnd < 0 {
				return false
			}
			rest = strings.TrimSpace(rest[lineEnd+1:])
		case strings.HasPrefix(rest, "/*"):
			commentEnd := strings.Index(rest[2:], "*/")
			if commentEnd < 0 {
				return false
			}
			rest = strings.TrimSpace(rest[2+commentEnd+2:])
		default:
			return hasCreateKeyword(rest)
		}
	}
}

// hasCreateKeyword reports whether s starts with the CREATE keyword followed by
// a separator. It accepts any whitespace between CREATE and what follows, so
// "CREATE\nVIEW" counts as a complete statement.
func hasCreateKeyword(s string) bool {
	const keyword = "CREATE"
	if len(s) < len(keyword) || !strings.EqualFold(s[:len(keyword)], keyword) {
		return false
	}
	rest := s[len(keyword):]
	return len(rest) > 0 && unicode.IsSpace(rune(rest[0]))
}

// errAnalysisStale tells drainAndAnalyze that the result was computed from text
// that has already been replaced, so the object is queued again instead of
// being retried as a failure.
var errAnalysisStale = errors.New("analysis result is stale")

// storeError surfaces an error before any result exists. It records the message
// on the object's existing analysis-state row without touching its hash: a
// transient failure must not make a successful result look unanalyzed, and the
// next attempt still compares against the last stored result.
func storeError(ctx context.Context, s *store.Store, metaGUID string, metaType storepb.MetaType, cause error, msg string) error {
	var full string
	if cause != nil {
		full = fmt.Sprintf("%s: %v", msg, cause)
	} else {
		full = msg
	}
	_ = s.RecordAnalysisError(ctx, metaGUID, metaType, full)
	if cause != nil {
		return errors.Wrap(cause, msg)
	}
	return errors.New(msg)
}

// analysisWriteError classifies a failed result write: a vanished object drops the
// result, a changed one asks for a re-analysis of the new text, and anything else
// is a transient store error that the retry map handles.
func analysisWriteError(ctx context.Context, s *store.Store, metaGUID string, metaType storepb.MetaType, cause error, msg string) error {
	switch {
	case errors.Is(cause, store.ErrAnalysisGone):
		slog.Debug("Lineage analysis dropped: the object disappeared during analysis",
			slog.String("guid", metaGUID), slog.String("type", metaType.String()))
		return nil
	case errors.Is(cause, store.ErrAnalysisStale):
		return errAnalysisStale
	default:
		return storeError(ctx, s, metaGUID, metaType, cause, msg)
	}
}

// markAnalyzed records a successful analysis (or a deliberate skip) with the
// current metahash and the analyzer version. A non-empty errorMessage documents
// why analysis was skipped while still recording the hash, so the object is not
// retried until its metadata changes or the analyzer rules do.
func markAnalyzed(ctx context.Context, s *store.Store, metaGUID string, metaType storepb.MetaType, metaHash []byte, errorMessage string) error {
	var message *string
	if errorMessage != "" {
		message = &errorMessage
	}
	if err := s.MarkAnalyzedIfCurrent(ctx, metaGUID, metaType, metaHash, lineage.AnalyzerVersion, message); err != nil {
		return analysisWriteError(ctx, s, metaGUID, metaType, err, "failed to record analysis state")
	}
	return nil
}

// markAnalysisFailed records SQL the analyzer could not parse. The current hash is
// stored with the error message so the hourly scan stops re-queueing an unchanged
// definition (a metadata or analyzer change re-queues it), and any lineage a
// previous definition left behind is dropped because it describes SQL the object
// no longer has. Clearing the edges and recording the failure commit together. A
// gap the analysis reported instead of a failure takes the partial path above and
// keeps its edges.
func markAnalysisFailed(ctx context.Context, s *store.Store, metaGUID string, metaType storepb.MetaType, metaHash []byte, cause error) error {
	err := s.MarkAnalysisFailedIfCurrent(ctx, metaGUID, metaType, metaHash, lineage.AnalyzerVersion,
		fmt.Sprintf("lineage analysis failed: %v", cause))
	if err != nil {
		return analysisWriteError(ctx, s, metaGUID, metaType, err, "failed to record analysis failure")
	}
	return nil
}

// classifyApplicationSQL decides whether a reported statement is a read or a write, and
// returns the graph edge a write with no column relation still needs.
//
// The rule, and why it is not simply "the analysis produced a relation": the analyzers
// record column-level derivation, so a statement that reads a column produces a relation
// whose target is the statement, while a statement that writes values from nowhere — the
// ORM's `INSERT INTO t (a, b) VALUES (?, ?)`, or a `DELETE` that references no column —
// produces none at all in every dialect. Those are writes all the same, and without a target
// the graph would show nothing about them, so the target is read from the statement itself.
//
// The table-level edge carries no columns and no transformations, which is the shape the
// OpenLineage table-level path already stores. See
// .agents/docs/application-sql-ingestion.md, "Classification and the table-level edge".
func (a *Analyzer) classifyApplicationSQL(
	ctx context.Context,
	ac catalog.AnalysisContext,
	engine storepb.Engine,
	definition string,
	relations []model.ColumnRelation,
	metaGUID string,
) (store.ApplicationSQLKind, []*store.ColumnLineage) {
	for _, rel := range relations {
		if !rel.IsTemp {
			return store.ApplicationSQLKindWrite, nil
		}
	}

	targets, err := dmltarget.Targets(engine, sqlsanitize.Analyzable(engine, definition))
	if err != nil {
		// Not a write, or one this package cannot name. Either way it has no out-edge, and a
		// SELECT with no source column is not drawn at all — manual SQL has the same hole.
		return store.ApplicationSQLKindRead, nil
	}

	var edges []*store.ColumnLineage
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		// The analyzer produced no relation here, so the name is read from the statement and the
		// registry is asked for the object it belongs to: an engine that binds identifiers
		// case-insensitively may have written `orders` for the object the registry holds as
		// `Orders`, and an edge into a GUID the registry does not have points at nothing.
		id := a.lineage.CanonicalIdentifier(ctx, engine, ac.Complete(target))
		guid := id.GUID()
		if guid == metaGUID {
			continue
		}
		if _, ok := seen[guid]; ok {
			continue
		}
		seen[guid] = struct{}{}
		edges = append(edges, &store.ColumnLineage{
			MetaGUID:     metaGUID,
			MetaType:     storepb.MetaType_APPLICATION_SQL,
			SourceGUID:   metaGUID,
			SourceType:   storepb.MetaType_APPLICATION_SQL,
			TargetGUID:   guid,
			TargetColumn: "",
			// The column is not null in the schema, so an empty list is required rather than a
			// nil one; RelationTypeOf reads it as `direct`.
			Transformation: make([]model.Transformation, 0),
			RelationType:   model.RelationTypeOf(nil),
		})
	}
	if len(edges) == 0 {
		return store.ApplicationSQLKindRead, nil
	}
	return store.ApplicationSQLKindWrite, edges
}
