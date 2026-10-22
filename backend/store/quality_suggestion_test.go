package store

import (
	"database/sql"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// qualitySuggestionScannerStub stands in for one quality_suggestion row.
type qualitySuggestionScannerStub struct {
	id            int64
	resourceID    string
	kind          int16
	source        int16
	targetGUID    string
	targetType    int16
	payload       []byte
	status        int16
	provider      string
	model         string
	promptVersion string
	createdBy     sql.NullInt32
	createdAt     time.Time
	decidedBy     sql.NullInt32
	decidedAt     sql.NullTime
	decisionNote  string
}

func (s qualitySuggestionScannerStub) Scan(dest ...any) error {
	*(dest[0].(*int64)) = s.id
	*(dest[1].(*string)) = s.resourceID
	*(dest[2].(*int16)) = s.kind
	*(dest[3].(*int16)) = s.source
	*(dest[4].(*string)) = s.targetGUID
	*(dest[5].(*int16)) = s.targetType
	*(dest[6].(*[]byte)) = s.payload
	*(dest[7].(*int16)) = s.status
	*(dest[8].(*string)) = s.provider
	*(dest[9].(*string)) = s.model
	*(dest[10].(*string)) = s.promptVersion
	*(dest[11].(*sql.NullInt32)) = s.createdBy
	*(dest[12].(*time.Time)) = s.createdAt
	*(dest[13].(*sql.NullInt32)) = s.decidedBy
	*(dest[14].(*sql.NullTime)) = s.decidedAt
	*(dest[15].(*string)) = s.decisionNote
	return nil
}

// suggestionRulePayload builds a payload whose oneof arm is a rule draft, which
// is what a RULE_DRAFT suggestion must carry.
func suggestionRulePayload(t *testing.T) *storepb.QualitySuggestionPayload {
	t.Helper()
	return &storepb.QualitySuggestionPayload{
		Payload: &storepb.QualitySuggestionPayload_Rule{
			Rule: &storepb.QualityRuleDraftConfig{
				Title:      "Orders must have an id",
				Rationale:  "the id column is unique in the profile",
				Confidence: 0.81,
			},
		},
	}
}

func TestQualitySuggestionWhereBindsEveryFilterValue(t *testing.T) {
	t.Parallel()

	where, args, err := qualitySuggestionWhere(nil)
	require.NoError(t, err)
	require.Equal(t, "TRUE", where)
	require.Empty(t, args)

	resourceID := "suggestions/abc"
	targetGUID := "instance_1;db;public;orders"
	where, args, err = qualitySuggestionWhere(&FindQualitySuggestionMessage{
		ResourceID: &resourceID,
		Statuses: []storepb.QualitySuggestionStatus{
			storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING,
		},
		Kinds: []storepb.QualitySuggestionKind{
			storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT,
			storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_LABEL,
		},
		Sources: []storepb.QualitySuggestionSource{
			storepb.QualitySuggestionSource_QUALITY_SUGGESTION_SOURCE_PROFILE,
		},
		TargetGUID: &targetGUID,
	})
	require.NoError(t, err)
	require.Equal(t,
		`TRUE AND resource_id = $1 AND status = ANY($2::int[]) AND kind = ANY($3::int[]) AND source = ANY($4::int[]) AND (target_guid = $5 OR target_guid LIKE $6 ESCAPE E'\\')`,
		where)
	require.Len(t, args, 6)
	require.Equal(t, resourceID, args[0])
	require.Equal(t, pq.Array([]int32{int32(storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING)}), args[1])
	require.Equal(t, pq.Array([]int32{
		int32(storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT),
		int32(storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_LABEL),
	}), args[2])
	require.Equal(t, pq.Array([]int32{int32(storepb.QualitySuggestionSource_QUALITY_SUGGESTION_SOURCE_PROFILE)}), args[3])
	// The exact target and its subtree share the rules' match semantics; a
	// metacharacter in the GUID is escaped in the LIKE pattern.
	require.Equal(t, targetGUID, args[4])
	require.Equal(t, `instance\_1;db;public;orders;%`, args[5])

	for _, value := range []string{resourceID, targetGUID} {
		require.NotContains(t, where, value)
	}
}

// The review-queue path filters are the same GUID subtree prefixes the rule list
// uses: the deepest filter decides the segment count, an empty schema segment is
// valid (MySQL), and a bare name is refused because it is not a GUID prefix.
func TestQualitySuggestionWhereFiltersByGUIDPathPrefix(t *testing.T) {
	t.Parallel()

	where, args, err := qualitySuggestionWhere(&FindQualitySuggestionMessage{Instance: "instances/inst-1"})
	require.NoError(t, err)
	require.Contains(t, where, "target_guid")
	require.Equal(t, []any{"instances/inst-1", `instances/inst-1;%`}, args)
	requirePlaceholdersMatchArgs(t, where, args)

	where, args, err = qualitySuggestionWhere(&FindQualitySuggestionMessage{
		Instance: "instances/inst-1", Database: "shop", Schema: "public", TablePrefix: "users",
	})
	require.NoError(t, err)
	require.Equal(t, []any{
		"instances/inst-1;shop;public;users",
		`instances/inst-1;shop;public;users;%`,
	}, args)
	requirePlaceholdersMatchArgs(t, where, args)

	// MySQL metadata GUIDs have no schema segment; the table prefix still has
	// four segments.
	_, args, err = qualitySuggestionWhere(&FindQualitySuggestionMessage{
		Instance: "instances/inst-1", Database: "shop", TablePrefix: "users",
	})
	require.NoError(t, err)
	require.Equal(t, []any{"instances/inst-1;shop;;users", `instances/inst-1;shop;;users;%`}, args)

	for _, find := range []*FindQualitySuggestionMessage{
		{Database: "shop"},
		{Schema: "public"},
		{TablePrefix: "users"},
	} {
		_, _, err := qualitySuggestionWhere(find)
		require.Error(t, err)
		require.Equal(t, common.Invalid, common.ErrorCode(err))
	}
}

func TestQualitySuggestionQueryOrdersNewestAndPaginates(t *testing.T) {
	t.Parallel()

	limit, offset := 20, 40
	query, args, err := qualitySuggestionQuery(&FindQualitySuggestionMessage{Limit: &limit, Offset: &offset})
	require.NoError(t, err)
	require.Contains(t, query, "FROM quality_suggestion")
	require.Contains(t, query, "ORDER BY created_at DESC, id DESC")
	require.Contains(t, query, "LIMIT $1")
	require.Contains(t, query, "OFFSET $2")
	requirePlaceholdersMatchArgs(t, query, args)
	require.Equal(t, []any{20, 40}, args)

	// A deeper path filter without its ancestors is refused by the query builder,
	// not silently dropped.
	_, _, err = qualitySuggestionQuery(&FindQualitySuggestionMessage{TablePrefix: "users"})
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))
}

