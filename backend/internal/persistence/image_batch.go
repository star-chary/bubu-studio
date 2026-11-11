package persistence

import (
	"context"
	"encoding/json"
	"fmt"

	"frame-space/backend/internal/ark"
	"frame-space/backend/internal/identity"
	"github.com/jackc/pgx/v5"
)

const nanoImageModel = "gemini-nano-banana-2.1"
const nanoImageExtModel = "gemini-nano-banana-2.1-ext"

func integerParameter(value any) int {
	encoded, err := json.Marshal(value)
	var n int
	if err != nil || json.Unmarshal(encoded, &n) != nil {
		return 0
	}
	return n
}

// Every saved child participates in the parent's one reservation. The parent
// row serializes final settlement when saving slots finish concurrently.
func completeImageBatchCredits(ctx context.Context, tx pgx.Tx, parentID string) error {
	var input Input
	var status, creditStatus, userID string
	var points int64
	if err := tx.QueryRow(ctx, `SELECT t.input,t.status,t.credit_status,t.credit_points,cv.owner_user_id::text
		FROM generation_tasks t JOIN canvases cv ON cv.id=t.canvas_id WHERE t.id=$1 FOR UPDATE OF t`, parentID).
		Scan(&input, &status, &creditStatus, &points, &userID); err != nil {
		return err
	}
	if creditStatus != "reserved" && creditStatus != "review" {
		return nil
	}
	expected := nanoBatchCount(input)
	if expected == 0 || len(input.OutputNodeIDs) != expected {
		return completeTaskCredits(ctx, tx, parentID, userID, creditStatus, "review", points)
	}
	var total, active, saved, flagged int
	if err := tx.QueryRow(ctx, `SELECT count(*),
		count(*) FILTER (WHERE status IN ('queued','preparing','submitting','running','saving')),
		count(*) FILTER (WHERE status='succeeded' AND jsonb_typeof(result->'asset')='object'),
		count(*) FILTER (WHERE result->>'batchReview'='true')
		FROM generation_tasks WHERE id=$1::uuid OR input->>'batchParentTaskId'=$1::text`, parentID).
		Scan(&total, &active, &saved, &flagged); err != nil {
		return err
	}
	if total == expected && active > 0 {
		return nil
	}
	action := "review"
	if total == expected && status == "succeeded" && saved == expected && flagged == 0 {
		action = "settle"
	}
	return completeTaskCredits(ctx, tx, parentID, userID, creditStatus, action, points)
}

// Distribute all returned URLs in one transaction. Missing or invalid outputs
// are marked for review; no second paid supplier request is created.
func (s *Store) distributeImageBatch(ctx context.Context, task Task, urls []string) {
	count := nanoBatchCount(task.Input)
	if count == 0 || len(task.Input.OutputNodeIDs) != count {
		s.finish(ctx, task.ID, "interrupted", nil, &ark.APIError{Code: "BATCH_INVALID", Message: "图片任务信息不完整，请核查原任务。"})
		return
	}
	review := len(urls) != count
	for _, address := range urls {
		if address == "" {
			review = true
		}
	}
	failure := &ark.APIError{Code: "BATCH_OUTPUT_MISSING", Message: "供应商返回的图片数量不足或地址无效，请核查原任务，未自动重新生成。"}
	s.retryWrite(ctx, func(c context.Context) error {
		tx, err := s.Pool.Begin(c)
		if err != nil {
			return err
		}
		defer tx.Rollback(context.Background())
		var status string
		if err = tx.QueryRow(c, `SELECT status FROM generation_tasks WHERE id=$1 FOR UPDATE`, task.ID).Scan(&status); err != nil {
			return err
		}
		if status != "running" {
			return nil
		}
		for index, nodeID := range task.Input.OutputNodeIDs {
			address := ""
			if index < len(urls) {
				address = urls[index]
			}
			var result *Result
			var itemFailure *ark.APIError
			nextStatus := "saving"
			if address == "" {
				nextStatus, itemFailure = "interrupted", failure
			} else {
				result = &Result{ImageResult: ark.ImageResult{URL: address, Model: task.Input.Model}, BatchReview: index == 0 && review}
			}
			query := `UPDATE generation_tasks SET status=$3,provider_task_id=$4,provider_status='succeeded',provider_url=$5,
				result=$6,error=$7,polling_error=NULL,finished_at=CASE WHEN $3='interrupted' THEN now() ELSE NULL END,updated_at=now()
				WHERE input->>'batchParentTaskId'=$1 AND node_id=$2 AND status='queued'`
			if index == 0 {
				query = `UPDATE generation_tasks SET status=$3,provider_task_id=$4,provider_status='succeeded',provider_url=$5,
					result=$6,error=$7,polling_error=NULL,finished_at=CASE WHEN $3='interrupted' THEN now() ELSE NULL END,updated_at=now()
					WHERE id=$1 AND node_id=$2 AND status='running'`
			}
			outcome, err := tx.Exec(c, query, task.ID, nodeID, nextStatus, task.ProviderTaskID, address, result, itemFailure)
			if err != nil {
				return err
			}
			if outcome.RowsAffected() != 1 {
				return fmt.Errorf("batch output node is missing or already claimed")
			}
		}
		if err = completeImageBatchCredits(c, tx, task.ID); err != nil {
			return err
		}
		return tx.Commit(c)
	})
}

