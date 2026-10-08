package persistence

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"frame-space/backend/internal/ark"
	"frame-space/backend/internal/storage"
	"github.com/jackc/pgx/v5"
)

const CreditPriceVersion = "test-2026-10-07-v2"
const SignupTestCredits int64 = 200

const signupCreditKeyPrefix = "signup-welcome-v1:"

var (
	ErrCreditsInsufficient = errors.New("insufficient test credits")
	ErrCreditQuoteChanged  = errors.New("credit quote changed")
)

type CreditAccount struct {
	Available int64 `json:"available"`
	Reserved  int64 `json:"reserved"`
}

type CreditEntry struct {
	ID             int64     `json:"id"`
	Operation      string    `json:"operation"`
	AvailableDelta int64     `json:"availableDelta"`
	ReservedDelta  int64     `json:"reservedDelta"`
	AvailableAfter int64     `json:"availableAfter"`
	ReservedAfter  int64     `json:"reservedAfter"`
	TaskID         *string   `json:"taskId,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
}

type CreditQuote struct {
	Points       int64  `json:"points"`
	PriceVersion string `json:"priceVersion"`
}

// grantSignupCredits is called only after a new users row was inserted.
// Its caller owns the transaction so a failed grant cannot leave an account
// registered without its promised starting balance.
func grantSignupCredits(ctx context.Context, tx pgx.Tx, userID string) error {
	var account CreditAccount
	if err := tx.QueryRow(ctx, `INSERT INTO credit_accounts(user_id,available) VALUES($1,$2)
		RETURNING available,reserved`, userID, SignupTestCredits).Scan(&account.Available, &account.Reserved); err != nil {
		return mapError(err)
	}
	_, err := tx.Exec(ctx, `INSERT INTO credit_ledger(user_id,operation,available_delta,reserved_delta,available_after,reserved_after,idempotency_key)
		VALUES($1,'grant',$2,0,$3,$4,$5)`, userID, SignupTestCredits, account.Available, account.Reserved, signupCreditKeyPrefix+userID)
	return mapError(err)
}

func (s *Store) Credits(ctx context.Context, userID string) (CreditAccount, error) {
	var account CreditAccount
	err := s.Pool.QueryRow(ctx, `SELECT available,reserved FROM credit_accounts WHERE user_id=$1`, userID).Scan(&account.Available, &account.Reserved)
	if errors.Is(err, pgx.ErrNoRows) {
		return account, nil
	}
	return account, err
}

func (s *Store) CreditEntries(ctx context.Context, userID string, limit, offset int) ([]CreditEntry, bool, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id,operation,available_delta,reserved_delta,available_after,reserved_after,task_id::text,created_at
		FROM credit_ledger WHERE user_id=$1 ORDER BY id DESC LIMIT $2 OFFSET $3`, userID, limit+1, offset)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	entries := make([]CreditEntry, 0, limit)
	for rows.Next() {
		var entry CreditEntry
		if err := rows.Scan(&entry.ID, &entry.Operation, &entry.AvailableDelta, &entry.ReservedDelta, &entry.AvailableAfter, &entry.ReservedAfter, &entry.TaskID, &entry.CreatedAt); err != nil {
			return nil, false, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if len(entries) > limit {
		return entries[:limit], true, nil
	}
	return entries, false, nil
}

// GrantCredits is an operator-only, idempotent transaction. No public route calls it.
func (s *Store) GrantCredits(ctx context.Context, userID string, points int64, key string) error {
	if !ValidID(userID) || points <= 0 || points > 1000000 || strings.TrimSpace(key) == "" || len(key) > 200 {
		return ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	var existingUser string
	var existingPoints int64
	err = tx.QueryRow(ctx, `SELECT user_id::text,available_delta FROM credit_ledger WHERE idempotency_key=$1`, key).Scan(&existingUser, &existingPoints)
	if err == nil {
		if existingUser == userID && existingPoints == points {
			return nil
		}
		return ErrConflict
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO credit_accounts(user_id) VALUES($1) ON CONFLICT(user_id) DO NOTHING`, userID); err != nil {
		return err
	}
	var account CreditAccount
	err = tx.QueryRow(ctx, `UPDATE credit_accounts SET available=available+$2,updated_at=now() WHERE user_id=$1
		RETURNING available,reserved`, userID, points).Scan(&account.Available, &account.Reserved)
	if err != nil {
		return mapError(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO credit_ledger(user_id,operation,available_delta,reserved_delta,available_after,reserved_after,idempotency_key)
		VALUES($1,'grant',$2,0,$3,$4,$5)`, userID, points, account.Available, account.Reserved, key); err != nil {
		return mapError(err)
	}
	return tx.Commit(ctx)
}

func (s *Store) prepareCreditTask(ctx context.Context, userID, kind string, input Input) ([]Reference, string, error) {
	if err := s.RequireCanvasOwner(ctx, input.CanvasID, userID); err != nil {
		return nil, "", err
	}
	node, err := s.Node(ctx, input.CanvasID, input.NodeID)
	if err != nil {
		return nil, "", err
	}
	if node.Data.Kind != kind || node.Data.Origin == "upload" {
		return nil, "", ErrInvalid
	}
	bindings, err := s.bindReferences(ctx, input, kind)
	if err != nil {
		return nil, "", err
	}
	compiled, err := CompilePrompt(input, kind, bindings)
	if err == nil && kind == "video" && input.Mode == "text" && compiled == "" {
		return nil, "", ErrInvalid
	}
	return bindings, compiled, err
}

func (s *Store) QuoteCredits(ctx context.Context, userID, kind string, input Input) (CreditQuote, error) {
	if kind == "image" {
		model, err := ark.ResolveImageModel(input.Model)
		if err != nil || len(input.ReferenceKeys) > ark.MaxReferenceImages(model) {
			return CreditQuote{}, ErrInvalid
		}
		input.Model = model
	} else if kind == "video" {
		if NormalizeVideoInput(&input) != nil {
			return CreditQuote{}, ErrInvalid
		}
	} else {
		return CreditQuote{}, ErrInvalid
	}
	bindings, _, err := s.prepareCreditTask(ctx, userID, kind, input)
	if err != nil {
		return CreditQuote{}, err
	}
	var yuan float64
	if kind == "image" {
		yuan = 0.22
		if input.Model == ark.ProImageModel {
			yuan = 0.60 + 0.02*float64(max(0, len(bindings)-1))
		}
	} else {
		yuan, err = s.videoQuoteYuan(ctx, input, bindings)
		if err != nil {
			return CreditQuote{}, err
		}
	}
	// Convert estimated list-price RMB to test points with the established
	// 2 * ceil(12 * RMB) rule. The price version changes with model rates.
	points := int64(2 * math.Ceil(12*yuan-1e-9))
	if points <= 0 || points > 1000000 {
		return CreditQuote{}, ErrInvalid
	}
	return CreditQuote{Points: points, PriceVersion: CreditPriceVersion}, nil
}

func (s *Store) videoQuoteYuan(ctx context.Context, input Input, bindings []Reference) (float64, error) {
	spec, ok := ark.VideoSpec(input.Model)
	if !ok {
		return 0, ErrInvalid
	}
	dims := map[string]map[string][2]int{
		"480p": {"16:9": {854, 480}, "4:3": {752, 560}, "1:1": {640, 640}, "3:4": {560, 752}, "9:16": {480, 854}, "21:9": {992, 432}},
		"720p": {"16:9": {1280, 720}, "4:3": {1112, 834}, "1:1": {960, 960}, "3:4": {834, 1112}, "9:16": {720, 1280}, "21:9": {1470, 630}},
	}
	if input.Model != ark.VideoModel {
		dims["480p"]["16:9"] = [2]int{864, 496}
		dims["480p"]["9:16"] = [2]int{496, 864}
	}
	area := 0
	if input.Ratio == "adaptive" {
		for _, size := range dims[input.Resolution] {
			area = max(area, size[0]*size[1])
		}
	} else {
		size := dims[input.Resolution][input.Ratio]
		area = size[0] * size[1]
	}
	if area <= 0 || input.Duration < 4 || input.Duration > spec.MaxDuration {
		return 0, ErrInvalid
	}
	videoSeconds := 0.0
	audioSeconds := 0.0
	videoCount := 0
	for _, ref := range bindings {
		if ref.Kind != "video" && ref.Kind != "audio" {
			continue
		}
		var asset storage.Asset
		if err := s.Pool.QueryRow(ctx, `SELECT metadata FROM assets WHERE key=$1 AND canvas_id=$2`, ref.AssetKey, input.CanvasID).Scan(&asset); err != nil {
			return 0, mapError(err)
		}
		if asset.Media == nil || !storage.CheckVideoReference(asset).Eligible || math.IsNaN(asset.Media.DurationSeconds) || math.IsInf(asset.Media.DurationSeconds, 0) || asset.Media.DurationSeconds > spec.MaxReferenceSeconds {
			return 0, ErrInvalid
		}
		if ref.Kind == "video" {
			videoSeconds += asset.Media.DurationSeconds
			videoCount++
		} else {
			audioSeconds += asset.Media.DurationSeconds
		}
	}
	if videoSeconds > spec.MaxReferenceSeconds || audioSeconds > spec.MaxReferenceSeconds || math.IsNaN(videoSeconds) || math.IsInf(videoSeconds, 0) || math.IsNaN(audioSeconds) || math.IsInf(audioSeconds, 0) {
		return 0, ErrInvalid
	}
	rates := map[string][2]float64{
		ark.VideoModel:       {70, 42},
		ark.VideoModel20:     {46, 28},
		ark.VideoModel20Fast: {37, 22},
		ark.VideoModel20Mini: {23, 14},
	}
	rate := rates[input.Model]
	if videoCount == 0 {
		return float64(input.Duration*area) * 24 / 1024 * rate[0] / 1000000, nil
	}
	// Ark video-reference examples imply a minimum 4 seconds of input-video
	// tokens. Round measured media up to whole seconds for the test-point quote.
	inputSeconds := math.Max(4, math.Ceil(videoSeconds))
	return (float64(input.Duration) + inputSeconds) * float64(area) * 24 / 1024 * rate[1] / 1000000, nil
}

func (s *Store) CreateBilledTask(ctx context.Context, userID, id, kind string, input Input, acceptedPoints int64, version string) (Task, error) {
	if !ValidID(id) {
		return Task{}, ErrInvalid
	}
	if kind == "video" && NormalizeVideoInput(&input) != nil {
		return Task{}, ErrInvalid
	}
	if existing, err := s.Task(ctx, id); err == nil {
		if s.RequireTaskOwner(ctx, id, userID) != nil {
			return Task{}, ErrNotFound
		}
		if existing.Kind != kind || !sameInput(existing.Input, input) {
			return Task{}, ErrConflict
		}
		return existing, nil
	} else if !errors.Is(err, ErrNotFound) {
		return Task{}, err
	}
	quote, err := s.QuoteCredits(ctx, userID, kind, input)
	if err != nil {
		return Task{}, err
	}
	if acceptedPoints != quote.Points || version != quote.PriceVersion {
		return Task{}, ErrCreditQuoteChanged
	}
	bindings, compiled, err := s.prepareCreditTask(ctx, userID, kind, input)
	if err != nil {
		return Task{}, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `INSERT INTO credit_accounts(user_id) VALUES($1) ON CONFLICT(user_id) DO NOTHING`, userID); err != nil {
		return Task{}, err
	}
	var account CreditAccount
	err = tx.QueryRow(ctx, `UPDATE credit_accounts SET available=available-$2,reserved=reserved+$2,updated_at=now()
		WHERE user_id=$1 AND available >= $2 RETURNING available,reserved`, userID, quote.Points).Scan(&account.Available, &account.Reserved)
	if errors.Is(err, pgx.ErrNoRows) {
		// A concurrent retry of the same task can wait for the first reservation
		// to commit, then see the reduced balance. Return that existing task.
		_ = tx.Rollback(ctx)
		if existing, lookup := s.Task(ctx, id); lookup == nil && s.RequireTaskOwner(ctx, id, userID) == nil && existing.Kind == kind && sameInput(existing.Input, input) {
			return existing, nil
		}
		return Task{}, ErrCreditsInsufficient
	}
	if err != nil {
		return Task{}, err
	}
	var task Task
	task, err = scanTask(tx.QueryRow(ctx, `INSERT INTO generation_tasks(id,canvas_id,node_id,input,status,kind,compiled_prompt,bindings,credit_points,credit_price_version,credit_status)
		VALUES($1,$2,$3,$4,'queued',$5,$6,$7,$8,$9,'reserved') RETURNING `+taskColumns,
		id, input.CanvasID, input.NodeID, input, kind, compiled, bindings, quote.Points, quote.PriceVersion))
	if err != nil {
		_ = tx.Rollback(ctx)
		if errors.Is(err, ErrConflict) || errors.Is(err, ErrBusy) {
			existing, lookup := s.Task(ctx, id)
			if lookup == nil && s.RequireTaskOwner(ctx, id, userID) == nil && existing.Kind == kind && sameInput(existing.Input, input) {
				return existing, nil
			}
		}
		return Task{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO credit_ledger(user_id,task_id,operation,available_delta,reserved_delta,available_after,reserved_after,idempotency_key)
		VALUES($1,$2,'reserve',$3,$4,$5,$6,$7)`, userID, id, -quote.Points, quote.Points, account.Available, account.Reserved, fmt.Sprintf("task:%s:reserve", id)); err != nil {
		return Task{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Task{}, err
	}
	return task, nil
}

func creditCompletion(kind, oldStatus, status string, result *Result, failure *ark.APIError) string {
	if status == "succeeded" {
		if result != nil && result.Asset != nil {
			return "settle"
		}
		// The provider succeeded, so it may already have charged the shared
		// account. Keep the reservation for review when durable storage failed.
		return "review"
	}
	if status == "storage_failed" {
		return "review"
	}
	if oldStatus == "queued" || oldStatus == "preparing" {
		return "release"
	}
	if status == "failed" && failure != nil {
		if kind == "video" {
			if strings.HasPrefix(failure.Code, "PROVIDER_") || failure.Code == "VIDEO_REJECTED" || failure.Code == "MODEL_AUTH_FAILED" || failure.Code == "MODEL_LIMITED" {
				return "release"
			}
		} else {
			switch failure.Code {
			case "MODEL_AUTH_FAILED", "MODEL_NOT_AVAILABLE", "MODEL_LIMITED", "GENERATION_REJECTED":
				return "release"
			}
		}
	}
	return "review"
}

// completeTaskCredits runs in the same transaction as the task's final state.
// A terminal task cannot settle/release twice, even if the worker retries a write.
func completeTaskCredits(ctx context.Context, tx pgx.Tx, id, userID, creditStatus, action string, points int64) error {
	if creditStatus == "legacy" || creditStatus == "settled" || creditStatus == "released" {
		return nil
	}
	if action == "review" {
		_, err := tx.Exec(ctx, `UPDATE generation_tasks SET credit_status='review' WHERE id=$1 AND credit_status IN ('reserved','review')`, id)
		return err
	}
	var account CreditAccount
	var availableDelta, reservedDelta int64
	if action == "settle" {
		reservedDelta = -points
	} else {
		availableDelta, reservedDelta = points, -points
	}
	err := tx.QueryRow(ctx, `UPDATE credit_accounts SET available=available+$2,reserved=reserved+$3,updated_at=now()
		WHERE user_id=$1 AND reserved >= $4 RETURNING available,reserved`, userID, availableDelta, reservedDelta, points).Scan(&account.Available, &account.Reserved)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO credit_ledger(user_id,task_id,operation,available_delta,reserved_delta,available_after,reserved_after,idempotency_key)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, userID, id, action, availableDelta, reservedDelta, account.Available, account.Reserved, fmt.Sprintf("task:%s:%s", id, action)); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE generation_tasks SET credit_status=$2 WHERE id=$1`, id, map[string]string{"settle": "settled", "release": "released"}[action])
	return err
}