func TestScanQualitySuggestionRoundTripsThePayloadAndDecision(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.October, 4, 9, 0, 0, 0, time.UTC)
	decidedAt := createdAt.Add(2 * time.Hour)
	payloadJSON, err := protojson.Marshal(suggestionRulePayload(t))
	require.NoError(t, err)

	message, err := scanQualitySuggestion(qualitySuggestionScannerStub{
		id:            31,
		resourceID:    "suggestions/abc",
		kind:          int16(storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT),
		source:        int16(storepb.QualitySuggestionSource_QUALITY_SUGGESTION_SOURCE_PROFILE),
		targetGUID:    "instance_1;db;public;orders",
		targetType:    int16(storepb.MetaType_TABLE),
		payload:       payloadJSON,
		status:        int16(storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_ACCEPTED),
		provider:      "profiles/openai",
		model:         "gpt-4",
		promptVersion: "dq.rule_draft.v1",
		createdBy:     sql.NullInt32{Int32: 5, Valid: true},
		createdAt:     createdAt,
		decidedBy:     sql.NullInt32{Int32: 6, Valid: true},
		decidedAt:     sql.NullTime{Time: decidedAt, Valid: true},
		decisionNote:  "looks right",
	})
	require.NoError(t, err)
	require.Equal(t, int64(31), message.ID)
	require.Equal(t, "suggestions/abc", message.ResourceID)
	require.Equal(t, storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT, message.Kind)
	require.Equal(t, storepb.QualitySuggestionSource_QUALITY_SUGGESTION_SOURCE_PROFILE, message.Source)
	require.Equal(t, "instance_1;db;public;orders", message.TargetGUID)
	require.Equal(t, storepb.MetaType_TABLE, message.TargetType)
	require.Equal(t, storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_ACCEPTED, message.Status)
	require.Equal(t, "profiles/openai", message.Provider)
	require.Equal(t, "gpt-4", message.Model)
	require.Equal(t, "dq.rule_draft.v1", message.PromptVersion)
	require.NotNil(t, message.Payload)
	require.Equal(t, "Orders must have an id", message.Payload.GetRule().GetTitle())
	require.InDelta(t, 0.81, message.Payload.GetRule().GetConfidence(), 1e-9)
	require.NotNil(t, message.CreatedBy)
	require.Equal(t, int32(5), *message.CreatedBy)
	require.Equal(t, createdAt, message.CreatedAt)
	require.NotNil(t, message.DecidedBy)
	require.Equal(t, int32(6), *message.DecidedBy)
	require.NotNil(t, message.DecidedAt)
	require.Equal(t, decidedAt, *message.DecidedAt)
	require.Equal(t, "looks right", message.DecisionNote)
}

