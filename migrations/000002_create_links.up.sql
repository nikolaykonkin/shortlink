CREATE TABLE links (
    id           BIGSERIAL PRIMARY KEY,
    short_code   VARCHAR(32)  NOT NULL UNIQUE,
    original_url TEXT         NOT NULL,
    user_id      BIGINT       NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_links_user_id ON links (user_id);

-- Частичный индекс: бессрочные ссылки в выборку воркера никогда не попадают
CREATE INDEX idx_links_expires_at ON links (expires_at) WHERE expires_at IS NOT NULL;
