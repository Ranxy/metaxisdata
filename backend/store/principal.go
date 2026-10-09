package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lib/pq"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// systemBotUser mirrors the seeded row in LATEST.sql. It is the fallback when
// the row cannot be read, so the two must describe the same principal.
var systemBotUser = &UserMessage{
	ID:    common.SystemBotID,
	Name:  "SYSTEM",
	Email: "support@example.com",
	Type:  storepb.PrincipalType_SYSTEM_BOT,
}

// FindUserMessage is the message for finding users.
type FindUserMessage struct {
	ID          *int
	Email       *string
	ShowDeleted bool
	Type        *storepb.PrincipalType
	Limit       *int
	Offset      *int
	Filter      *ListResourceFilter
}

// IDPBinding identifies the identity provider subject an account may sign in
// with. It is the identity an SSO login resolves against; the email is not.
type IDPBinding struct {
	ResourceID string
	Subject    string
}

// UpdateUserMessage is the message to update a user.
type UpdateUserMessage struct {
	Email        *string
	Name         *string
	PasswordHash *string
	Delete       *bool
	Profile      *storepb.UserProfile
	Phone        *string
	// Language replaces the profile's language key instead of the whole column,
	// so a preference change cannot write a stale copy of the rest of the
	// profile back into the row.
	Language *string
}

// UserMessage is the message for an user.
type UserMessage struct {
	ID int
	// Email must be lower case.
	Email         string
	Name          string
	Type          storepb.PrincipalType
	PasswordHash  string
	MemberDeleted bool
	Profile       *storepb.UserProfile
	// Phone conforms E.164 format.
	Phone string
	// output only
	CreatedAt time.Time
	// The group email list
	Groups []string
}

type UserStat struct {
	Type    storepb.PrincipalType
	Deleted bool
	Count   int
}

// isUniqueViolation reports whether err is a PostgreSQL unique-constraint
// violation (SQLSTATE 23505), so callers can map it to a conflict instead of a
// 500. Both error shapes are checked: the store talks to PostgreSQL through the
// pgx stdlib driver, which returns *pgconn.PgError, while lib/pq types appear
// through helper code that still uses the pq package.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}

// GetSystemBotUser gets the system bot.
func (s *Store) GetSystemBotUser(ctx context.Context) *UserMessage {
	user, err := s.GetUserByID(ctx, common.SystemBotID)
	if err != nil {
		slog.Error("failed to find system bot", slog.Int("id", common.SystemBotID), log.WithError(err))
		return systemBotUser
	}
	if user == nil {
		return systemBotUser
	}
	return user
}

// GetUserByID gets the user by ID.
func (s *Store) GetUserByID(ctx context.Context, id int) (*UserMessage, error) {
	if v, ok := s.userIDCache.Get(id); ok && !s.cacheDisabled {
		return v, nil
	}
	return s.getUser(ctx, &FindUserMessage{ID: &id, ShowDeleted: true})
}

// GetUserByEmail gets the user by email.
func (s *Store) GetUserByEmail(ctx context.Context, email string) (*UserMessage, error) {
	cacheKey := userEmailCacheKey(email)
	if v, ok := s.userEmailCache.Get(cacheKey); ok && !s.cacheDisabled {
		return v, nil
	}
	return s.getUser(ctx, &FindUserMessage{Email: &email, ShowDeleted: true})
}

// GetActiveUserByEmail gets a user by email that is still active, ignoring
// soft-deleted rows. A deleted account is inert — it cannot sign in and its
// address no longer belongs to it, which is what the partial unique email index
// already assumes — so it must not keep that address out of a new account.
func (s *Store) GetActiveUserByEmail(ctx context.Context, email string) (*UserMessage, error) {
	return s.getUser(ctx, &FindUserMessage{Email: &email})
}

// GetUserByIDPBinding gets the user an identity provider subject is bound to. The
// binding is what an SSO login resolves against: a user who changed their email at
// the provider keeps the same account, and nobody can claim it by taking the
// address first. Soft-deleted rows are returned too, so that signing in cannot
// resurrect a deactivated account.
func (s *Store) GetUserByIDPBinding(ctx context.Context, binding *IDPBinding) (*UserMessage, error) {
	userID, found, err := s.idpBindingOwner(ctx, s.GetDB(), binding)
	if err != nil || !found {
		return nil, err
	}
	return s.GetUserByID(ctx, userID)
}

// userEmailCacheKey normalizes an email the same way listUserImpl does, so a
// lookup with different casing hits the cached row instead of a second query.
func userEmailCacheKey(email string) string {
	if email == common.AllUsers {
		return email
	}
	return strings.ToLower(email)
}

