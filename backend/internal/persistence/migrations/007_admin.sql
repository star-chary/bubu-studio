ALTER TABLE users ADD COLUMN role text NOT NULL DEFAULT 'user'
    CHECK (role IN ('user', 'admin'));
CREATE INDEX users_recent ON users(created_at DESC, id DESC);

ALTER TABLE credit_ledger
    ADD COLUMN source text NOT NULL DEFAULT 'system'
        CHECK (source IN ('system', 'signup', 'manual', 'legacy')),
    ADD COLUMN actor_user_id uuid REFERENCES users(id),
    ADD COLUMN reason text NOT NULL DEFAULT '',
    ADD CONSTRAINT credit_ledger_manual_audit CHECK (
        source <> 'manual' OR (operation = 'grant' AND actor_user_id IS NOT NULL
            AND char_length(btrim(reason)) BETWEEN 1 AND 200)
    );
-- Preserve historical amounts; classify only grants with a known origin.
UPDATE credit_ledger SET source = CASE
    WHEN idempotency_key LIKE 'signup-welcome-v1:%' THEN 'signup'
    ELSE 'legacy' END WHERE operation = 'grant';
