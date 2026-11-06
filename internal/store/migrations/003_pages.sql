-- Project wiki. title_key is the lower-cased, trimmed title computed in Go (SQLite lower() folds only ASCII).
CREATE TABLE pages (
  id         INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects(id),
  parent_id  INTEGER REFERENCES pages(id),
  title      TEXT NOT NULL,
  title_key  TEXT NOT NULL,
  body_md    TEXT NOT NULL DEFAULT '',
  position   REAL NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE UNIQUE INDEX idx_pages_title ON pages(project_id, title_key);
CREATE INDEX idx_pages_parent ON pages(parent_id);