// getUser loads a single user (with groups) and caches it. It replaces the old
// "cache miss loads every user" path, which made each authenticated request an
// unindexed full-table scan.
func (s *Store) getUser(ctx context.Context, find *FindUserMessage) (*UserMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	users, err := listUserImpl(ctx, tx, find)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, nil
	}
	if len(users) > 1 {
		return nil, errors.Errorf("found multiple users matching the criteria")
	}

	user := users[0]
	s.userIDCache.Add(user.ID, user)
	s.userEmailCache.Add(user.Email, user)
	return user, nil
}

func (s *Store) StatUsers(ctx context.Context) ([]*UserStat, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
	SELECT
		COUNT(*),
		type,
		deleted
	FROM principal
	GROUP BY type, deleted`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []*UserStat

	for rows.Next() {
		var stat UserStat
		var typeString string
		if err := rows.Scan(
			&stat.Count,
			&typeString,
			&stat.Deleted,
		); err != nil {
			return nil, err
		}
		if typeValue, ok := storepb.PrincipalType_value[typeString]; ok {
			stat.Type = storepb.PrincipalType(typeValue)
		} else {
			return nil, errors.Errorf("invalid principal type string: %s", typeString)
		}
		stats = append(stats, &stat)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to scan rows")
	}

	return stats, nil
}

// ListUsers list users.
func (s *Store) ListUsers(ctx context.Context, find *FindUserMessage) ([]*UserMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	users, err := listUserImpl(ctx, tx, find)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	for _, user := range users {
		s.userIDCache.Add(user.ID, user)
		s.userEmailCache.Add(user.Email, user)
	}
	return users, nil
}

func listUserImpl(ctx context.Context, txn *sql.Tx, find *FindUserMessage) ([]*UserMessage, error) {
	where, args := []string{"TRUE"}, []any{}
	if filter := find.Filter; filter != nil {
		where = append(where, filter.Where)
		args = append(args, filter.Args...)
	}
	if v := find.ID; v != nil {
		where, args = append(where, fmt.Sprintf("principal.id = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.Email; v != nil {
		if *v == common.AllUsers {
			where, args = append(where, fmt.Sprintf("principal.email = $%d", len(args)+1)), append(args, *v)
		} else {
			where, args = append(where, fmt.Sprintf("principal.email = $%d", len(args)+1)), append(args, strings.ToLower(*v))
		}
	}
	if v := find.Type; v != nil {
		where, args = append(where, fmt.Sprintf("principal.type = $%d", len(args)+1)), append(args, v.String())
	}
	if !find.ShowDeleted {
		where, args = append(where, fmt.Sprintf("principal.deleted = $%d", len(args)+1)), append(args, false)
	}

	// Join the user_group table to find groups for each user.
	// The user will be stored in the user_group.payload.members.member field, the member is in the "users/{id}" format
	query := `WITH user_groups AS (
		SELECT
			principal.id AS user_id,
			COALESCE(ARRAY_AGG(user_group.email ORDER BY user_group.email) FILTER (WHERE user_group.email IS NOT NULL), '{}') AS groups
		FROM principal
		LEFT JOIN user_group ON EXISTS (
			SELECT 1 FROM jsonb_array_elements(user_group.payload->'members') AS m
			WHERE m->>'member' = CONCAT('users/', principal.id)
		)
		GROUP BY principal.id
	)
	SELECT
		principal.id AS user_id,
		principal.deleted,
		principal.email,
		principal.name,
		principal.type,
		principal.password_hash,
		principal.phone,
		principal.profile,
		principal.created_at,
		user_groups.groups
	FROM principal
	INNER JOIN user_groups ON principal.id = user_groups.user_id
	WHERE ` + strings.Join(where, " AND ") + ` ORDER BY type DESC, created_at ASC`

	if v := find.Limit; v != nil {
		args = append(args, max(*v, 0))
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if v := find.Offset; v != nil {
		args = append(args, max(*v, 0))
		query += fmt.Sprintf(" OFFSET $%d", len(args))
	}

	var userMessages []*UserMessage
	rows, err := txn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var userMessage UserMessage
		var profileBytes []byte
		var typeString string
		var groups pq.StringArray
		if err := rows.Scan(
			&userMessage.ID,
			&userMessage.MemberDeleted,
			&userMessage.Email,
			&userMessage.Name,
			&typeString,
			&userMessage.PasswordHash,
			&userMessage.Phone,
			&profileBytes,
			&userMessage.CreatedAt,
			&groups,
		); err != nil {
			return nil, err
		}
		userMessage.Groups = []string(groups)
		if typeValue, ok := storepb.PrincipalType_value[typeString]; ok {
			userMessage.Type = storepb.PrincipalType(typeValue)
		} else {
			return nil, errors.Errorf("invalid user type string: %s", typeString)
		}

		profile := storepb.UserProfile{}
		if err := common.ProtojsonUnmarshaler.Unmarshal(profileBytes, &profile); err != nil {
			return nil, err
		}
		userMessage.Profile = &profile

		userMessages = append(userMessages, &userMessage)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return userMessages, nil
}

// createEndUserAdvisoryLockKey serializes end-user creation so that two
// concurrent registrations cannot both be elected as the first workspace admin.
const createEndUserAdvisoryLockKey int64 = 0x6d65746178697301

// CreateUser creates an user.
//
// When the new user is an end user and there is no other active end user, the
// user is granted the workspace admin role in the same transaction. This is
// what bootstraps a fresh workspace, and doing it atomically prevents a
// concurrent registration from also being elected admin.
//
// A binding makes the account reachable through an identity provider subject; it
// is written in the same transaction, because an SSO account without one could
// not sign in again. Password accounts pass nil.
func (s *Store) CreateUser(ctx context.Context, create *UserMessage, binding *IDPBinding) (*UserMessage, error) {
	// Double check the passing-in emails.
	// We use lower-case for emails.
	if create.Email != strings.ToLower(create.Email) {
		return nil, errors.Errorf("emails must be lower-case when they are passed into store")
	}

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", createEndUserAdvisoryLockKey); err != nil {
		return nil, err
	}

	activeEndUserCount := 0
	if create.Type == storepb.PrincipalType_END_USER {
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM principal
			WHERE type = $1 AND deleted = FALSE`,
			storepb.PrincipalType_END_USER.String(),
		).Scan(&activeEndUserCount); err != nil {
			return nil, err
		}
	}

	if create.Profile == nil {
		create.Profile = &storepb.UserProfile{}
	}
	profileBytes, err := protojson.Marshal(create.Profile)
	if err != nil {
		return nil, err
	}

	set := []string{"email", "name", "type", "password_hash", "phone", "profile"}
	args := []any{create.Email, create.Name, create.Type.String(), create.PasswordHash, create.Phone, profileBytes}
	placeholder := []string{}
	for index := range set {
		placeholder = append(placeholder, fmt.Sprintf("$%d", index+1))
	}

	var userID int
	if err := tx.QueryRowContext(ctx, fmt.Sprintf(`
			INSERT INTO principal (
				%s
			)
			VALUES (%s)
			RETURNING id, created_at
		`, strings.Join(set, ","), strings.Join(placeholder, ",")),
		args...,
	).Scan(&userID, &create.CreatedAt); err != nil {
		if isUniqueViolation(err) {
			// The address is taken; the caller pre-checks it, so reaching this is
			// a race.
			return nil, common.Errorf(common.Conflict, "user already exists")
		}
		return nil, err
	}

	if binding != nil {
		if err := insertIDPBinding(ctx, tx, userID, binding); err != nil {
			return nil, err
		}
	}

	if create.Type == storepb.PrincipalType_END_USER && activeEndUserCount == 0 {
		if err := s.patchWorkspaceIamPolicyImpl(ctx, tx, &PatchIamPolicyMessage{
			Member: common.FormatUserUID(userID),
			Roles:  []string{common.FormatRole(common.WorkspaceAdmin)},
		}); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	s.policyCache.Remove(getPolicyCacheKey(storepb.Policy_WORKSPACE, "", storepb.Policy_IAM))

	user := &UserMessage{
		ID:           userID,
		Email:        create.Email,
		Name:         create.Name,
		Type:         create.Type,
		PasswordHash: create.PasswordHash,
		Phone:        create.Phone,
		CreatedAt:    create.CreatedAt,
		Profile:      create.Profile,
	}
	s.userIDCache.Add(user.ID, user)
	s.userEmailCache.Add(user.Email, user)
	return user, nil
}

