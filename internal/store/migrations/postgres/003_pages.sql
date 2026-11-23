-- Project wiki. title_key is the lower-cased, trimmed title computed in Go (SQLite lower() folds only ASCII).
CREATE TABLE pages (
  id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  project_id BIGINT NOT NULL REFERENCES projects(id),
  parent_id  BIGINT REFERENCES pages(id),
  title      TEXT NOT NULL,
  title_key  TEXT NOT NULL,
  body_md    TEXT NOT NULL DEFAULT '',
  position   DOUBLE PRECISION NOT NULL,
  created_at TEXT NOT NULL DEFAULT (to_char(clock_timestamp() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"')),
  updated_at TEXT NOT NULL DEFAULT (to_char(clock_timestamp() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"'))
);
CREATE UNIQUE INDEX idx_pages_title ON pages(project_id, title_key);
CREATE INDEX idx_pages_parent ON pages(parent_id);
