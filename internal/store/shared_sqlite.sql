CREATE TABLE projects (
  workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  id          INTEGER PRIMARY KEY,
  slug        TEXT NOT NULL,
  key         TEXT NOT NULL CHECK (key GLOB '[A-Z][A-Z0-9]*' AND length(key) BETWEEN 2 AND 5),
  name        TEXT NOT NULL,
  icon        TEXT NOT NULL DEFAULT 'board',
  color       TEXT NOT NULL DEFAULT '#2D5CC9',
  description TEXT NOT NULL DEFAULT '',
  task_seq    INTEGER NOT NULL DEFAULT 0,
  position    REAL NOT NULL,
  archived_at TEXT,
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  deleted_at TEXT,
  icon_png BLOB,
  icon_hash TEXT NOT NULL DEFAULT '',
  UNIQUE(workspace_id,slug),
  UNIQUE(workspace_id,key),
  UNIQUE(workspace_id,id)
);

CREATE TABLE statuses (
  workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  id            INTEGER PRIMARY KEY,
  project_id    INTEGER NOT NULL,
  key           TEXT NOT NULL,
  name          TEXT NOT NULL,
  color         TEXT NOT NULL,
  category      TEXT NOT NULL CHECK (category IN ('todo','doing','done')),
  show_on_board INTEGER NOT NULL DEFAULT 1,
  position      REAL NOT NULL,
  UNIQUE (project_id, key),
  UNIQUE(workspace_id,id),
  FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id)
);

CREATE TABLE labels (
  workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  id         INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL,
  name       TEXT NOT NULL,
  color      TEXT NOT NULL,
  UNIQUE (project_id, name),
  UNIQUE(workspace_id,id),
  FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id)
);

CREATE TABLE custom_fields (
  workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  id           INTEGER PRIMARY KEY,
  project_id   INTEGER NOT NULL,
  name         TEXT NOT NULL,
  type         TEXT NOT NULL CHECK (type IN ('number','url','select')),
  options      TEXT NOT NULL DEFAULT '[]',
  show_on_card INTEGER NOT NULL DEFAULT 0,
  position     REAL NOT NULL,
  UNIQUE (project_id, name),
  UNIQUE(workspace_id,id),
  FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id)
);

CREATE TABLE tasks (
  workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  id             INTEGER PRIMARY KEY,
  project_id     INTEGER NOT NULL,
  number         INTEGER NOT NULL,
  parent_id      INTEGER,
  title          TEXT NOT NULL,
  description_md TEXT NOT NULL DEFAULT '',
  status_id      INTEGER NOT NULL,
  priority       TEXT NOT NULL DEFAULT 'P1' CHECK (priority IN ('P0','P1','P2')),
  area           TEXT NOT NULL DEFAULT '',
  planned_for    TEXT,
  position       REAL NOT NULL,
  custom         TEXT NOT NULL DEFAULT '{}',
  external_id    TEXT,
  created_by     TEXT NOT NULL DEFAULT 'mcp' CHECK(length(created_by) BETWEEN 1 AND 48),
  created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  done_at        TEXT,
  deleted_at     TEXT,
  UNIQUE (project_id, number),
  UNIQUE(workspace_id,external_id),
  UNIQUE(workspace_id,id),
  FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id),
  FOREIGN KEY(workspace_id,parent_id) REFERENCES tasks(workspace_id,id),
  FOREIGN KEY(workspace_id,status_id) REFERENCES statuses(workspace_id,id)
);
CREATE INDEX idx_tasks_board ON tasks(workspace_id, project_id, status_id, position) WHERE deleted_at IS NULL;
CREATE INDEX idx_tasks_day   ON tasks(workspace_id, planned_for) WHERE deleted_at IS NULL;

CREATE TABLE saved_views (
  workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  id          INTEGER PRIMARY KEY,
  project_id  INTEGER,
  name        TEXT NOT NULL,
  layout      TEXT NOT NULL CHECK (layout IN ('board','list')),
  filter_json TEXT NOT NULL DEFAULT '{}',
  group_by    TEXT NOT NULL DEFAULT 'status',
  sort        TEXT NOT NULL DEFAULT 'manual',
  pinned      INTEGER NOT NULL DEFAULT 0,
  position    REAL NOT NULL,
  UNIQUE(workspace_id,id),
  FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id)
);

