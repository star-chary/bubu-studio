package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"frame-space/backend/internal/identity"
	"github.com/go-oauth2/oauth2/v4"
	"github.com/go-oauth2/oauth2/v4/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// OAuthStore implements the library's storage with opaque, hashed credentials.
// Browser sessions are never accepted as MCP access tokens.
type OAuthStore struct{ DB *Store }

type OAuthClient struct {
	ID           string   `json:"client_id"`
	Name         string   `json:"client_name"`
	RedirectURIs []string `json:"redirect_uris"`
}

func (c *OAuthClient) GetID() string     { return c.ID }
func (c *OAuthClient) GetSecret() string { return "" }
func (c *OAuthClient) GetUserID() string { return "" }
func (c *OAuthClient) IsPublic() bool    { return true }
func (c *OAuthClient) GetDomain() string {
	b, _ := json.Marshal(c.RedirectURIs)
	return string(b)
}

type oauthTransactionKey struct{}
type oauthQueries interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (s *OAuthStore) queries(ctx context.Context) oauthQueries {
	if tx, ok := ctx.Value(oauthTransactionKey{}).(pgx.Tx); ok {
		return tx
	}
	return s.DB.Pool
}

// The library reads and removes a code in separate calls. The transaction and
// per-user/client lock make checking PKCE, consuming the code and issuing the token atomic.
func (s *OAuthStore) Transaction(ctx context.Context, key string, fn func(context.Context) error) error {
	tx, err := s.DB.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,718204008))`, key); err != nil {
		return err
	}
	if err = fn(context.WithValue(ctx, oauthTransactionKey{}, tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *OAuthStore) RegisterClient(ctx context.Context, c *OAuthClient) error {
	return s.Transaction(ctx, "mcp-client-registration", func(ctx context.Context) error {
		q := s.queries(ctx)
		if _, err := q.Exec(ctx, `DELETE FROM mcp_oauth_clients WHERE expires_at <= now()`); err != nil {
			return err
		}
		var count int
		if err := q.QueryRow(ctx, `SELECT count(*) FROM mcp_oauth_clients`).Scan(&count); err != nil {
			return err
		}
		if count >= 1000 {
			return ErrBusy
		}
		_, err := q.Exec(ctx, `INSERT INTO mcp_oauth_clients(id,name,redirect_uris) VALUES($1,$2,$3)`, c.ID, c.Name, c.RedirectURIs)
		return err
	})
}

func (s *OAuthStore) GetByID(ctx context.Context, id string) (oauth2.ClientInfo, error) {
	c := &OAuthClient{}
	err := s.queries(ctx).QueryRow(ctx, `SELECT id,name,redirect_uris FROM mcp_oauth_clients WHERE id=$1 AND expires_at>now()`, id).Scan(&c.ID, &c.Name, &c.RedirectURIs)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return c, err
}

func (s *OAuthStore) Create(ctx context.Context, info oauth2.TokenInfo) error {
	kind, secret := "access", info.GetAccess()
	expires := info.GetAccessCreateAt().Add(info.GetAccessExpiresIn())
	if info.GetCode() != "" {
		kind, secret = "code", info.GetCode()
		expires = info.GetCodeCreateAt().Add(info.GetCodeExpiresIn())
	}
	if secret == "" || info.GetRefresh() != "" {
		return ErrInvalid
	}
	raw, err := json.Marshal(info)
	if err != nil {
		return err
	}
	var metadata map[string]any
	if err = json.Unmarshal(raw, &metadata); err != nil {
		return err
	}
	delete(metadata, "Code")
	delete(metadata, "Access")
	delete(metadata, "Refresh")
	raw, err = json.Marshal(metadata)
	if err != nil {
		return err
	}
	q := s.queries(ctx)
	// Cleanup is bounded by the account and occurs only during credential issuance.
	if _, err = q.Exec(ctx, `DELETE FROM mcp_oauth_tokens WHERE user_id=$1 AND expires_at<=now()`, info.GetUserID()); err != nil {
		return err
	}
	tag, err := q.Exec(ctx, `INSERT INTO mcp_oauth_tokens(token_hash,kind,client_id,user_id,metadata,expires_at)
		SELECT $1,$2,$3,id,$5,$6 FROM users WHERE id=$4 AND role='admin' AND status='active'`,
		identity.TokenHash(secret), kind, info.GetClientID(), info.GetUserID(), raw, expires)
	if err == nil && tag.RowsAffected() != 1 {
		return ErrAdminRequired
	}
	return err
}

func (s *OAuthStore) get(ctx context.Context, secret, kind string) (oauth2.TokenInfo, error) {
	if !identity.ValidToken(secret) {
		return nil, nil
	}
	var raw []byte
	err := s.queries(ctx).QueryRow(ctx, `SELECT t.metadata FROM mcp_oauth_tokens t
		JOIN users u ON u.id=t.user_id JOIN mcp_oauth_clients c ON c.id=t.client_id
		WHERE t.token_hash=$1 AND t.kind=$2 AND t.revoked_at IS NULL AND t.expires_at>now()
		AND c.expires_at>now() AND u.role='admin' AND u.status='active'`, identity.TokenHash(secret), kind).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	info := models.NewToken()
	if err = json.Unmarshal(raw, info); err != nil {
		return nil, err
	}
	if kind == "code" {
		info.SetCode(secret)
	} else {
		info.SetAccess(secret)
	}
	return info, nil
}

func (s *OAuthStore) GetByCode(ctx context.Context, code string) (oauth2.TokenInfo, error) {
	return s.get(ctx, code, "code")
}
func (s *OAuthStore) GetByAccess(ctx context.Context, access string) (oauth2.TokenInfo, error) {
	return s.get(ctx, access, "access")
}
func (s *OAuthStore) GetByRefresh(context.Context, string) (oauth2.TokenInfo, error) { return nil, nil }
func (s *OAuthStore) RemoveByCode(ctx context.Context, code string) error {
	_, err := s.queries(ctx).Exec(ctx, `UPDATE mcp_oauth_tokens SET revoked_at=now() WHERE token_hash=$1 AND kind='code' AND revoked_at IS NULL`, identity.TokenHash(code))
	return err
}
func (s *OAuthStore) RemoveByAccess(ctx context.Context, access string) error {
	_, err := s.queries(ctx).Exec(ctx, `UPDATE mcp_oauth_tokens SET revoked_at=now() WHERE token_hash=$1 AND kind='access' AND revoked_at IS NULL`, identity.TokenHash(access))
	return err
}
func (s *OAuthStore) RemoveByRefresh(context.Context, string) error { return ErrInvalid }

type OAuthConnection struct {
	ClientID  string
	Name      string
	CreatedAt time.Time
	ExpiresAt time.Time
}

func (s *OAuthStore) Connections(ctx context.Context, userID string) ([]OAuthConnection, error) {
	rows, err := s.DB.Pool.Query(ctx, `SELECT c.id,c.name,max(t.created_at),max(t.expires_at)
		FROM mcp_oauth_tokens t JOIN mcp_oauth_clients c ON c.id=t.client_id
		WHERE t.user_id=$1 AND t.revoked_at IS NULL AND t.expires_at>now() AND c.expires_at>now()
		GROUP BY c.id,c.name ORDER BY max(t.created_at) DESC LIMIT 100`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []OAuthConnection{}
	for rows.Next() {
		var item OAuthConnection
		if err := rows.Scan(&item.ClientID, &item.Name, &item.CreatedAt, &item.ExpiresAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *OAuthStore) RevokeClient(ctx context.Context, userID, clientID string) error {
	return s.Transaction(ctx, "oauth-client:"+userID+":"+clientID, func(ctx context.Context) error {
		_, err := s.queries(ctx).Exec(ctx, `UPDATE mcp_oauth_tokens SET revoked_at=now() WHERE user_id=$1 AND client_id=$2 AND revoked_at IS NULL`, userID, clientID)
		return err
	})
}

// Both HTTP and MCP supply a server-verified actor, never a caller-provided role.
func (s *Store) AdminUsersForActor(ctx context.Context, actorID, query string, limit, offset int) ([]AdminUser, int64, error) {
	if !ValidID(actorID) {
		return nil, 0, ErrAdminRequired
	}
	var allowed bool
	if err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND role='admin' AND status='active')`, actorID).Scan(&allowed); err != nil {
		return nil, 0, err
	}
	if !allowed {
		return nil, 0, ErrAdminRequired
	}
	return s.AdminUsers(ctx, query, limit, offset)
}
