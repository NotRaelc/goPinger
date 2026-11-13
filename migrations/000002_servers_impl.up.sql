CREATE TABLE IF NOT EXISTS servers (
    id            BIGSERIAL PRIMARY KEY,
    user_id       VARCHAR(32) NOT NULL REFERENCES users(username) ON DELETE CASCADE,
    address       VARCHAR(255) NOT NULL,
    type          SMALLINT NOT NULL DEFAULT 0,
    interval_sec  INTEGER NOT NULL DEFAULT 0,
    label         VARCHAR(255) NOT NULL DEFAULT '',
    enabled       BOOLEAN NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, address)
);

CREATE INDEX IF NOT EXISTS idx_servers_user    ON servers(user_id);
CREATE INDEX IF NOT EXISTS idx_servers_enabled ON servers(enabled) WHERE enabled;

CREATE TABLE IF NOT EXISTS server_states (
    server_id   BIGINT PRIMARY KEY REFERENCES servers(id) ON DELETE CASCADE,
    ok          BOOLEAN NOT NULL DEFAULT FALSE,
    error       TEXT,
    payload     JSONB,
    checked_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);