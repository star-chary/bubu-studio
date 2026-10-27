-- MCP authorization is separate from browser sessions and contains no user data copy.
CREATE TABLE mcp_oauth_clients (
    id text PRIMARY KEY,
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    redirect_uris text[] NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL DEFAULT now() + interval '90 days'
);

CREATE TABLE mcp_oauth_tokens (
    token_hash text PRIMARY KEY,
    kind text NOT NULL CHECK (kind IN ('code', 'access')),
    client_id text NOT NULL REFERENCES mcp_oauth_clients(id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    metadata jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    CHECK (NOT metadata ? 'Code' AND NOT metadata ? 'Access' AND NOT metadata ? 'Refresh')
);
CREATE INDEX mcp_oauth_tokens_user_client ON mcp_oauth_tokens(user_id, client_id);
CREATE INDEX mcp_oauth_tokens_expiry ON mcp_oauth_tokens(expires_at);