func TestScanQualitySuggestionLeavesPendingDecisionColumnsNil(t *testing.T) {
	t.Parallel()

	message, err := scanQualitySuggestion(qualitySuggestionScannerStub{
		kind:   int16(storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_LABEL),
		source: int16(storepb.QualitySuggestionSource_QUALITY_SUGGESTION_SOURCE_NATURAL_LANGUAGE),
		status: int16(storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING),
	})
	require.NoError(t, err)
	require.Nil(t, message.CreatedBy)
	require.Nil(t, message.DecidedBy)
	require.Nil(t, message.DecidedAt)
	require.NotNil(t, message.Payload)
}

func TestValidateQualitySuggestionMatchesKindToPayload(t *testing.T) {
	t.Parallel()

	base := &QualitySuggestionMessage{
		ResourceID: "suggestions/abc",
		Kind:       storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT,
		Source:     storepb.QualitySuggestionSource_QUALITY_SUGGESTION_SOURCE_PROFILE,
		TargetGUID: "g",
		TargetType: storepb.MetaType_TABLE,
		Payload:    suggestionRulePayload(t),
	}
	require.NoError(t, validateQualitySuggestion(base))
	require.Equal(t, common.Invalid, common.ErrorCode(validateQualitySuggestion(nil)))

	// A RULE_DRAFT suggestion whose payload is a label would render an empty
	// review card, so the mismatch is refused before it is stored.
	broken := *base
	broken.Payload = &storepb.QualitySuggestionPayload{
		Payload: &storepb.QualitySuggestionPayload_Label{
			Label: &storepb.SemanticLabelSuggestion{Column: "email"},
		},
	}
	require.Equal(t, common.Invalid, common.ErrorCode(validateQualitySuggestion(&broken)))

	// The mirror image: a LABEL suggestion carrying a rule draft.
	broken = *base
	broken.Kind = storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_LABEL
	require.Equal(t, common.Invalid, common.ErrorCode(validateQualitySuggestion(&broken)))

	labelPayload := &storepb.QualitySuggestionPayload{
		Payload: &storepb.QualitySuggestionPayload_Label{
			Label: &storepb.SemanticLabelSuggestion{
				Column: "email",
				Label:  storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_EMAIL,
			},
		},
	}
	broken = *base
	broken.Kind = storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_LABEL
	broken.Payload = labelPayload
	require.NoError(t, validateQualitySuggestion(&broken))

	broken = *base
	broken.Payload = nil
	require.Equal(t, common.Invalid, common.ErrorCode(validateQualitySuggestion(&broken)))

	broken = *base
	broken.ResourceID = ""
	require.Equal(t, common.Invalid, common.ErrorCode(validateQualitySuggestion(&broken)))

	broken = *base
	broken.Kind = storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_UNSPECIFIED
	require.Equal(t, common.Invalid, common.ErrorCode(validateQualitySuggestion(&broken)))

	broken = *base
	broken.Source = storepb.QualitySuggestionSource_QUALITY_SUGGESTION_SOURCE_UNSPECIFIED
	require.Equal(t, common.Invalid, common.ErrorCode(validateQualitySuggestion(&broken)))

	broken = *base
	broken.TargetGUID = ""
	require.Equal(t, common.Invalid, common.ErrorCode(validateQualitySuggestion(&broken)))

	broken = *base
	broken.TargetType = storepb.MetaType_UNSPECIFIED
	require.Equal(t, common.Invalid, common.ErrorCode(validateQualitySuggestion(&broken)))
}

