CREATE TABLE projects (
  id          INTEGER PRIMARY KEY,
  slug        TEXT NOT NULL UNIQUE,
  key         TEXT NOT NULL UNIQUE CHECK (key GLOB '[A-Z][A-Z0-9]*' AND length(key) BETWEEN 2 AND 5),
  name        TEXT NOT NULL,
  icon        TEXT NOT NULL DEFAULT 'board',
  color       TEXT NOT NULL DEFAULT '#2D5CC9',
  description TEXT NOT NULL DEFAULT '',
  task_seq    INTEGER NOT NULL DEFAULT 0,
  position    REAL NOT NULL,
  archived_at TEXT,
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE statuses (
  id            INTEGER PRIMARY KEY,
  project_id    INTEGER NOT NULL REFERENCES projects(id),
  key           TEXT NOT NULL,
  name          TEXT NOT NULL,
  color         TEXT NOT NULL,
  category      TEXT NOT NULL CHECK (category IN ('todo','doing','done')),
  show_on_board INTEGER NOT NULL DEFAULT 1,
  position      REAL NOT NULL,
  UNIQUE (project_id, key)
);

CREATE TABLE labels (
  id         INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects(id),
  name       TEXT NOT NULL,
  color      TEXT NOT NULL,
  UNIQUE (project_id, name)
);

CREATE TABLE custom_fields (
  id           INTEGER PRIMARY KEY,
  project_id   INTEGER NOT NULL REFERENCES projects(id),
  name         TEXT NOT NULL,
  type         TEXT NOT NULL CHECK (type IN ('number','url','select')),
  options      TEXT NOT NULL DEFAULT '[]',
  show_on_card INTEGER NOT NULL DEFAULT 0,
  position     REAL NOT NULL,
  UNIQUE (project_id, name)
);

CREATE TABLE tasks (
  id             INTEGER PRIMARY KEY,
  project_id     INTEGER NOT NULL REFERENCES projects(id),
  number         INTEGER NOT NULL,
  parent_id      INTEGER REFERENCES tasks(id),
  title          TEXT NOT NULL,
  description_md TEXT NOT NULL DEFAULT '',
  status_id      INTEGER NOT NULL REFERENCES statuses(id),
  priority       TEXT NOT NULL DEFAULT 'P1' CHECK (priority IN ('P0','P1','P2')),
  area           TEXT NOT NULL DEFAULT '',
  planned_for    TEXT,
  position       REAL NOT NULL,
  custom         TEXT NOT NULL DEFAULT '{}',
  external_id    TEXT UNIQUE,
  created_by     TEXT NOT NULL CHECK (created_by IN ('denis','claude')),
  created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  done_at        TEXT,
  deleted_at     TEXT,
  UNIQUE (project_id, number)
);
CREATE INDEX idx_tasks_board ON tasks(project_id, status_id, position) WHERE deleted_at IS NULL;
CREATE INDEX idx_tasks_day   ON tasks(planned_for) WHERE deleted_at IS NULL;

CREATE TABLE saved_views (
  id          INTEGER PRIMARY KEY,
  project_id  INTEGER REFERENCES projects(id),
  name        TEXT NOT NULL,
  layout      TEXT NOT NULL CHECK (layout IN ('board','list')),
  filter_json TEXT NOT NULL DEFAULT '{}',
  group_by    TEXT NOT NULL DEFAULT 'status',
  sort        TEXT NOT NULL DEFAULT 'manual',
  pinned      INTEGER NOT NULL DEFAULT 0,
  position    REAL NOT NULL
);

CREATE TABLE notes (
  id         INTEGER PRIMARY KEY,
  task_id    INTEGER NOT NULL REFERENCES tasks(id),
  author     TEXT NOT NULL CHECK (author IN ('denis','claude')),
  body_md    TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE activity (
  id         INTEGER PRIMARY KEY,
  task_id    INTEGER,
  project_id INTEGER,
  actor      TEXT NOT NULL CHECK (actor IN ('denis','claude')),
  action     TEXT NOT NULL,
  diff_json  TEXT NOT NULL DEFAULT '{}',
  at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX idx_activity_task ON activity(task_id, at);

CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);

CREATE TABLE mcp_calls (
  id          INTEGER PRIMARY KEY,
  at          TEXT NOT NULL,
  tool        TEXT NOT NULL,
  args_summary TEXT NOT NULL,
  duration_ms INTEGER NOT NULL,
  status      TEXT NOT NULL CHECK (status IN ('ok','error')),
  error_code  TEXT,
  actor       TEXT NOT NULL DEFAULT 'claude'
);
CREATE INDEX idx_mcp_calls_at ON mcp_calls(at);

CREATE VIRTUAL TABLE tasks_fts USING fts5(title, description_md, content='tasks', content_rowid='id');

CREATE TRIGGER tasks_fts_ai AFTER INSERT ON tasks BEGIN
  INSERT INTO tasks_fts(rowid, title, description_md) VALUES (new.id, new.title, new.description_md);
END;
CREATE TRIGGER tasks_fts_ad AFTER DELETE ON tasks BEGIN
  INSERT INTO tasks_fts(tasks_fts, rowid, title, description_md) VALUES ('delete', old.id, old.title, old.description_md);
END;
CREATE TRIGGER tasks_fts_au AFTER UPDATE OF title, description_md ON tasks BEGIN
  INSERT INTO tasks_fts(tasks_fts, rowid, title, description_md) VALUES ('delete', old.id, old.title, old.description_md);
  INSERT INTO tasks_fts(rowid, title, description_md) VALUES (new.id, new.title, new.description_md);
END;
