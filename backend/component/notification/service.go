// Package notification delivers in-app messages to users: the outcome of a sync
// operation somebody asked for, and the failures a workspace administrator needs
// to see. It is the one writer of the notification table, so the recipient
// resolution, the suppression of repeated background messages and the envelope
// they share live here rather than in each caller.
//
// Delivery is best effort by contract. A caller is on a sync, an ingestion or a
// cleanup path, and none of them may fail because a message could not be
// written: they log the error and carry on, the same way the audit interceptor
// treats its ledger writes.
//
// A written message is also handed to the recipient's live subscriptions (see
// Subscribe), which is how a signed-in client learns about it without polling.
// That channel is process-local and cannot fail a write: a subscription that has
// stopped reading is dropped rather than allowed to hold the writer open.
package notification

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/proto"

	"github.com/Ranxy/metaxisdata/backend/common"
	clog "github.com/Ranxy/metaxisdata/backend/common/log"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
	"github.com/Ranxy/metaxisdata/backend/utils"
)

const (
	// BackgroundFailureWindow suppresses a repeated failure report of the same
	// target. A failing instance is retried by the periodic scan every fifteen
	// minutes and a failing database backs off for a few, so without a window a
	// single broken target would report every few minutes forever.
	BackgroundFailureWindow = time.Hour
	// UnmappedNamespaceWindow is longer than the failure window on purpose: a
	// dataset that matched no instance is a configuration mistake rather than an
	// outage, and it repeats for every event the producer sends.
	UnmappedNamespaceWindow = 24 * time.Hour

	// gateLimit bounds the process-local suppression gate. Past it the gate stops
	// suppressing instead of growing without bound; correctness never depended on
	// it, only the cost of a repeated report does.
	gateLimit = 1 << 16
	// gateRetention is how long a gate entry is remembered. An entry older than
	// the longest window can never match a new key, because the key carries the
	// window bucket.
	gateRetention = 24 * time.Hour

	// MaxFailureEntries bounds how many databases one sync message lists.
	// The message's counts carry the rest, so it stays a pointer into the estate
	// rather than a copy of it.
	MaxFailureEntries = 100
	// MaxErrorBytes bounds one recorded error. A driver error can quote a whole
	// statement, and the row is kept forever, so it is trimmed at the door.
	MaxErrorBytes = 2 << 10

	// writeTimeout bounds a notification write. It runs detached from the request
	// that triggered it — see Send — so the bound has to come from here, the way
	// audit.AuditWriteTimeout bounds the ledger's.
	writeTimeout = 10 * time.Second
)

// AdminStore is the subset of *store.Store this package reads. Narrowing it
// keeps the recipient resolution testable without a database and documents
// exactly what delivering a message costs.
type AdminStore interface {
	CreateNotification(ctx context.Context, notification *storepb.Notification) (*storepb.Notification, error)
	CountNotificationCounts(ctx context.Context, recipientID int) (unread int, unseen int, err error)
	GetWorkspaceID(ctx context.Context) (string, error)
	GetWorkspaceIamPolicy(ctx context.Context) (*store.IamPolicyMessage, error)
	GetGroup(ctx context.Context, email string) (*store.GroupMessage, error)
	GetUserByID(ctx context.Context, id int) (*store.UserMessage, error)
}

// Service writes notifications and works out who receives them. It is also where
// the live push channel starts: a message it writes is handed to the subscriptions
// of its recipient, so a signed-in client sees it without asking.
type Service struct {
	store AdminStore
	hub   *hub

	// mu guards the suppression gate below. publishMu serializes "read the count, then
	// hand the message over" — see publish.
	mu        sync.Mutex
	publishMu sync.Mutex
	recent    map[string]time.Time
}

// New returns a notification service backed by the store.
func New(stores *store.Store) *Service {
	return newServiceWithStore(stores)
}

func newServiceWithStore(stores AdminStore) *Service {
	return &Service{
		store:  stores,
		hub:    newHub(),
		recent: make(map[string]time.Time),
	}
}

