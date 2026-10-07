-- Pending tasks are durable. Only one unfinished generation per canvas node.
DROP INDEX generation_one_active;
ALTER TABLE generation_tasks ADD COLUMN claimed_at timestamptz;
ALTER TABLE generation_tasks DROP CONSTRAINT generation_tasks_status_check;
ALTER TABLE generation_tasks ADD CONSTRAINT generation_tasks_status_check CHECK
    (status IN ('queued','preparing','submitting','running','saving','succeeded','failed','interrupted','storage_failed'));
CREATE UNIQUE INDEX generation_node_active ON generation_tasks(canvas_id,node_id)
    WHERE status IN ('queued','preparing','submitting','running','saving');
CREATE INDEX generation_dispatch_queue ON generation_tasks(kind,created_at,id)
    WHERE claimed_at IS NULL AND status IN ('queued','running','saving');