// profileAssignments returns the SET fragments and arguments that persist the
// profile parts of a patch, numbered after the arguments the caller already
// collected.
//
// A language on its own is written as a single JSONB key. That matters: a
// whole-column write carries the profile this caller read back into the row, so
// it would resurrect whatever a concurrent write changed in it — including the
// password change time that retires sessions, which is the failure the
// targeted write in RecordLastLogin exists to avoid. A patch that already
// carries a whole profile (the password branch builds one) absorbs the language
// into that value instead.
func profileAssignments(set []string, args []any, patch *UpdateUserMessage) ([]string, []any, error) {
	profile := patch.Profile
	if patch.Language != nil {
		if profile == nil {
			set = append(set, fmt.Sprintf("profile = jsonb_set(profile, ARRAY[$%d], to_jsonb($%d::text))", len(args)+1, len(args)+2))
			return set, append(args, userProfileLanguageKey, *patch.Language), nil
		}
		merged := proto.CloneOf(profile)
		merged.Language = *patch.Language
		profile = merged
	}
	if profile != nil {
		profileBytes, err := protojson.Marshal(profile)
		if err != nil {
			return set, args, err
		}
		set = append(set, fmt.Sprintf("profile = $%d", len(args)+1))
		args = append(args, profileBytes)
	}
	return set, args, nil
}