// A suggestion exists to be reviewed, so it is created pending: accepting or
// rejecting in the same write would skip the decision fields and the audit.
func TestQualitySuggestionInitialStatusOnlyAcceptsPending(t *testing.T) {
	t.Parallel()

	status, err := qualitySuggestionInitialStatus(storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_UNSPECIFIED)
	require.NoError(t, err)
	require.Equal(t, storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING, status)

	status, err = qualitySuggestionInitialStatus(storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING)
	require.NoError(t, err)
	require.Equal(t, storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING, status)

	for _, decided := range []storepb.QualitySuggestionStatus{
		storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_ACCEPTED,
		storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_REJECTED,
	} {
		_, err = qualitySuggestionInitialStatus(decided)
		require.Error(t, err)
		require.Equal(t, common.Invalid, common.ErrorCode(err))
	}
}

// The review queue allows exactly one transition, and the store refuses
// everything else before it reaches the database.
func TestValidateSuggestionDecision(t *testing.T) {
	t.Parallel()

	require.NoError(t, validateSuggestionDecision("suggestions/abc",
		storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_ACCEPTED, 5))
	require.NoError(t, validateSuggestionDecision("suggestions/abc",
		storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_REJECTED, 5))

	for _, invalid := range []struct {
		name       string
		resourceID string
		status     storepb.QualitySuggestionStatus
		decidedBy  int32
	}{
		{"no resource", " ", storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_ACCEPTED, 5},
		{"still pending", "s", storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING, 5},
		{"unspecified status", "s", storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_UNSPECIFIED, 5},
		{"no decider", "s", storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_ACCEPTED, 0},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			t.Parallel()
			err := validateSuggestionDecision(invalid.resourceID, invalid.status, invalid.decidedBy)
			require.Error(t, err)
			require.Equal(t, common.Invalid, common.ErrorCode(err))
		})
	}
}

// A get must name exactly one suggestion; by-target or by-status queries are a
// list, and answering them with one row would silently drop the rest.
func TestValidateFindQualitySuggestionRequiresTheResource(t *testing.T) {
	t.Parallel()

	require.Equal(t, common.Invalid, common.ErrorCode(validateFindQualitySuggestion(nil)))
	empty := ""
	require.Equal(t, common.Invalid, common.ErrorCode(validateFindQualitySuggestion(&FindQualitySuggestionMessage{ResourceID: &empty})))
	target := "instance_1;db;public;orders"
	require.Equal(t, common.Invalid, common.ErrorCode(validateFindQualitySuggestion(&FindQualitySuggestionMessage{TargetGUID: &target})))
	resourceID := "suggestions/abc"
	require.NoError(t, validateFindQualitySuggestion(&FindQualitySuggestionMessage{ResourceID: &resourceID}))
}

// The decision is one conditional UPDATE: the `status = 1` predicate is what
// makes it exactly-once under concurrent reviewers, so the statement text is the
// guard and cannot be replaced by a read-then-write.
func TestDecideQualitySuggestionQueryGuardsThePendingState(t *testing.T) {
	t.Parallel()

	require.Contains(t, decideQualitySuggestionQuery, "UPDATE quality_suggestion")
	require.Contains(t, decideQualitySuggestionQuery, "WHERE resource_id = $1 AND status = 1")
	require.Contains(t, decideQualitySuggestionQuery, "RETURNING")
	require.Contains(t, decideQualitySuggestionQuery, "decision_note")
	requirePlaceholdersMatchArgs(t, decideQualitySuggestionQuery,
		[]any{"suggestions/abc", int32(2), int32(5), time.Now(), "note"})

	require.Contains(t, createQualitySuggestionQuery, "INSERT INTO quality_suggestion")
	require.Contains(t, createQualitySuggestionQuery, "RETURNING id, created_at")
	requirePlaceholdersMatchArgs(t, createQualitySuggestionQuery,
		[]any{"r", int32(1), int32(2), "g", int32(4), []byte("{}"), int32(1), "", "", "", nil, time.Now()})
}
