-- +goose Up
CREATE TABLE api_keys (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name       TEXT NOT NULL,
    key_prefix TEXT NOT NULL,
    key_hash   BYTEA NOT NULL UNIQUE CHECK (octet_length(key_hash) = 32),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ
);

CREATE TABLE usage_logs (
    id                BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    api_key_id        BIGINT NOT NULL REFERENCES api_keys (id),
    started_at        TIMESTAMPTZ NOT NULL,
    duration_ms       INTEGER NOT NULL CHECK (duration_ms >= 0),
    model             TEXT NOT NULL,
    provider          TEXT NOT NULL,
    stream            BOOLEAN NOT NULL,
    status_code       INTEGER NOT NULL,
    outcome           TEXT NOT NULL CHECK (outcome IN
                          ('ok', 'client_canceled', 'idle_timeout', 'upstream_error', 'bad_request')),
    prompt_tokens     INTEGER CHECK (prompt_tokens >= 0),
    completion_tokens INTEGER CHECK (completion_tokens >= 0)
);

CREATE INDEX usage_logs_api_key_id_started_at_idx ON usage_logs (api_key_id, started_at);

-- +goose Down
DROP TABLE usage_logs;
DROP TABLE api_keys;
