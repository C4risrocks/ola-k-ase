DELETE FROM sessions;

ALTER TABLE sessions ADD COLUMN csrf_hash TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions (expires_at);
