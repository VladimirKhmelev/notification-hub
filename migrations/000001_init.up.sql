CREATE TABLE sources (
    id         BIGSERIAL PRIMARY KEY,
    type       TEXT        NOT NULL,
    name       TEXT        NOT NULL,
    config     JSONB       NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE events (
    id         BIGSERIAL PRIMARY KEY,
    source_id  BIGINT      NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    title      TEXT        NOT NULL,
    body       TEXT        NOT NULL DEFAULT '',
    priority   TEXT        NOT NULL DEFAULT 'normal',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    read_at    TIMESTAMPTZ
);

CREATE INDEX events_source_id_idx ON events(source_id);
CREATE INDEX events_created_at_idx ON events(created_at DESC);
