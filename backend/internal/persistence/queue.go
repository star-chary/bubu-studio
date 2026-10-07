package persistence

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"
)

// The video limit is shared by all users, not multiplied by user or canvas.
// Images and result transfers have independent, conservative limits.
type WorkerConfig struct {
	Videos int
	Images int
	Saves  int
}

func DefaultWorkerConfig() WorkerConfig { return WorkerConfig{Videos: 3, Images: 1, Saves: 2} }

func (c WorkerConfig) validate() error {
	if c.Videos < 1 || c.Videos > 3 || c.Images < 1 || c.Images > 16 || c.Saves < 1 || c.Saves > 4 {
		return fmt.Errorf("任务并发配置无效：视频 1–3，图片 1–16，转存 1–4")
	}
	return nil
}

func WorkerConfigFromEnv() (WorkerConfig, error) {
	c := DefaultWorkerConfig()
	for name, target := range map[string]*int{
		"VIDEO_GENERATION_CONCURRENCY": &c.Videos,
		"IMAGE_GENERATION_CONCURRENCY": &c.Images,
		"ASSET_SAVE_CONCURRENCY":       &c.Saves,
	} {
		if raw := os.Getenv(name); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil {
				return c, fmt.Errorf("%s 必须为整数", name)
			}
			*target = value
		}
	}
	return c, c.validate()
}

// The CTE locks one eligible row and claims it in the same statement/transaction.
// Other slots skip that row, even while media preparation or polling is slow.
func (s *Store) claimTask(ctx context.Context, lane string) (Task, error) {
	return scanTask(s.Pool.QueryRow(ctx, `WITH next_task AS (
		SELECT id FROM generation_tasks
		WHERE claimed_at IS NULL AND (
			($1='saving' AND status='saving') OR
			($1='video' AND kind='video' AND status IN ('queued','running')) OR
			($1='image' AND kind='image' AND status='queued'))
		ORDER BY CASE WHEN status='running' THEN 0 ELSE 1 END,created_at,id
		FOR UPDATE SKIP LOCKED LIMIT 1
	) UPDATE generation_tasks SET claimed_at=now(),updated_at=now(),
		started_at=CASE WHEN status='queued' THEN COALESCE(started_at,now()) ELSE started_at END,
		status=CASE WHEN status='queued' THEN 'preparing' ELSE status END
	WHERE id=(SELECT id FROM next_task) RETURNING `+taskColumns, lane))
}

func (s *Store) dispatch(ctx context.Context, cancel context.CancelFunc, generator Generator, objects ResultStorage, config WorkerConfig) error {
	var workers sync.WaitGroup
	failures := make(chan error, 1)
	for lane, count := range map[string]int{"video": config.Videos, "image": config.Images, "saving": config.Saves} {
		for range count {
			workers.Add(1)
			go func(lane string) {
				defer workers.Done()
				if err := s.runSlot(ctx, lane, generator, objects); err != nil && ctx.Err() == nil {
					select {
					case failures <- err:
					default:
					}
					cancel()
				}
			}(lane)
		}
	}
	// Do not release the singleton lock until every slot has stopped. A replacement
	// backend must never reset claims while this backend is still making requests.
	workers.Wait()
	select {
	case err := <-failures:
		return err
	default:
		return ctx.Err()
	}
}

func (s *Store) runSlot(ctx context.Context, lane string, generator Generator, objects ResultStorage) error {
	for ctx.Err() == nil {
		task, err := s.claimTask(ctx, lane)
		if errors.Is(err, ErrNotFound) {
			timer := time.NewTimer(200 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			continue
		}
		if err != nil {
			return err
		}
		if task.Kind == "video" {
			videoGenerator, _ := generator.(VideoGenerator)
			videoStorage, _ := objects.(VideoStorage)
			s.executeVideo(ctx, task, videoGenerator, videoStorage)
		} else {
			s.execute(ctx, task, generator, objects)
		}
		// State writes are complete before releasing ownership. A saving slot can
		// now take the result while this generation slot starts the next task.
		if !s.retryWrite(ctx, func(c context.Context) error {
			_, err := s.Pool.Exec(c, `UPDATE generation_tasks SET claimed_at=NULL WHERE id=$1`, task.ID)
			return err
		}) {
			return ctx.Err()
		}
	}
	return ctx.Err()
}
