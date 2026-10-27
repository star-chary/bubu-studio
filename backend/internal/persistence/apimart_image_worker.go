package persistence

import (
	"context"
	"errors"
	"time"

	"frame-space/backend/internal/apimart"
	"frame-space/backend/internal/ark"
	"frame-space/backend/internal/storage"
)

type APIMartImageGenerator interface {
	CreateImage(context.Context, apimart.ImageRequest) (string, error)
	QueryImage(context.Context, string, string) (apimart.ImageResponse, error)
}

func (s *Store) executeAPIMartImage(ctx context.Context, t Task, generator APIMartImageGenerator, objects ResultStorage) {
	defer func() {
		if recover() != nil {
			s.finish(ctx, t.ID, "interrupted", nil, &ark.APIError{Code: "WORKER_PANIC", Message: "图片任务执行异常，请核查任务，未自动重新生成。"})
		}
	}()
	if generator == nil || objects == nil {
		s.finish(ctx, t.ID, "failed", nil, &ark.APIError{Code: "IMAGE_SERVICE_REQUIRED", Message: "图片生成需要模型和素材保存服务。"})
		return
	}
	if t.Status == "preparing" {
		var urls []string
		if len(t.Input.ReferenceKeys) > 0 {
			scope := storage.Scope{CanvasID: t.Input.CanvasID, NodeID: t.Input.NodeID}
			var err error
			if signer, ok := objects.(interface {
				APIMartImageReferenceURLs(context.Context, storage.Scope, []string) ([]string, error)
			}); ok {
				urls, err = signer.APIMartImageReferenceURLs(ctx, scope, t.Input.ReferenceKeys)
			} else {
				err = &storage.Error{Status: 503, Code: "REFERENCE_SIGN_FAILED", Message: "未配置异步参考图片签名服务。"}
			}
			if err != nil {
				s.finish(ctx, t.ID, "failed", nil, videoFailure(err))
				return
			}
		}
		if ctx.Err() != nil {
			return
		}
		submittedAt := time.Now()
		if !s.retryWrite(ctx, func(c context.Context) error {
			_, err := s.Pool.Exec(c, `UPDATE generation_tasks SET status='submitting',started_at=$2,updated_at=now() WHERE id=$1 AND status='preparing'`, t.ID, submittedAt)
			return err
		}) {
			return
		}
		t.StartedAt = &submittedAt
		id, err := generator.CreateImage(ctx, apimart.ImageRequest{TaskID: t.ID, Prompt: t.CompiledPrompt, Model: t.Input.Model, Parameters: t.Input.Parameters, Images: urls})
		if err != nil {
			failure := videoFailure(err)
			status := "failed"
			if failure.Code == "SUBMISSION_UNKNOWN" {
				status = "interrupted"
			}
			s.finish(ctx, t.ID, status, nil, failure)
			return
		}
		if !s.retryWrite(ctx, func(c context.Context) error {
			_, err := s.Pool.Exec(c, `UPDATE generation_tasks SET status='running',provider_task_id=$2,provider_status='queued',updated_at=now() WHERE id=$1 AND status='submitting'`, t.ID, id)
			return err
		}) {
			return
		}
		t.ProviderTaskID, t.Status = id, "running"
	}
	if t.Status != "running" {
		return
	}
	delay := 5 * time.Second
	for ctx.Err() == nil {
		if videoQueryExpired(t) {
			s.finish(ctx, t.ID, "interrupted", nil, &ark.APIError{Code: "PROVIDER_RESULT_UNAVAILABLE", Message: "任务已超过查询窗口，请在中转站核查，未重新生成。"})
			return
		}
		out, err := generator.QueryImage(ctx, t.ProviderTaskID, t.Input.Model)
		if ctx.Err() != nil {
			return
		}
		var pollingError *ark.APIError
		if err != nil {
			pollingError = &ark.APIError{Code: "IMAGE_POLL_RETRY", Message: "暂时无法查询图片进度，正在继续查询同一任务。"}
			delay = min(delay*2, 60*time.Second)
			var pe *ark.PollError
			if errors.As(err, &pe) && pe.RetryAfter > delay {
				delay = min(pe.RetryAfter, 10*time.Minute)
			}
		} else {
			delay = 5 * time.Second
			switch out.Status {
			case "succeeded":
				result := &Result{ImageResult: ark.ImageResult{URL: out.URL, Model: t.Input.Model}}
				if !s.retryWrite(ctx, func(c context.Context) error {
					_, err := s.Pool.Exec(c, `UPDATE generation_tasks SET status='saving',provider_status='succeeded',provider_url=$2,result=$3,polling_error=NULL,updated_at=now() WHERE id=$1 AND status='running'`, t.ID, out.URL, result)
					return err
				}) {
					return
				}
				return
			case "failed":
				s.finish(ctx, t.ID, "failed", nil, out.Failure)
				return
			case "queued", "running":
			default:
				pollingError = &ark.APIError{Code: "UNKNOWN_PROVIDER_STATUS", Message: "中转站返回未知进度，继续查询原任务。"}
			}
		}
		if !s.retryWrite(ctx, func(c context.Context) error {
			_, err := s.Pool.Exec(c, `UPDATE generation_tasks SET provider_status=CASE WHEN $2='' THEN provider_status ELSE $2 END,polling_error=$3,updated_at=now() WHERE id=$1 AND status='running'`, t.ID, out.Status, pollingError)
			return err
		}) {
			return
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