// Subscribe registers a live subscription for one recipient's inbox. The returned
// channel is closed when the subscription ends — the server is shutting down, the
// connection could not keep up, or the caller unsubscribed — and the returned
// function releases it. Releasing is safe to call more than once.
func (s *Service) Subscribe(recipientID int) (<-chan Event, func(), error) {
	sub, err := s.hub.subscribe(recipientID)
	if err != nil {
		return nil, nil, err
	}
	return sub.ch, func() { s.hub.unsubscribe(recipientID, sub) }, nil
}

// Close ends every live subscription. It is what lets the server drain: the HTTP
// server's shutdown waits for in-flight requests, and a notification stream is one
// that never ends on its own.
func (s *Service) Close() {
	s.hub.close()
}

// SchemaSyncMessage builds the message one sync operation reports its outcome
// with. recipientID is 0 when the message is addressed to the workspace
// administrators rather than to the user who asked for the sync.
func SchemaSyncMessage(recipientID int, severity storepb.NotificationSeverity, detail *storepb.SchemaSyncDetail) *storepb.Notification {
	return &storepb.Notification{
		RecipientId: int32(recipientID),
		Type:        storepb.NotificationType_NOTIFICATION_TYPE_SCHEMA_SYNC,
		Severity:    severity,
		Detail:      &storepb.Notification_SchemaSync{SchemaSync: boundSyncDetail(detail)},
	}
}

// OpenLineageMessage builds the message an OpenLineage ingestion failure is
// reported with. It is always addressed to the workspace administrators: an
// ingestion key is a machine credential, so no user asked for the request.
func OpenLineageMessage(severity storepb.NotificationSeverity, detail *storepb.OpenLineageDetail) *storepb.Notification {
	return &storepb.Notification{
		Type:     storepb.NotificationType_NOTIFICATION_TYPE_OPENLINEAGE,
		Severity: severity,
		Detail:   &storepb.Notification_Openlineage{Openlineage: boundOpenLineageDetail(detail)},
	}
}

// ReportUnmatchedNamespace tells the workspace administrators that an ingested
// dataset matched no registered instance and was stored as an external dataset. It
// is the OpenLineage resolver's reporter: the resolver decides when a namespace is
// worth reporting, this decides how.
func (s *Service) ReportUnmatchedNamespace(ctx context.Context, namespace, dataset string) {
	detail := &storepb.OpenLineageDetail{
		Kind:      storepb.OpenLineageFailureKind_OPENLINEAGE_FAILURE_KIND_NAMESPACE_UNMAPPED,
		Namespace: namespace,
		Dataset:   dataset,
	}
	message := OpenLineageMessage(storepb.NotificationSeverity_NOTIFICATION_SEVERITY_WARNING, detail)
	// A namespace nobody mapped keeps producing this for every event it carries, so
	// the window is a day rather than an hour: it is a configuration gap to fix, not
	// an outage to be paged about.
	message.DedupeKey = DedupeKey("openlineage.namespace-unmapped", namespace, time.Now(), UnmappedNamespaceWindow)
	if err := s.SendToWorkspaceAdmins(ctx, message); err != nil {
		LogFailure(err, slog.String("namespace", namespace))
	}
}

// boundSyncDetail copies the detail with its payload bounded: at most
// MaxFailureEntries failures, each error trimmed to MaxErrorBytes. The counts are
// left alone, so a message still reports how many databases failed even when it
// lists only the first hundred.
func boundSyncDetail(detail *storepb.SchemaSyncDetail) *storepb.SchemaSyncDetail {
	if detail == nil {
		return nil
	}
	bounded, ok := proto.Clone(detail).(*storepb.SchemaSyncDetail)
	if !ok {
		return detail
	}
	databases := bounded.GetDatabases()
	if len(databases) > MaxFailureEntries {
		databases = databases[:MaxFailureEntries]
	}
	bounded.Databases = nil
	for _, database := range databases {
		database.Error = truncateBytes(database.GetError(), MaxErrorBytes)
		bounded.Databases = append(bounded.Databases, database)
	}
	return bounded
}