// UpdateUser updates a user.
func (s *Store) UpdateUser(ctx context.Context, currentUser *UserMessage, patch *UpdateUserMessage) (*UserMessage, error) {
	if currentUser.ID == common.SystemBotID {
		return nil, errors.Errorf("cannot update system bot")
	}

	principalSet, principalArgs := []string{}, []any{}
	if v := patch.Delete; v != nil {
		principalSet, principalArgs = append(principalSet, fmt.Sprintf("deleted = $%d", len(principalArgs)+1)), append(principalArgs, *v)
	}
	if v := patch.Email; v != nil {
		principalSet, principalArgs = append(principalSet, fmt.Sprintf("email = $%d", len(principalArgs)+1)), append(principalArgs, strings.ToLower(*v))
	}
	if v := patch.Name; v != nil {
		principalSet, principalArgs = append(principalSet, fmt.Sprintf("name = $%d", len(principalArgs)+1)), append(principalArgs, *v)
	}
	if v := patch.PasswordHash; v != nil {
		principalSet, principalArgs = append(principalSet, fmt.Sprintf("password_hash = $%d", len(principalArgs)+1)), append(principalArgs, *v)
		// Clone: patch.Profile (or currentUser.Profile) may be the shared cache
		// entry. The change time is stamped even when the caller sends a profile
		// in the same patch, otherwise it would be silently lost.
		profile := proto.CloneOf(patch.Profile)
		if profile == nil {
			profile = proto.CloneOf(currentUser.Profile)
		}
		if profile == nil {
			profile = &storepb.UserProfile{}
		}
		profile.LastChangePasswordTime = timestamppb.New(time.Now())
		patch.Profile = profile
	}
	if v := patch.Phone; v != nil {
		principalSet, principalArgs = append(principalSet, fmt.Sprintf("phone = $%d", len(principalArgs)+1)), append(principalArgs, *v)
	}
	var err error
	principalSet, principalArgs, err = profileAssignments(principalSet, principalArgs, patch)
	if err != nil {
		return nil, err
	}
	principalArgs = append(principalArgs, currentUser.ID)

	if len(principalSet) == 0 {
		return currentUser, nil
	}

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
		UPDATE principal
		SET `+strings.Join(principalSet, ", ")+`
		WHERE id = $%d
	`, len(principalArgs)),
		principalArgs...,
	); err != nil {
		if isUniqueViolation(err) {
			return nil, common.Errorf(common.Conflict, "email already exists")
		}
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	s.userEmailCache.Remove(currentUser.Email)
	s.userIDCache.Remove(currentUser.ID)
	user, err := s.GetUserByID(ctx, currentUser.ID)
	if err != nil {
		return nil, err
	}

	s.userIDCache.Add(currentUser.ID, user)
	s.userEmailCache.Add(user.Email, user)
	return user, nil
}

// userProfileLastLoginKey is the JSONB key protojson produces for
// UserProfile.last_login_time. RecordLastLogin writes that key directly, so a
// rename of the proto field would silently stop recording logins.
const userProfileLastLoginKey = "lastLoginTime"

// userProfileLanguageKey is the JSONB key protojson produces for
// UserProfile.language. A language update writes that key on its own, so the
// same rename would silently stop recording the preference.
const userProfileLanguageKey = "language"

// RecordLastLogin stamps the last login time in the profile without rewriting the
// rest of the column. A login used to write the whole profile back from the row it
// had read, which could resurrect a stale copy of it — including the password
// change time that retires sessions, the value an SSO adoption sets.
func (s *Store) RecordLastLogin(ctx context.Context, user *UserMessage, at time.Time) error {
	if _, err := s.GetDB().ExecContext(ctx, `
		UPDATE principal
		SET profile = jsonb_set(profile, ARRAY[$1], to_jsonb($2::text))
		WHERE id = $3`,
		userProfileLastLoginKey, at.UTC().Format(time.RFC3339Nano), user.ID); err != nil {
		return err
	}
	s.userIDCache.Remove(user.ID)
	s.userEmailCache.Remove(user.Email)
	return nil
}
