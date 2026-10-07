package persistence

import (
	"context"
	"errors"
	"strings"
	"time"

	"frame-space/backend/internal/ark"
	"frame-space/backend/internal/storage"
)

type VideoGenerator interface {
	CreateVideo(context.Context, ark.VideoRequest) (string, error)
	QueryVideo(context.Context, string) (ark.VideoResponse, error)
}
type VideoStorage interface {
	VideoReferenceURLsWithLimits(context.Context, storage.Scope, []string, storage.VideoReferenceLimits) ([]string, error)
	SaveGeneratedVideo(context.Context, string, string, storage.Scope) (storage.Asset, error)
}

func videoFailure(err error) *ark.APIError {
	var a *ark.APIError
	if errors.As(err, &a) {
		return a
	}
	var s *storage.Error
	if errors.As(err, &s) {
		return &ark.APIError{Code: s.Code, Message: s.Message}
	}
	return &ark.APIError{Code: "VIDEO_FAILED", Message: "视频任务未能完成，请核查后重试。"}
}
func videoQueryExpired(t Task) bool {
	start := t.CreatedAt // Compatibility with old tasks without a start timestamp.
	if t.StartedAt != nil {
		start = *t.StartedAt
	}
	return time.Since(start) > 48*time.Hour
}

func (s *Store) executeVideo(ctx context.Context, t Task, generator VideoGenerator, objects VideoStorage) {
	defer func() {
		if recover() != nil {
			s.finish(ctx, t.ID, "interrupted", nil, &ark.APIError{Code: "WORKER_PANIC", Message: "视频执行异常，请核查任务，未自动重新生成。"})
		}
	}()
	if generator == nil || objects == nil {
		s.finish(ctx, t.ID, "failed", nil, &ark.APIError{Code: "VIDEO_SERVICE_REQUIRED", Message: "视频生成需要模型、数据库与素材保存服务。"})
		return
	}
	if t.Input.Model == "" {
		t.Input.Model = ark.VideoModel
	}
	if t.Input.Mode == "" {
		t.Input.Mode = "reference"
	}
	scope := storage.Scope{CanvasID: t.Input.CanvasID, NodeID: t.Input.NodeID}
	if t.Status == "preparing" {
		keys := make([]string, 0, len(t.Bindings))
		for _, r := range t.Bindings {
			keys = append(keys, r.AssetKey)
		}
		urls := []string{}
		var err error
		if len(keys) > 0 {
			spec, ok := ark.VideoSpec(t.Input.Model)
			if !ok {
				s.finish(ctx, t.ID, "failed", nil, &ark.APIError{Code: "INVALID_VIDEO_INPUT", Message: "视频模型无效。"})
				return
			}
			limits := storage.VideoReferenceLimits{MaxImages: spec.MaxImages, MaxVideos: spec.MaxVideos, MaxAudios: spec.MaxAudios, MaxMediaSeconds: spec.MaxReferenceSeconds, AllowAudioOnly: t.Input.Model == ark.VideoModel}
			preflight, cancel := context.WithTimeout(ctx, 5*time.Minute)
			urls, err = objects.VideoReferenceURLsWithLimits(preflight, scope, keys, limits)
			cancel()
		} else if t.Input.Mode != "text" {
			s.finish(ctx, t.ID, "failed", nil, &ark.APIError{Code: "INVALID_VIDEO_INPUT", Message: "参考模式缺少参考素材。"})
			return
		}
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			s.finish(ctx, t.ID, "failed", nil, videoFailure(err))
			return
		}
		refs := make([]ark.VideoReference, 0, len(urls))
		for i, u := range urls {
			refs = append(refs, ark.VideoReference{Kind: t.Bindings[i].Kind, URL: u})
		}
		submittedAt := time.Now()
		if !s.retryWrite(ctx, func(c context.Context) error {
			_, err := s.Pool.Exec(c, `UPDATE generation_tasks SET status='submitting',started_at=$2,updated_at=now() WHERE id=$1 AND status='preparing'`, t.ID, submittedAt)
			return err
		}) {
			return
		}
		t.StartedAt = &submittedAt
		id, err := generator.CreateVideo(ctx, ark.VideoRequest{Prompt: t.CompiledPrompt, Model: t.Input.Model, Mode: t.Input.Mode, Resolution: t.Input.Resolution, Ratio: t.Input.Ratio, Duration: t.Input.Duration, GenerateAudio: t.Input.GenerateAudio == nil || *t.Input.GenerateAudio, References: refs})
		if err != nil {
			failure := videoFailure(err)
			status := "failed"
			if failure.Code == "SUBMISSION_UNKNOWN" {
				status = "interrupted"
			}
			s.finish(ctx, t.ID, status, nil, failure)
			return
		}
		// Retain the provider ID until its durable write succeeds; never re-POST.
		if !s.retryWrite(ctx, func(c context.Context) error {
			_, err := s.Pool.Exec(c, `UPDATE generation_tasks SET status='running',provider_task_id=$2,provider_status='queued',updated_at=now() WHERE id=$1 AND status='submitting'`, t.ID, id)
			return err
		}) {
			return
		}
		t.ProviderTaskID = id
		t.Status = "running"
	}
	if t.Status == "running" {
		delay := 10 * time.Second
		for ctx.Err() == nil {
			if videoQueryExpired(t) {
				s.finish(ctx, t.ID, "interrupted", nil, &ark.APIError{Code: "PROVIDER_RESULT_UNAVAILABLE", Message: "任务记录已超过查询窗口，请在供应商控制台核查，未重新生成。"})
				return
			}
			out, err := generator.QueryVideo(ctx, t.ProviderTaskID)
			if ctx.Err() != nil {
				return
			}
			var pollingError *ark.APIError
			if err != nil {
				pollingError = &ark.APIError{Code: "VIDEO_POLL_RETRY", Message: "暂时无法查询视频进度，正在继续查询同一任务。"}
				delay = min(delay*2, 60*time.Second)
				var pe *ark.PollError
				if errors.As(err, &pe) && pe.RetryAfter > delay {
					delay = min(pe.RetryAfter, 10*time.Minute)
				}
			} else {
				delay = 10 * time.Second
				switch out.Status {
				case "succeeded":
					if !strings.HasPrefix(out.Content.URL, "https://") {
						pollingError = &ark.APIError{Code: "VIDEO_RESULT_PENDING", Message: "模型尚未返回可读取的结果，继续核查。"}
						break
					}
					result := &Result{ImageResult: ark.ImageResult{Model: t.Input.Model}, Resolution: out.Resolution, Ratio: out.Ratio, Duration: out.Duration, FramesPerSecond: out.FramesPerSecond}
					if !s.retryWrite(ctx, func(c context.Context) error {
						_, err := s.Pool.Exec(c, `UPDATE generation_tasks SET status='saving',provider_status='succeeded',provider_url=$2,result=$3,polling_error=NULL,updated_at=now() WHERE id=$1 AND status='running'`, t.ID, out.Content.URL, result)
						return err
					}) {
						return
					}
					t.Status = "saving"
					t.ProviderURL = out.Content.URL
					t.Result = result
				case "failed", "expired", "cancelled":
					s.retryWrite(ctx, func(c context.Context) error {
						_, err := s.Pool.Exec(c, `UPDATE generation_tasks SET provider_status=$2,polling_error=NULL,updated_at=now() WHERE id=$1`, t.ID, out.Status)
						return err
					})
					s.finish(ctx, t.ID, "failed", nil, &ark.APIError{Code: "PROVIDER_" + strings.ToUpper(out.Status), Message: "供应商视频任务" + map[string]string{"failed": "失败", "expired": "已过期", "cancelled": "已取消"}[out.Status] + "，请检查素材、提示词和模型控制台。"})
					return
				case "queued", "running":
				default:
					pollingError = &ark.APIError{Code: "UNKNOWN_PROVIDER_STATUS", Message: "供应商返回未知进度，继续查询原任务。"}
				}
			}
			if t.Status == "saving" {
				return // Release the model slot; saving uses its own bounded pool.
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
	if ctx.Err() != nil || t.Status != "saving" {
		return
	}
	result := t.Result
	if result == nil || t.ProviderURL == "" {
		s.finish(ctx, t.ID, "interrupted", nil, &ark.APIError{Code: "RESULT_MISSING", Message: "视频结果来源缺失，请核查任务。"})
		return
	}
	asset, err := objects.SaveGeneratedVideo(ctx, t.ProviderURL, t.Input.Model, scope)
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		s.finish(ctx, t.ID, "storage_failed", nil, &ark.APIError{Code: "VIDEO_STORAGE_FAILED", Message: "视频已生成但保存失败；请重试保存，不会重新生成。"})
		return
	}
	if asset.Kind != storage.Video || asset.Media == nil || asset.Media.Width <= 0 || asset.Media.Height <= 0 || asset.Media.DurationSeconds <= 0 {
		s.finish(ctx, t.ID, "storage_failed", nil, &ark.APIError{Code: "INVALID_VIDEO_RESULT", Message: "保存的视频元数据无效，请重试保存。"})
		return
	}
	result.Asset = &asset
	result.URL = asset.URL
	result.Width = asset.Media.Width
	result.Height = asset.Media.Height
	result.DurationSeconds = asset.Media.DurationSeconds
	audio := asset.Media.AudioCodec != ""
	result.HasAudio = &audio
	s.finish(ctx, t.ID, "succeeded", result, nil)
}

func (s *Store) RetryVideoStorage(ctx context.Context, id string) (Task, error) {
	t, err := s.Task(ctx, id)
	if err != nil {
		return t, err
	}
	if t.Kind != "video" {
		return Task{}, &ark.APIError{Status: 409, Code: "STORAGE_RETRY_NOT_ALLOWED", Message: "该任务不支持视频保存重试。"}
	}
	if t.Status == "saving" || t.Status == "succeeded" {
		return t, nil
	}
	if t.Status != "storage_failed" {
		return Task{}, &ark.APIError{Status: 409, Code: "STORAGE_RETRY_NOT_ALLOWED", Message: "只有视频转存失败才能重试保存。"}
	}
	if t.ProviderURL == "" || videoQueryExpired(t) {
		return Task{}, &ark.APIError{Status: 410, Code: "PROVIDER_RESULT_UNAVAILABLE", Message: "视频结果已超过保存重试窗口，请核查供应商结果。"}
	}
	_, err = s.Pool.Exec(ctx, `UPDATE generation_tasks SET status='saving',error=NULL,finished_at=NULL,updated_at=now() WHERE id=$1 AND status='storage_failed'`, id)
	if err != nil {
		return Task{}, mapError(err)
	}
	return s.Task(ctx, id)
}
