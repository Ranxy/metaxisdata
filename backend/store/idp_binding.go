package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// rowQuerier is the shape *sql.DB and *sql.Tx share, so a binding lookup can run
// on its own or inside the transaction that writes one.
type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// insertIDPBinding binds an account to one identity provider subject. It is the
// write half of the binding contract, shared by account creation and by a login
// that makes an existing account reachable through a provider.
func insertIDPBinding(ctx context.Context, tx *sql.Tx, principalID int, binding *IDPBinding) error {
	if binding == nil || binding.ResourceID == "" || binding.Subject == "" {
		return errors.New("an identity provider binding needs a resource id and a subject")
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO principal_idp_binding (principal_id, idp_resource_id, subject)
		VALUES ($1, $2, $3)`,
		principalID, binding.ResourceID, binding.Subject); err != nil {
		if isUniqueViolation(err) {
			return common.Errorf(common.Conflict, "the identity provider subject is already bound to another account")
		}
		return err
	}
	return nil
}

// idpBindingOwner returns the account a binding belongs to, if any.
func (*Store) idpBindingOwner(ctx context.Context, q rowQuerier, binding *IDPBinding) (int, bool, error) {
	var principalID int
	if err := q.QueryRowContext(ctx, `
		SELECT principal_id FROM principal_idp_binding
		WHERE idp_resource_id = $1 AND subject = $2`,
		binding.ResourceID, binding.Subject).Scan(&principalID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, err
	}
	return principalID, true, nil
}

// BindAccountForSSO makes an existing account reachable through an identity
// provider subject.
//
// An account that carries no binding at all is being adopted: the password it had
// is replaced with a random one, which — through the password change time — also
// retires the sessions minted with it. An account that already signs in through a
// provider only gains another way in and keeps its sessions, because nothing it
// held was a secret to anyone else.
//
// It reports whether this call is the one that adopted the account. It fails when
// the subject belongs to a different account, or when the account stops being the
// active end-user account at that address while the login is resolving.
func (s *Store) BindAccountForSSO(ctx context.Context, account *UserMessage, binding *IDPBinding, passwordHash string) (bool, error) {
	if account.ID == common.SystemBotID {
		return false, errors.Errorf("cannot update system bot")
	}
	if binding == nil || binding.ResourceID == "" || binding.Subject == "" {
		return false, errors.New("an identity provider binding needs a resource id and a subject")
	}

	profile := proto.CloneOf(account.Profile)
	if profile == nil {
		profile = &storepb.UserProfile{}
	}
	profile.LastChangePasswordTime = timestamppb.New(time.Now())
	profileBytes, err := protojson.Marshal(profile)
	if err != nil {
		return false, err
	}

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	// The account has to still be the end-user account at the address the login
	// resolved it to; an administrator may have deleted or renamed it meanwhile.
	// FOR UPDATE serializes two logins that are binding the same account, and it
	// comes first so that the binding is read on this side of the lock: a duplicate
	// login then sees the row the winner just committed instead of failing on the
	// primary key.
	var exists bool
	if err := tx.QueryRowContext(ctx, `
		SELECT TRUE FROM principal
		WHERE id = $1 AND deleted = FALSE AND type = $2 AND LOWER(email) = LOWER($3)
		FOR UPDATE`,
		account.ID, storepb.PrincipalType_END_USER.String(), account.Email).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, common.Errorf(common.Conflict, "the account changed while signing in")
		}
		return false, err
	}

	ownerID, bound, err := s.idpBindingOwner(ctx, tx, binding)
	if err != nil {
		return false, err
	}
	if bound && ownerID != account.ID {
		return false, common.Errorf(common.Conflict, "the identity provider subject is already bound to another account")
	}

	var existingBindings int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM principal_idp_binding WHERE principal_id = $1`,
		account.ID).Scan(&existingBindings); err != nil {
		return false, err
	}

	if !bound {
		if err := insertIDPBinding(ctx, tx, account.ID, binding); err != nil {
			return false, err
		}
	}

	adopted := !bound && existingBindings == 0
	if adopted {
		if _, err := tx.ExecContext(ctx, `
			UPDATE principal
			SET password_hash = $1, profile = $2
			WHERE id = $3`,
			passwordHash, profileBytes, account.ID); err != nil {
			return false, err
		}
	}

	if err := tx.Commit(); err != nil {
		return false, err
	}

	s.userIDCache.Remove(account.ID)
	s.userEmailCache.Remove(account.Email)
	return adopted, nil
}
