package persistence

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

const MaxAdminGrantPoints int64 = 10000
const MaxGrantReasonLength = 200

var ErrAdminRequired = errors.New("administrator required")
var ErrUserDisabled = errors.New("target user disabled")

type AdminUser struct {
	User
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	CreditAccount
}

type AdminCreditEntry struct {
	CreditEntry
	Source     string  `json:"source"`
	ActorID    *string `json:"actorId,omitempty"`
	ActorEmail *string `json:"actorEmail,omitempty"`
	Reason     string  `json:"reason"`
}

type AdminGrantResult struct {
	Account  CreditAccount    `json:"account"`
	Entry    AdminCreditEntry `json:"entry"`
	Replayed bool             `json:"replayed"`
}

const adminUserColumns = `u.id::text,u.email,u.role,u.status,u.created_at,COALESCE(a.available,0),COALESCE(a.reserved,0)`

func scanAdminUser(row pgx.Row) (AdminUser, error) {
	var u AdminUser
	err := row.Scan(&u.ID, &u.Email, &u.Role, &u.Status, &u.CreatedAt, &u.Available, &u.Reserved)
	return u, mapError(err)
}

func (s *Store) AdminUsers(ctx context.Context, query string, limit, offset int) ([]AdminUser, int64, error) {
	query = strings.ToLower(strings.TrimSpace(query))
	if utf8.RuneCountInString(query) > 254 || limit < 1 || limit > 100 || offset < 0 || offset > 100000 {
		return nil, 0, ErrInvalid
	}
	const filter = ` WHERE ($1='' OR strpos(u.email,$1)>0 OR u.id::text=$1)`
	var total int64
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM users u`+filter, query).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT `+adminUserColumns+` FROM users u LEFT JOIN credit_accounts a ON a.user_id=u.id`+filter+` ORDER BY u.created_at DESC,u.id DESC LIMIT $2 OFFSET $3`, query, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	users := []AdminUser{}
	for rows.Next() {
		u, err := scanAdminUser(rows)
		if err != nil {
			return nil, 0, err
		}
		users = append(users, u)
	}
	return users, total, rows.Err()
}

func (s *Store) AdminUser(ctx context.Context, id string) (AdminUser, error) {
	if !ValidID(id) {
		return AdminUser{}, ErrNotFound
	}
	return scanAdminUser(s.Pool.QueryRow(ctx, `SELECT `+adminUserColumns+` FROM users u LEFT JOIN credit_accounts a ON a.user_id=u.id WHERE u.id=$1`, id))
}

const adminEntryColumns = `l.id,l.operation,l.available_delta,l.reserved_delta,l.available_after,l.reserved_after,l.task_id::text,l.created_at,l.source,l.actor_user_id::text,actor.email,l.reason`

func scanAdminEntry(row pgx.Row) (AdminCreditEntry, error) {
	var e AdminCreditEntry
	err := row.Scan(&e.ID, &e.Operation, &e.AvailableDelta, &e.ReservedDelta, &e.AvailableAfter, &e.ReservedAfter, &e.TaskID, &e.CreatedAt, &e.Source, &e.ActorID, &e.ActorEmail, &e.Reason)
	return e, err
}