CREATE TABLE notes (
  workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  id         INTEGER PRIMARY KEY,
  task_id    INTEGER NOT NULL,
  author     TEXT NOT NULL DEFAULT 'mcp' CHECK(length(author) BETWEEN 1 AND 48),
  body_md    TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  UNIQUE(workspace_id,id),
  FOREIGN KEY(workspace_id,task_id) REFERENCES tasks(workspace_id,id)
);

CREATE TABLE activity (
  workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  id         INTEGER PRIMARY KEY,
  task_id    INTEGER,
  project_id INTEGER,
  actor      TEXT NOT NULL DEFAULT 'mcp' CHECK(length(actor) BETWEEN 1 AND 48),
  action     TEXT NOT NULL,
  diff_json  TEXT NOT NULL DEFAULT '{}',
  at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  UNIQUE(workspace_id,id),
  FOREIGN KEY(workspace_id,task_id) REFERENCES tasks(workspace_id,id) ON DELETE CASCADE,
  FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id) ON DELETE CASCADE
);
CREATE INDEX idx_activity_task ON activity(workspace_id, task_id, at);

CREATE TABLE settings (workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE, key TEXT NOT NULL, value TEXT NOT NULL, PRIMARY KEY(workspace_id,key));

CREATE TABLE mcp_calls (
  workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  id          INTEGER PRIMARY KEY,
  at          TEXT NOT NULL,
  tool        TEXT NOT NULL,
  args_summary TEXT NOT NULL,
  duration_ms INTEGER NOT NULL,
  status      TEXT NOT NULL CHECK (status IN ('ok','error')),
  error_code  TEXT,
  actor       TEXT NOT NULL DEFAULT 'claude',
  UNIQUE(workspace_id,id)
);
CREATE INDEX idx_mcp_calls_expiry ON mcp_calls(at);
CREATE INDEX idx_mcp_calls_at ON mcp_calls(workspace_id, at);

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
-- Project wiki. title_key is the lower-cased, trimmed title computed in Go (SQLite lower() folds only ASCII).
CREATE TABLE pages (
  workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  id         INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL,
  parent_id  INTEGER,
  title      TEXT NOT NULL,
  title_key  TEXT NOT NULL,
  body_md    TEXT NOT NULL DEFAULT '',
  position   REAL NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  pinned INTEGER NOT NULL DEFAULT 0,
  body_html TEXT CHECK(body_html IS NULL OR body_md=''),
  UNIQUE(workspace_id,id),
  FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id),
  FOREIGN KEY(workspace_id,parent_id) REFERENCES pages(workspace_id,id)
);
CREATE UNIQUE INDEX idx_pages_title ON pages(project_id, title_key);
CREATE INDEX idx_pages_parent ON pages(parent_id);
ALTER TABLE tasks ADD COLUMN issue_kind TEXT NOT NULL DEFAULT '' CHECK (issue_kind IN ('','problem','unverified','decision','blocker'));
ALTER TABLE tasks ADD COLUMN attention_after TEXT;

CREATE TABLE issue_reports (
  workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  issue_id INTEGER NOT NULL,
  fingerprint TEXT NOT NULL,
  PRIMARY KEY (issue_id, fingerprint),
  FOREIGN KEY(workspace_id,issue_id) REFERENCES tasks(workspace_id,id) ON DELETE CASCADE
);
CREATE INDEX idx_tasks_attention ON tasks(project_id, issue_kind, area)
  WHERE deleted_at IS NULL AND (issue_kind <> '' OR area = 'issue');
CREATE TABLE html_documents (
  workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id INTEGER,
  page_id INTEGER,
  title TEXT NOT NULL,
  title_key TEXT NOT NULL,
  version INTEGER NOT NULL DEFAULT 0,
  CHECK ((task_id IS NULL) <> (page_id IS NULL)),
  UNIQUE (task_id, title_key),
  UNIQUE (page_id, title_key),
  UNIQUE(workspace_id,id),
  FOREIGN KEY(workspace_id,task_id) REFERENCES tasks(workspace_id,id) ON DELETE CASCADE,
  FOREIGN KEY(workspace_id,page_id) REFERENCES pages(workspace_id,id) ON DELETE CASCADE
);
CREATE TABLE html_revisions (
  workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  document_id INTEGER NOT NULL,
  version INTEGER NOT NULL,
  author TEXT NOT NULL,
  created_at TEXT NOT NULL,
  html TEXT NOT NULL,
  PRIMARY KEY (document_id, version),
  FOREIGN KEY(workspace_id,document_id) REFERENCES html_documents(workspace_id,id) ON DELETE CASCADE
);
