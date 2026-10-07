package persistence

import (
	"context"
	"errors"
	"fmt"
	"time"

	"frame-space/backend/internal/ark"
	"frame-space/backend/internal/storage"
)

type Generator interface {
	Generate(context.Context, string, string, ...string) (ark.ImageResult, error)
}
type ResultStorage interface {
	ReferenceURLs(context.Context, storage.Scope, []string) ([]string, error)
	SaveGenerated(context.Context, string, string, storage.Scope) (storage.Asset, error)
}

// RunWorker owns a session advisory lock. A second backend cannot interrupt the
// first one's jobs on startup. Browser request cancellation never owns this context.
func (s *Store) RunWorker(parent context.Context, generator Generator, objects ResultStorage, ready chan<- error, configs ...WorkerConfig) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	config := DefaultWorkerConfig()
	if len(configs) > 0 {
		config = configs[0]
	}
	if err := config.validate(); err != nil {
		ready <- err
		return err
	}
	lock, err := s.Pool.Acquire(ctx)
	if err != nil {
		ready <- err
		return err
	}
	// Close the physical connection instead of returning a session lock to the pool.
	defer func() {
		conn := lock.Hijack()
		closeCtx, c := context.WithTimeout(context.Background(), 3*time.Second)
		defer c()
		_ = conn.Close(closeCtx)
	}()
	var acquired bool
	err = lock.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended(current_schema() || ':frame-space-worker', 0))`).Scan(&acquired)
	if err == nil && !acquired {
		err = fmt.Errorf("已有后端持有任务执行锁")
	}
	if err != nil {
		ready <- err
		return err
	}
	_, err = s.Pool.Exec(ctx, `UPDATE generation_tasks SET status='interrupted',credit_status=CASE WHEN credit_status='reserved' THEN 'review' ELSE credit_status END,error='{"code":"WORKER_INTERRUPTED","message":"后端在生成期间中断，供应商可能已计费；请核查后手动决定是否重新生成。"}',finished_at=now(),updated_at=now() WHERE kind='image' AND status='running'`)
	if err != nil {
		ready <- err
		return err
	}
	_, err = s.Pool.Exec(ctx, `UPDATE generation_tasks SET status='interrupted',credit_status=CASE WHEN credit_status='reserved' THEN 'review' ELSE credit_status END,error='{"code":"SUBMISSION_UNKNOWN","message":"视频提交期间服务中断，可能已计费；请核查任务，未自动重新提交。"}',finished_at=now(),updated_at=now() WHERE kind='video' AND status='submitting'`)
	if err != nil {
		ready <- err
		return err
	}
	// Preparation never calls a paid model; it is safe to queue again. All old
	// claims belong to the previous lock owner, which has now stopped.
	_, err = s.Pool.Exec(ctx, `UPDATE generation_tasks SET claimed_at=NULL,
		status=CASE WHEN status='preparing' THEN 'queued' ELSE status END,
		updated_at=now() WHERE claimed_at IS NOT NULL OR status='preparing'`)
	if err != nil {
		ready <- err
		return err
	}
	// Losing the lock connection cancels in-flight model work and stops the worker.
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pingCtx, c := context.WithTimeout(ctx, 3*time.Second)
				err := lock.Conn().Ping(pingCtx)
				c()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()
	defer func() { cancel(); <-heartbeatDone }()
	ready <- nil
	return s.dispatch(ctx, cancel, generator, objects, config)
}

// Database writes may retry, model calls never do. Keep a result in memory until
// persisted, then record it as 'saving' before performing object-storage work.
func (s *Store) retryWrite(ctx context.Context, write func(context.Context) error) bool {
	for ctx.Err() == nil {
		attempt, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := write(attempt)
		cancel()
		if err == nil {
			return true
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
	return false
}
func (s *Store) execute(ctx context.Context, t Task, generator Generator, objects ResultStorage) {
	defer func() {
		if recover() != nil {
			s.finish(ctx, t.ID, "interrupted", nil, &ark.APIError{Code: "WORKER_PANIC", Message: "任务执行异常，未自动重新生成，请核查后重试。"})
		}
	}()
	scope := storage.Scope{CanvasID: t.Input.CanvasID, NodeID: t.Input.NodeID}
	result := t.Result
	if t.Status != "saving" {
		jobCtx, cancel := context.WithTimeout(ctx, 215*time.Second)
		var urls []string
		var err error
		if len(t.Input.ReferenceKeys) > 0 {
			if objects == nil {
				err = &ark.APIError{Code: "REFERENCE_STORAGE_REQUIRED", Message: "参考图片保存服务未启用。"}
			} else {
				urls, err = objects.ReferenceURLs(jobCtx, scope, t.Input.ReferenceKeys)
			}
		}
		var generated ark.ImageResult
		if err == nil {
			if !s.retryWrite(ctx, func(c context.Context) error {
				_, err := s.Pool.Exec(c, `UPDATE generation_tasks SET status='running',updated_at=now() WHERE id=$1 AND status='preparing'`, t.ID)
				return err
			}) {
				cancel()
				return
			}
			prompt := t.CompiledPrompt
			if prompt == "" {
				prompt = t.Input.Prompt
			}
			generated, err = generator.Generate(jobCtx, prompt, t.Input.Model, urls...)
		}
		cancel()
		if err != nil {
			failure := &ark.APIError{Code: "GENERATION_FAILED", Message: "图片生成失败，未自动重试。"}
			var apiError *ark.APIError
			var storageError *storage.Error
			if errors.As(err, &apiError) {
				failure = apiError
			} else if errors.As(err, &storageError) {
				failure = &ark.APIError{Code: storageError.Code, Message: storageError.Message}
			}
			s.finish(ctx, t.ID, "failed", nil, failure)
			return
		}
		result = &Result{ImageResult: generated}
		if !s.retryWrite(ctx, func(c context.Context) error {
			_, err := s.Pool.Exec(c, `UPDATE generation_tasks SET status='saving',result=$2,updated_at=now() WHERE id=$1 AND status='running'`, t.ID, result)
			return err
		}) {
			return
		}
		return // Result transfer is claimed by a separate saving slot.
	}
	if result == nil {
		s.finish(ctx, t.ID, "interrupted", nil, &ark.APIError{Code: "RESULT_MISSING", Message: "任务结果缺失，请核查后重试。"})
		return
	}
	if objects != nil && result.Asset == nil {
		saveCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
		asset, err := objects.SaveGenerated(saveCtx, result.URL, result.Model, scope)
		cancel()
		if err != nil {
			result.StorageError = "图片已生成，但云端保存失败，当前展示临时图片，请及时下载。生成按钮会重新生成图片，不是重试保存。"
		} else {
			result.Asset = &asset
			result.URL = asset.URL
			result.StorageError = ""
		}
	} else if objects == nil {
		result.StorageError = "素材保存服务未启用，结果为临时地址，过期后无法恢复图片。"
	}
	s.finish(ctx, t.ID, "succeeded", result, nil)
}
func (s *Store) finish(ctx context.Context, id, status string, result *Result, failure *ark.APIError) {
	s.retryWrite(ctx, func(c context.Context) error {
		tx, err := s.Pool.Begin(c)
		if err != nil {
			return err
		}
		defer tx.Rollback(context.Background())
		var oldStatus, kind, creditStatus, userID string
		var points int64
		if err = tx.QueryRow(c, `SELECT t.status,t.kind,t.credit_status,t.credit_points,cv.owner_user_id::text
			FROM generation_tasks t JOIN canvases cv ON cv.id=t.canvas_id WHERE t.id=$1 FOR UPDATE OF t`, id).Scan(&oldStatus, &kind, &creditStatus, &points, &userID); err != nil {
			return err
		}
		if oldStatus != "queued" && oldStatus != "preparing" && oldStatus != "submitting" && oldStatus != "running" && oldStatus != "saving" {
			return nil
		}
		if result != nil && result.Asset != nil {
			a := result.Asset
			if _, err = tx.Exec(c, `INSERT INTO assets(key,canvas_id,node_id,metadata) VALUES($1,$2,$3,$4) ON CONFLICT(key) DO NOTHING`, a.Key, a.CanvasID, a.NodeID, a); err != nil {
				return err
			}
		}
		if _, err = tx.Exec(c, `UPDATE generation_tasks SET status=$2,result=COALESCE($3,result),error=$4,finished_at=now(),updated_at=now() WHERE id=$1`, id, status, result, failure); err != nil {
			return err
		}
		if err = completeTaskCredits(c, tx, id, userID, creditStatus, creditCompletion(kind, oldStatus, status, result, failure), points); err != nil {
			return err
		}
		return tx.Commit(c)
	})
}
