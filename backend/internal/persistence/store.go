package persistence

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"time"

	"frame-space/backend/internal/storage"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct{ Pool *pgxpool.Pool }

func Open(ctx context.Context, url string) (*Store, error) {
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	config.MaxConns = 6
	config.ConnConfig.ConnectTimeout = 5 * time.Second
	config.ConnConfig.RuntimeParams["statement_timeout"] = "15000"
	config.ConnConfig.RuntimeParams["lock_timeout"] = "5000"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}
	s := &Store{pool}
	if err = pool.Ping(ctx); err == nil {
		err = s.migrate(ctx)
	}
	if err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) Close() { s.Pool.Close() }
func (s *Store) migrate(ctx context.Context) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(718204001)`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	entries, _ := migrations.ReadDir("migrations")
	for _, entry := range entries {
		var exists bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, entry.Name()).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		sql, readErr := migrations.ReadFile("migrations/" + entry.Name())
		if readErr != nil {
			return readErr
		}
		if _, err = tx.Exec(ctx, string(sql)); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES($1)`, entry.Name()); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func mapError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		if pgErr.ConstraintName == "generation_node_active" {
			return ErrBusy
		}
		return ErrConflict
	}
	return err
}
func (s *Store) CreateCanvas(ctx context.Context, id, title, userID string) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO canvases(id,title,owner_user_id) VALUES($1,$2,$3) ON CONFLICT(id) DO NOTHING`, id, title, userID)
	if err != nil {
		return err
	}
	return s.RequireCanvasOwner(ctx, id, userID)
}
func (s *Store) Canvases(ctx context.Context, userID string, limit, offset int) ([]CanvasSummary, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id::text,title,updated_at,snapshot FROM canvases WHERE owner_user_id=$1 ORDER BY updated_at DESC,id DESC LIMIT $2 OFFSET $3`, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []CanvasSummary{}
	for rows.Next() {
		item := CanvasSummary{Preview: []CanvasPreviewNode{}}
		var snapshot Snapshot
		if err := rows.Scan(&item.ID, &item.Title, &item.UpdatedAt, &snapshot); err != nil {
			return nil, err
		}
		item.NodeCount = len(snapshot.Nodes)
		// The library needs only layout, never prompts, asset URLs or task inputs.
		for i, node := range snapshot.Nodes {
			if i == 36 {
				break
			}
			item.Preview = append(item.Preview, CanvasPreviewNode{Position: node.Position, Kind: node.Data.Kind})
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (s *Store) Canvas(ctx context.Context, id string) (Canvas, error) {
	c := Canvas{Assets: []storage.Asset{}, Tasks: []Task{}, Results: []Task{}}
	err := s.Pool.QueryRow(ctx, `SELECT id::text,title,version,snapshot,updated_at FROM canvases WHERE id=$1`, id).Scan(&c.ID, &c.Title, &c.Version, &c.Snapshot, &c.UpdatedAt)
	if err != nil {
		return c, mapError(err)
	}
	rows, err := s.Pool.Query(ctx, `SELECT DISTINCT ON(node_id) metadata FROM assets WHERE canvas_id=$1 ORDER BY node_id,created_at DESC,key DESC`, id)
	if err != nil {
		return c, err
	}
	for rows.Next() {
		var a storage.Asset
		if err = rows.Scan(&a); err != nil {
			rows.Close()
			return c, err
		}
		c.Assets = append(c.Assets, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return c, err
	}
	c.Tasks, err = s.LatestTasks(ctx, id, false)
	if err != nil {
		return c, err
	}
	c.Results, err = s.LatestTasks(ctx, id, true)
	return c, err
}
func (s *Store) SaveCanvas(ctx context.Context, id string, version int64, snapshot Snapshot) (int64, error) {
	var next int64
	err := s.Pool.QueryRow(ctx, `UPDATE canvases SET snapshot=$3,version=version+1,updated_at=now() WHERE id=$1 AND version=$2 RETURNING version`, id, version, snapshot).Scan(&next)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrConflict
	}
	return next, err
}
func (s *Store) Node(ctx context.Context, canvasID, nodeID string) (Node, error) {
	var snapshot Snapshot
	err := s.Pool.QueryRow(ctx, `SELECT snapshot FROM canvases WHERE id=$1`, canvasID).Scan(&snapshot)
	if err != nil {
		return Node{}, mapError(err)
	}
	for _, n := range snapshot.Nodes {
		if n.ID == nodeID {
			return n, nil
		}
	}
	return Node{}, ErrNotFound
}
func (s *Store) RecordAsset(ctx context.Context, a storage.Asset) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO assets(key,canvas_id,node_id,metadata) VALUES($1,$2,$3,$4) ON CONFLICT(key) DO NOTHING`, a.Key, a.CanvasID, a.NodeID, a)
	return err
}
func (s *Store) ValidateReferences(ctx context.Context, input Input) error {
	seen := map[string]bool{}
	for _, key := range input.ReferenceKeys {
		if seen[key] {
			return ErrInvalid
		}
		seen[key] = true
		var a storage.Asset
		if err := s.Pool.QueryRow(ctx, `SELECT metadata FROM assets WHERE key=$1 AND canvas_id=$2 AND node_id<>$3`, key, input.CanvasID, input.NodeID).Scan(&a); err != nil {
			return mapError(err)
		}
		if a.Kind != storage.Image {
			return ErrInvalid
		}
	}
	return nil
}

const taskColumns = `id::text,input,status,result,error,created_at,started_at,finished_at,updated_at,kind,compiled_prompt,bindings,provider_task_id,provider_status,provider_url,polling_error,credit_points,credit_price_version,credit_status`

func scanTask(row pgx.Row) (Task, error) {
	var t Task
	var result, failure []byte
	err := row.Scan(&t.ID, &t.Input, &t.Status, &result, &failure, &t.CreatedAt, &t.StartedAt, &t.FinishedAt, &t.UpdatedAt, &t.Kind, &t.CompiledPrompt, &t.Bindings, &t.ProviderTaskID, &t.ProviderStatus, &t.ProviderURL, &t.PollingError, &t.CreditPoints, &t.CreditPriceVersion, &t.CreditStatus)
	if err != nil {
		return t, mapError(err)
	}
	if len(result) > 0 {
		if err = json.Unmarshal(result, &t.Result); err != nil {
			return t, err
		}
	}
	if len(failure) > 0 {
		err = json.Unmarshal(failure, &t.Error)
	}
	return t, err
}
func (s *Store) Task(ctx context.Context, id string) (Task, error) {
	return scanTask(s.Pool.QueryRow(ctx, `SELECT `+taskColumns+` FROM generation_tasks WHERE id=$1`, id))
}
func (s *Store) CreateTask(ctx context.Context, id string, input Input) (Task, error) {
	return s.createTask(ctx, id, input, "image")
}
func (s *Store) CreateVideoTask(ctx context.Context, id string, input Input) (Task, error) {
	return s.createTask(ctx, id, input, "video")
}
func (s *Store) createTask(ctx context.Context, id string, input Input, kind string) (Task, error) {
	if kind == "video" && NormalizeVideoInput(&input) != nil {
		return Task{}, ErrInvalid
	}
	existing, err := s.Task(ctx, id)
	if err == nil {
		if existing.Kind != kind || !sameInput(existing.Input, input) {
			return Task{}, ErrConflict
		}
		return existing, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Task{}, err
	}
	node, err := s.Node(ctx, input.CanvasID, input.NodeID)
	if err != nil {
		return Task{}, err
	}
	if node.Data.Kind != kind || node.Data.Origin == "upload" {
		return Task{}, ErrInvalid
	}
	bindings, err := s.bindReferences(ctx, input, kind)
	if err != nil {
		return Task{}, err
	}
	compiled, err := CompilePrompt(input, kind, bindings)
	if err != nil {
		return Task{}, err
	}
	if kind == "video" && input.Mode == "text" && compiled == "" {
		return Task{}, ErrInvalid
	}
	t, err := scanTask(s.Pool.QueryRow(ctx, `INSERT INTO generation_tasks(id,canvas_id,node_id,input,status,kind,compiled_prompt,bindings) VALUES($1,$2,$3,$4,'queued',$5,$6,$7) RETURNING `+taskColumns, id, input.CanvasID, input.NodeID, input, kind, compiled, bindings))
	if errors.Is(err, ErrConflict) || errors.Is(err, ErrBusy) {
		existing, e := s.Task(ctx, id)
		if e == nil {
			if existing.Kind == kind && sameInput(existing.Input, input) {
				return existing, nil
			}
			return Task{}, ErrConflict
		}
	}
	return t, err
}
func queryTasks(ctx context.Context, pool *pgxpool.Pool, query string, args ...any) ([]Task, error) {
	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := []Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}
func (s *Store) LatestTasks(ctx context.Context, canvasID string, success bool) ([]Task, error) {
	filter := ""
	if success {
		filter = " AND status='succeeded'"
	}
	return queryTasks(ctx, s.Pool, `SELECT DISTINCT ON(node_id) `+taskColumns+` FROM generation_tasks WHERE canvas_id=$1`+filter+` ORDER BY node_id,created_at DESC,id DESC`, canvasID)
}
func (s *Store) Tasks(ctx context.Context, canvasID string, limit, offset int) ([]Task, error) {
	return queryTasks(ctx, s.Pool, `SELECT `+taskColumns+` FROM generation_tasks WHERE canvas_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3`, canvasID, limit, offset)
}
