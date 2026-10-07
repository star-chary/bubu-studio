-- Credits are test-only and cannot be purchased. Existing tasks remain legacy.
CREATE TABLE credit_accounts (
    user_id uuid PRIMARY KEY REFERENCES users(id),
    available bigint NOT NULL DEFAULT 0 CHECK (available >= 0),
    reserved bigint NOT NULL DEFAULT 0 CHECK (reserved >= 0),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE credit_ledger (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users(id),
    task_id uuid REFERENCES generation_tasks(id),
    operation text NOT NULL CHECK (operation IN ('grant','reserve','settle','release')),
    available_delta bigint NOT NULL,
    reserved_delta bigint NOT NULL,
    available_after bigint NOT NULL CHECK (available_after >= 0),
    reserved_after bigint NOT NULL CHECK (reserved_after >= 0),
    idempotency_key text NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (available_delta <> 0 OR reserved_delta <> 0)
);
CREATE INDEX credit_ledger_user_recent ON credit_ledger(user_id,created_at DESC,id DESC);
CREATE INDEX credit_ledger_task ON credit_ledger(task_id) WHERE task_id IS NOT NULL;
ALTER TABLE generation_tasks
    ADD COLUMN credit_points bigint NOT NULL DEFAULT 0 CHECK (credit_points >= 0),
    ADD COLUMN credit_price_version text NOT NULL DEFAULT '',
    ADD COLUMN credit_status text NOT NULL DEFAULT 'legacy'
        CHECK (credit_status IN ('legacy','reserved','settled','released','review'));