// boundOpenLineageDetail copies the detail with its error trimmed.
func boundOpenLineageDetail(detail *storepb.OpenLineageDetail) *storepb.OpenLineageDetail {
	if detail == nil {
		return nil
	}
	bounded, ok := proto.Clone(detail).(*storepb.OpenLineageDetail)
	if !ok {
		return detail
	}
	bounded.Error = truncateBytes(bounded.GetError(), MaxErrorBytes)
	return bounded
}

// truncateBytes cuts value to at most limit bytes without splitting a rune.
func truncateBytes(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	cut := value[:limit]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut
}

// DedupeKey builds the suppression key of a background notification:
// <event>:<target>:<bucket>, where the bucket is the start of the window the
// occurrence falls in. The unique index on (recipient_id, dedupe_key) turns a
// second occurrence inside one window into a no-op, so a broken producer or a
// failing instance cannot fill an inbox, and it does so across replicas because
// the suppression is the index rather than process memory.
func DedupeKey(event, target string, now time.Time, window time.Duration) string {
	if window <= 0 {
		return ""
	}
	return fmt.Sprintf("%s:%s:%d", event, target, now.UTC().Truncate(window).Unix())
}

// Send delivers one notification to one recipient. A notification whose
// dedupe key cannot be claimed is silently dropped: the message it repeats is
// already in the inbox.
func (s *Service) Send(ctx context.Context, n *storepb.Notification) error {
	if n == nil {
		return errors.New("notification is required")
	}
	if n.GetRecipientId() <= 0 {
		return errors.New("notification recipient is required")
	}
	// The write is detached from the caller's cancellation but not left unbounded:
	// it reports work that already happened, so a user who closed the tab must not
	// take the record with them, and a dead connection must not hold the caller —
	// often a runner goroutine — open either.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), writeTimeout)
	defer cancel()

	key := n.GetDedupeKey()
	if !s.claim(key) {
		return nil
	}
	if err := s.create(ctx, n); err != nil {
		// A failed write must not consume the window: the next occurrence has to
		// be allowed to try again.
		s.release(key)
		return err
	}
	return nil
}

// SendToWorkspaceAdmins delivers a copy of the notification to every workspace
// administrator. The message may arrive with recipient_id unset: it is stamped
// per recipient here.
func (s *Service) SendToWorkspaceAdmins(ctx context.Context, n *storepb.Notification) error {
	if n == nil {
		return errors.New("notification is required")
	}
	// See Send: detached from the caller's cancellation, bounded in time.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), writeTimeout)
	defer cancel()

	key := n.GetDedupeKey()
	if !s.claim(key) {
		return nil
	}
	fail := func(err error) error {
		s.release(key)
		return err
	}

	admins, err := s.WorkspaceAdminIDs(ctx)
	if err != nil {
		return fail(err)
	}
	if len(admins) == 0 {
		// No administrator can act on it, so there is no inbox to put it in. The
		// caller's own log line stays the record.
		slog.Warn("No workspace administrator to receive a notification",
			slog.String("type", n.GetType().String()))
		return nil
	}

	var firstErr error
	for _, id := range admins {
		clone, ok := proto.Clone(n).(*storepb.Notification)
		if !ok {
			return fail(errors.New("failed to clone notification"))
		}
		// Each recipient gets their own row, and therefore their own read state.
		clone.Id = 0
		clone.CreateTime = nil
		clone.ReadTime = nil
		clone.RecipientId = int32(id)
		if err := s.create(ctx, clone); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		return fail(firstErr)
	}
	return nil
}

// WorkspaceAdminIDs resolves the principals holding roles/workspaceAdmin in the
// workspace IAM policy, expanding group members. The allUsers pseudo-member
// cannot be bound to that role, and the system bot cannot sign in, so both are
// skipped rather than notified.
func (s *Service) WorkspaceAdminIDs(ctx context.Context) ([]int, error) {
	policy, err := s.store.GetWorkspaceIamPolicy(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the workspace IAM policy")
	}

	var ids []int
	seen := map[int]bool{}
	for _, binding := range policy.Policy.GetBindings() {
		if binding.GetRole() != common.FormatRole(common.WorkspaceAdmin) {
			continue
		}
		for _, member := range binding.GetMembers() {
			if member == common.AllUsers {
				continue
			}
			for _, user := range utils.GetUsersByMember(ctx, s.store, member) {
				if user == nil || user.ID == common.SystemBotID || seen[user.ID] {
					continue
				}
				seen[user.ID] = true
				ids = append(ids, user.ID)
			}
		}
	}
	return ids, nil
}