func nanoBatchCount(input Input) int {
	if input.Provider != ProviderAPIMart || input.Model != nanoImageModel {
		return 0
	}
	n := integerParameter(input.Parameters["n"])
	if n != 2 && n != 4 {
		return 0
	}
	return n
}

func validateNanoSelection(selection *ModelSelection, input Input) error {
	count := 1
	if value, ok := selection.Parameters["n"]; ok {
		count = integerParameter(value)
	}
	actual := integerParameter(input.Parameters["n"])
	if count != actual || (count == 1 && input.Model != nanoImageExtModel) || ((count == 2 || count == 4) && input.Model != nanoImageModel) || (count != 1 && count != 2 && count != 4) {
		return ErrInvalid
	}
	return nil
}

// A quote needs no output nodes. Task creation must bind every output to a
// distinct saved image node before credits are reserved or a provider is called.
func (s *Store) validateImageBatchTargets(ctx context.Context, input Input, kind string) error {
	count := nanoBatchCount(input)
	if count == 0 {
		if len(input.OutputNodeIDs) != 0 {
			return ErrInvalid
		}
		return nil
	}
	if kind != "image" || len(input.OutputNodeIDs) != count || input.OutputNodeIDs[0] != input.NodeID {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, id := range input.OutputNodeIDs {
		if !ValidID(id) || seen[id] {
			return ErrInvalid
		}
		seen[id] = true
		node, err := s.Node(ctx, input.CanvasID, id)
		if err != nil {
			return err
		}
		if node.Data.Kind != "image" || node.Data.Origin == "upload" || node.Data.ModelSelection == nil || node.Data.ModelSelection.Provider != ProviderAPIMart || node.Data.ModelSelection.ModelID != nanoImageModel {
			return ErrInvalid
		}
	}
	return nil
}

// Children reserve their nodes but never submit to APIMart or reserve credits.
// All records are created in the parent's credit-reservation transaction.
func insertImageBatchChildren(ctx context.Context, tx pgx.Tx, parentID string, input Input, compiled string, bindings []Reference) error {
	if nanoBatchCount(input) == 0 {
		return nil
	}
	for _, nodeID := range input.OutputNodeIDs[1:] {
		child := input
		child.NodeID = nodeID
		child.OutputNodeIDs = nil
		child.BatchParentTaskID = parentID
		_, err := tx.Exec(ctx, `INSERT INTO generation_tasks(id,canvas_id,node_id,input,status,kind,compiled_prompt,bindings)
			VALUES($1,$2,$3,$4,'queued','image',$5,$6)`, identity.ID(), input.CanvasID, nodeID, child, compiled, bindings)
		if err != nil {
			return mapError(err)
		}
	}
	return nil
}
