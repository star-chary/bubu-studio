CREATE TABLE IF NOT EXISTS canvases (
    id uuid PRIMARY KEY,
    version bigint NOT NULL DEFAULT 0,
    snapshot jsonb NOT NULL DEFAULT '{"nodes":[],"viewport":{"x":0,"y":0,"zoom":1}}',
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS assets (
    key text PRIMARY KEY,
    canvas_id uuid NOT NULL REFERENCES canvases(id),
    node_id uuid NOT NULL,
    metadata jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS assets_node_idx ON assets(canvas_id, node_id, created_at DESC);
CREATE TABLE IF NOT EXISTS generation_tasks (
    id uuid PRIMARY KEY,
    canvas_id uuid NOT NULL REFERENCES canvases(id),
    node_id uuid NOT NULL,
    input jsonb NOT NULL,
    status text NOT NULL CHECK (status IN ('queued','running','saving','succeeded','failed','interrupted')),
    result jsonb,
    error jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    finished_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now()
);
-- Preserve the local application's one-active-generation limit across requests/processes.
CREATE UNIQUE INDEX IF NOT EXISTS generation_one_active ON generation_tasks ((1))
    WHERE status IN ('queued','running','saving');
CREATE INDEX IF NOT EXISTS generation_history_idx ON generation_tasks(canvas_id, created_at DESC, id);
