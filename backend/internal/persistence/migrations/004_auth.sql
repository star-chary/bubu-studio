CREATE TABLE users (
    id uuid PRIMARY KEY,
    email text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    email_verified_at timestamptz,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE user_sessions (
    token_hash text PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users(id),
    csrf_token text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    idle_expires_at timestamptz NOT NULL DEFAULT now() + interval '24 hours',
    absolute_expires_at timestamptz NOT NULL DEFAULT now() + interval '7 days',
    revoked_at timestamptz
);
CREATE INDEX user_sessions_user_idx ON user_sessions(user_id);
CREATE INDEX user_sessions_expiry_idx ON user_sessions(absolute_expires_at);
-- No automatic data destruction on application startup. Reset legacy canvases
-- explicitly with cmd/reset-canvases before applying this migration.
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM canvases) THEN
        RAISE EXCEPTION 'legacy canvases require explicit reset before auth migration';
    END IF;
END $$;
ALTER TABLE canvases ADD COLUMN owner_user_id uuid NOT NULL REFERENCES users(id);
CREATE INDEX canvases_owner_recent_idx ON canvases(owner_user_id, updated_at DESC, id DESC);