// create stamps the workspace on the message, writes it and reports it to the live
// subscriptions of its recipient. The workspace is single tenant, but the resource
// name and the list path both carry it, so the row records which one it belongs to
// rather than relying on the caller.
func (s *Service) create(ctx context.Context, n *storepb.Notification) error {
	if strings.TrimSpace(n.GetParent()) == "" {
		workspaceID, err := s.store.GetWorkspaceID(ctx)
		if err != nil {
			return errors.Wrap(err, "failed to resolve the workspace")
		}
		n.Parent = common.FormatWorkspace(workspaceID)
	}
	created, err := s.store.CreateNotification(ctx, n)
	if err != nil {
		return err
	}
	if created == nil {
		// The unique index on (recipient_id, dedupe_key) refused a repeat, so no row
		// was written and there is no new message to push: the inbox already holds
		// the one this repeats.
		return nil
	}
	s.publish(ctx, created)
	return nil
}

// publish hands one written message to the live subscriptions of its recipient. The
// counts are read here, once per message, so every connection of the same inbox
// shares one answer instead of each paying for its own query.
func (s *Service) publish(ctx context.Context, n *storepb.Notification) {
	recipientID := int(n.GetRecipientId())

	// Reading the counts and handing the message over happen under one lock, so two
	// messages written at nearly the same moment cannot deliver their counts out of
	// order and leave a client showing the older one. It is one mutex for the process
	// rather than one per recipient: a notification write is rare, the section is one
	// indexed count plus a non-blocking fan-out, and a map of per-recipient locks would
	// need the bounding and sweeping every other process-local map here has.
	//
	// Waiting for it is not bounded by the caller's context, so the section has to stay
	// short: the count is its only I/O, and it is answered by the partial index the badge
	// itself reads from.
	s.publishMu.Lock()
	defer s.publishMu.Unlock()

	event := Event{Notification: n}
	// A count that cannot be read is no reason to drop the message: the client
	// receives the event without it and asks for the counts itself.
	if unread, unseen, err := s.store.CountNotificationCounts(ctx, recipientID); err != nil {
		slog.Error("Failed to read the notification counts of a message",
			slog.Int("recipient_id", recipientID), clog.WithError(err))
	} else {
		event.UnreadCount, event.UnseenCount, event.CountsKnown = int32(unread), int32(unseen), true
	}
	s.hub.publish(recipientID, event)
}

// claim reports whether this dedupe key may be delivered, remembering it when
// it may. An empty key is never suppressed: a message addressed to the user who
// asked for the work is not a repeat of anything.
//
// The gate is process-local and is not what makes a repetition impossible — the
// unique index on (recipient_id, dedupe_key) is. It exists because the recipient
// of a background message is resolved by reading the IAM policy and expanding
// its groups, and a producer that sends nothing but garbage must not make every
// rejected request pay for that.
func (s *Service) claim(key string) bool {
	if key == "" {
		return true
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if _, ok := s.recent[key]; ok {
		return false
	}
	s.recent[key] = now
	if len(s.recent) > gateLimit {
		for k, at := range s.recent {
			if now.Sub(at) >= gateRetention {
				delete(s.recent, k)
			}
		}
	}
	return true
}

// release lets a failed delivery be retried within its window.
func (s *Service) release(key string) {
	if key == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.recent, key)
}

// LogFailure records a message that could not be delivered. Callers on a sync or
// ingestion path use it instead of propagating the error.
func LogFailure(err error, attrs ...any) {
	if err == nil {
		return
	}
	slog.Error("Failed to write a notification", append(attrs, clog.WithError(err))...)
}
