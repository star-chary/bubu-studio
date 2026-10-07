package persistence

import (
	"context"
	"errors"
	"time"

	"frame-space/backend/internal/identity"
	"github.com/jackc/pgx/v5"
)

var ErrCredentials = errors.New("invalid credentials")
var ErrSession = errors.New("invalid session")

type User struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}
type Session struct {
	User      User      `json:"user"`
	CSRFToken string    `json:"csrfToken"`
	ExpiresAt time.Time `json:"expiresAt"`
	Token     string    `json:"-"`
}

// LoginOrCreate never replaces an existing password. A unique email constraint
// arbitrates concurrent registrations; the losing request verifies the winner.
func (s *Store) LoginOrCreate(ctx context.Context, email, password string) (Session, error) {
	var user User
	var hash, status string
	err := s.Pool.QueryRow(ctx, `SELECT id::text,email,password_hash,status FROM users WHERE email=$1`, email).Scan(&user.ID, &user.Email, &hash, &status)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Session{}, err
	}
	newUser := errors.Is(err, pgx.ErrNoRows)
	if !newUser && (!identity.PasswordMatches(password, hash) || status != "active") {
		return Session{}, ErrCredentials
	}
	if newUser {
		hash = identity.PasswordHash(password)
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback(context.Background())
	if newUser {
		user = User{ID: identity.ID(), Email: email}
		tag, err := tx.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,$2,$3) ON CONFLICT(email) DO NOTHING`, user.ID, email, hash)
		if err != nil {
			return Session{}, err
		}
		if tag.RowsAffected() == 0 {
			if err := tx.QueryRow(ctx, `SELECT id::text,email,password_hash,status FROM users WHERE email=$1 FOR SHARE`, email).Scan(&user.ID, &user.Email, &hash, &status); err != nil {
				return Session{}, err
			}
			if !identity.PasswordMatches(password, hash) || status != "active" {
				return Session{}, ErrCredentials
			}
		}
	} else {
		// Serialize disabling/password changes with session creation.
		var currentHash string
		if err := tx.QueryRow(ctx, `SELECT password_hash,status FROM users WHERE id=$1 FOR SHARE`, user.ID).Scan(&currentHash, &status); err != nil {
			return Session{}, err
		}
		if status != "active" || currentHash != hash {
			return Session{}, ErrCredentials
		}
	}
	out := Session{User: user, Token: identity.Token(), CSRFToken: identity.Token()}
	err = tx.QueryRow(ctx, `INSERT INTO user_sessions(token_hash,user_id,csrf_token) VALUES($1,$2,$3) RETURNING absolute_expires_at`, identity.TokenHash(out.Token), user.ID, out.CSRFToken).Scan(&out.ExpiresAt)
	if err != nil {
		return Session{}, err
	}
	// Bounded to this account; do not scan/update all sessions on each request.
	if _, err = tx.Exec(ctx, `DELETE FROM user_sessions WHERE user_id=$1 AND (revoked_at IS NOT NULL OR idle_expires_at<=now() OR absolute_expires_at<=now())`, user.ID); err != nil {
		return Session{}, err
	}
	return out, tx.Commit(ctx)
}

func (s *Store) Session(ctx context.Context, token string, touch bool) (Session, error) {
	if !identity.ValidToken(token) {
		return Session{}, ErrSession
	}
	var out Session
	err := s.Pool.QueryRow(ctx, `SELECT u.id::text,u.email,s.csrf_token,s.absolute_expires_at FROM user_sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=$1 AND s.revoked_at IS NULL AND s.idle_expires_at>now() AND s.absolute_expires_at>now() AND u.status='active'`, identity.TokenHash(token)).Scan(&out.User.ID, &out.User.Email, &out.CSRFToken, &out.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrSession
	}
	if err != nil {
		return out, err
	}
	if touch {
		_, err = s.Pool.Exec(ctx, `UPDATE user_sessions SET idle_expires_at=LEAST(now()+interval '24 hours',absolute_expires_at) WHERE token_hash=$1 AND revoked_at IS NULL AND idle_expires_at>now() AND absolute_expires_at>now() AND idle_expires_at<now()+interval '23 hours 55 minutes'`, identity.TokenHash(token))
	}
	return out, err
}
func (s *Store) RevokeSession(ctx context.Context, token string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE user_sessions SET revoked_at=now() WHERE token_hash=$1`, identity.TokenHash(token))
	return err
}

func (s *Store) RequireCanvasOwner(ctx context.Context, id, userID string) error {
	if !ValidID(id) || !ValidID(userID) {
		return ErrNotFound
	}
	var found bool
	err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM canvases WHERE id=$1 AND owner_user_id=$2)`, id, userID).Scan(&found)
	if err != nil {
		return err
	}
	if !found {
		return ErrNotFound
	}
	return nil
}
func (s *Store) RequireTaskOwner(ctx context.Context, id, userID string) error {
	if !ValidID(id) || !ValidID(userID) {
		return ErrNotFound
	}
	var found bool
	err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM generation_tasks t JOIN canvases c ON c.id=t.canvas_id WHERE t.id=$1 AND c.owner_user_id=$2)`, id, userID).Scan(&found)
	if err != nil {
		return err
	}
	if !found {
		return ErrNotFound
	}
	return nil
}
func (s *Store) RequireAssetOwner(ctx context.Context, key, userID string) error {
	if !ValidID(userID) {
		return ErrNotFound
	}
	var found bool
	err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM assets a JOIN canvases c ON c.id=a.canvas_id WHERE a.key=$1 AND c.owner_user_id=$2)`, key, userID).Scan(&found)
	if err != nil {
		return err
	}
	if !found {
		return ErrNotFound
	}
	return nil
}

func (s *Store) SaveOwnedCanvas(ctx context.Context, id, userID string, version int64, snapshot Snapshot) (int64, error) {
	if err := s.RequireCanvasOwner(ctx, id, userID); err != nil {
		return 0, err
	}
	var next int64
	err := s.Pool.QueryRow(ctx, `UPDATE canvases SET snapshot=$4,version=version+1,updated_at=now() WHERE id=$1 AND owner_user_id=$2 AND version=$3 RETURNING version`, id, userID, version, snapshot).Scan(&next)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrConflict
	}
	return next, err
}