func (s *Store) AdminCreditEntries(ctx context.Context, userID string, limit, offset int) ([]AdminCreditEntry, bool, error) {
	if !ValidID(userID) || limit < 1 || limit > 100 || offset < 0 || offset > 100000 {
		return nil, false, ErrInvalid
	}
	rows, err := s.Pool.Query(ctx, `SELECT `+adminEntryColumns+` FROM credit_ledger l LEFT JOIN users actor ON actor.id=l.actor_user_id WHERE l.user_id=$1 ORDER BY l.id DESC LIMIT $2 OFFSET $3`, userID, limit+1, offset)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	entries := []AdminCreditEntry{}
	for rows.Next() {
		e, err := scanAdminEntry(rows)
		if err != nil {
			return nil, false, err
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if len(entries) > limit {
		return entries[:limit], true, nil
	}
	return entries, false, nil
}

// AdminGrantCredits serializes with task reservations on the same account row.
// Audit and balance commit together. A retry keeps its original request ID.
func (s *Store) AdminGrantCredits(ctx context.Context, actorID, userID string, points int64, reason, requestID string) (AdminGrantResult, error) {
	var result AdminGrantResult
	reason = strings.TrimSpace(reason)
	if !ValidID(actorID) || !ValidID(userID) || !ValidID(requestID) || points < 1 || points > MaxAdminGrantPoints || reason == "" || utf8.RuneCountInString(reason) > MaxGrantReasonLength {
		return result, ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(context.Background())
	var actorEmail string
	// Hold role/status stable until commit, including when an operator demotes an account.
	if err = tx.QueryRow(ctx, `SELECT email FROM users WHERE id=$1 AND role='admin' AND status='active' FOR SHARE`, actorID).Scan(&actorEmail); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return result, ErrAdminRequired
		}
		return result, err
	}
	var status string
	if err = tx.QueryRow(ctx, `SELECT status FROM users WHERE id=$1 FOR SHARE`, userID).Scan(&status); err != nil {
		return result, mapError(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO credit_accounts(user_id) VALUES($1) ON CONFLICT(user_id) DO NOTHING`, userID); err != nil {
		return result, err
	}
	if err = tx.QueryRow(ctx, `SELECT available,reserved FROM credit_accounts WHERE user_id=$1 FOR UPDATE`, userID).Scan(&result.Account.Available, &result.Account.Reserved); err != nil {
		return result, err
	}
	key := "admin-grant:" + strings.ToLower(requestID)
	var previousUser string
	err = tx.QueryRow(ctx, `SELECT user_id::text FROM credit_ledger WHERE idempotency_key=$1`, key).Scan(&previousUser)
	if err == nil {
		entry, readErr := scanAdminEntry(tx.QueryRow(ctx, `SELECT `+adminEntryColumns+` FROM credit_ledger l LEFT JOIN users actor ON actor.id=l.actor_user_id WHERE l.idempotency_key=$1`, key))
		if readErr != nil {
			return result, readErr
		}
		if previousUser != userID || entry.Source != "manual" || entry.ActorID == nil || *entry.ActorID != actorID || entry.AvailableDelta != points || entry.Reason != reason {
			return result, ErrConflict
		}
		result.Entry = entry
		result.Replayed = true
		return result, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	if status != "active" {
		return result, ErrUserDisabled
	}
	if result.Account.Available > 9007199254740991-points {
		return result, ErrInvalid
	}
	if err = tx.QueryRow(ctx, `UPDATE credit_accounts SET available=available+$2,updated_at=now() WHERE user_id=$1 RETURNING available,reserved`, userID, points).Scan(&result.Account.Available, &result.Account.Reserved); err != nil {
		return result, err
	}
	result.Entry = AdminCreditEntry{CreditEntry: CreditEntry{Operation: "grant", AvailableDelta: points, AvailableAfter: result.Account.Available, ReservedAfter: result.Account.Reserved}, Source: "manual", ActorID: &actorID, ActorEmail: &actorEmail, Reason: reason}
	err = tx.QueryRow(ctx, `INSERT INTO credit_ledger(user_id,operation,available_delta,reserved_delta,available_after,reserved_after,idempotency_key,source,actor_user_id,reason)
		VALUES($1,'grant',$2,0,$3,$4,$5,'manual',$6,$7) RETURNING id,created_at`, userID, points, result.Account.Available, result.Account.Reserved, key, actorID, reason).Scan(&result.Entry.ID, &result.Entry.CreatedAt)
	if err != nil {
		return result, mapError(err)
	}
	return result, tx.Commit(ctx)
}

// PromoteAdmin is available only to the server-side initialization command.
// Revoke existing sessions so an old ordinary-user session cannot gain privileges.
func (s *Store) PromoteAdmin(ctx context.Context, id string) error {
	if !ValidID(id) {
		return ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	var role, status string
	if err = tx.QueryRow(ctx, `SELECT role,status FROM users WHERE id=$1 FOR UPDATE`, id).Scan(&role, &status); err != nil {
		return mapError(err)
	}
	if status != "active" {
		return ErrUserDisabled
	}
	if role == "admin" {
		return nil
	}
	if _, err = tx.Exec(ctx, `UPDATE users SET role='admin' WHERE id=$1`, id); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE user_sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
