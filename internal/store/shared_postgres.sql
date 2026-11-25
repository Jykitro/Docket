CREATE TABLE projects (
  workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  slug        TEXT NOT NULL,
  key         TEXT NOT NULL CHECK (key ~ '^[A-Z][A-Z0-9]{1,4}$'),
  name        TEXT NOT NULL,
  icon        TEXT NOT NULL DEFAULT 'board',
  color       TEXT NOT NULL DEFAULT '#2D5CC9',
  description TEXT NOT NULL DEFAULT '',
  task_seq    BIGINT NOT NULL DEFAULT 0,
  position    DOUBLE PRECISION NOT NULL,
  archived_at TEXT,
  created_at  TEXT NOT NULL DEFAULT (to_char(clock_timestamp() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"')),
  updated_at  TEXT NOT NULL DEFAULT (to_char(clock_timestamp() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"')),
  deleted_at TEXT,
  icon_png BYTEA,
  icon_hash TEXT NOT NULL DEFAULT '',
  UNIQUE(workspace_id,slug),
  UNIQUE(workspace_id,key),
  UNIQUE(workspace_id,id)
);

CREATE TABLE statuses (
  workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  project_id    BIGINT NOT NULL,
  key           TEXT NOT NULL,
  name          TEXT NOT NULL,
  color         TEXT NOT NULL,
  category      TEXT NOT NULL CHECK (category IN ('todo','doing','done')),
  show_on_board BIGINT NOT NULL DEFAULT 1,
  position      DOUBLE PRECISION NOT NULL,
  UNIQUE (project_id, key),
  UNIQUE(workspace_id,id),
  FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id)
);

CREATE TABLE labels (
  workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  project_id BIGINT NOT NULL,
  name       TEXT NOT NULL,
  color      TEXT NOT NULL,
  UNIQUE (project_id, name),
  UNIQUE(workspace_id,id),
  FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id)
);

CREATE TABLE custom_fields (
  workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  project_id   BIGINT NOT NULL,
  name         TEXT NOT NULL,
  type         TEXT NOT NULL CHECK (type IN ('number','url','select')),
  options      TEXT NOT NULL DEFAULT '[]',
  show_on_card BIGINT NOT NULL DEFAULT 0,
  position     DOUBLE PRECISION NOT NULL,
  UNIQUE (project_id, name),
  UNIQUE(workspace_id,id),
  FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id)
);

CREATE TABLE tasks (
  workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  project_id     BIGINT NOT NULL,
  number         BIGINT NOT NULL,
  parent_id      BIGINT,
  title          TEXT NOT NULL,
  description_md TEXT NOT NULL DEFAULT '',
  status_id      BIGINT NOT NULL,
  priority       TEXT NOT NULL DEFAULT 'P1' CHECK (priority IN ('P0','P1','P2')),
  area           TEXT NOT NULL DEFAULT '',
  planned_for    TEXT,
  position       DOUBLE PRECISION NOT NULL,
  custom         TEXT NOT NULL DEFAULT '{}',
  external_id    TEXT,
  created_by     TEXT NOT NULL DEFAULT 'mcp' CHECK(length(created_by) BETWEEN 1 AND 48),
  created_at     TEXT NOT NULL DEFAULT (to_char(clock_timestamp() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"')),
  updated_at     TEXT NOT NULL DEFAULT (to_char(clock_timestamp() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"')),
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
  workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  project_id  BIGINT,
  name        TEXT NOT NULL,
  layout      TEXT NOT NULL CHECK (layout IN ('board','list')),
  filter_json TEXT NOT NULL DEFAULT '{}',
  group_by    TEXT NOT NULL DEFAULT 'status',
  sort        TEXT NOT NULL DEFAULT 'manual',
  pinned      BIGINT NOT NULL DEFAULT 0,
  position    DOUBLE PRECISION NOT NULL,
  UNIQUE(workspace_id,id),
  FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id)
);

CREATE TABLE notes (
  workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  task_id    BIGINT NOT NULL,
  author     TEXT NOT NULL DEFAULT 'mcp' CHECK(length(author) BETWEEN 1 AND 48),
  body_md    TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (to_char(clock_timestamp() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"')),
  UNIQUE(workspace_id,id),
  FOREIGN KEY(workspace_id,task_id) REFERENCES tasks(workspace_id,id)
);

CREATE TABLE activity (
  workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  task_id    BIGINT,
  project_id BIGINT,
  actor      TEXT NOT NULL DEFAULT 'mcp' CHECK(length(actor) BETWEEN 1 AND 48),
  action     TEXT NOT NULL,
  diff_json  TEXT NOT NULL DEFAULT '{}',
  at         TEXT NOT NULL DEFAULT (to_char(clock_timestamp() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"')),
  UNIQUE(workspace_id,id),
  FOREIGN KEY(workspace_id,task_id) REFERENCES tasks(workspace_id,id) ON DELETE CASCADE,
  FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id) ON DELETE CASCADE
);
CREATE INDEX idx_activity_task ON activity(workspace_id, task_id, at);

CREATE TABLE settings (workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE, key TEXT NOT NULL, value TEXT NOT NULL, PRIMARY KEY(workspace_id,key));

CREATE TABLE mcp_calls (
  workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  at          TEXT NOT NULL,
  tool        TEXT NOT NULL,
  args_summary TEXT NOT NULL,
  duration_ms BIGINT NOT NULL,
  status      TEXT NOT NULL CHECK (status IN ('ok','error')),
  error_code  TEXT,
  actor       TEXT NOT NULL DEFAULT 'claude',
  UNIQUE(workspace_id,id)
);
CREATE INDEX idx_mcp_calls_expiry ON mcp_calls(at);
CREATE INDEX idx_mcp_calls_at ON mcp_calls(workspace_id, at);

-- The simple dictionary keeps Russian, Ukrainian and English prefixes without stemming.
CREATE INDEX idx_tasks_search ON tasks USING gin(to_tsvector('simple', title || ' ' || description_md));
CREATE INDEX idx_tasks_title_search ON tasks USING gin(to_tsvector('simple', title));
CREATE INDEX idx_tasks_description_search ON tasks USING gin(to_tsvector('simple', description_md));
-- Project wiki. title_key is the lower-cased, trimmed title computed in Go (SQLite lower() folds only ASCII).
CREATE TABLE pages (
  workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  project_id BIGINT NOT NULL,
  parent_id  BIGINT,
  title      TEXT NOT NULL,
  title_key  TEXT NOT NULL,
  body_md    TEXT NOT NULL DEFAULT '',
  position   DOUBLE PRECISION NOT NULL,
  created_at TEXT NOT NULL DEFAULT (to_char(clock_timestamp() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"')),
  updated_at TEXT NOT NULL DEFAULT (to_char(clock_timestamp() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"')),
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
  workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  issue_id BIGINT NOT NULL,
  fingerprint TEXT NOT NULL,
  PRIMARY KEY (issue_id, fingerprint),
  FOREIGN KEY(workspace_id,issue_id) REFERENCES tasks(workspace_id,id) ON DELETE CASCADE
);
CREATE INDEX idx_tasks_attention ON tasks(project_id, issue_kind, area)
  WHERE deleted_at IS NULL AND (issue_kind <> '' OR area = 'issue');
CREATE TABLE html_documents (
  workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  task_id BIGINT,
  page_id BIGINT,
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
  workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  document_id BIGINT NOT NULL,
  version INTEGER NOT NULL,
  author TEXT NOT NULL,
  created_at TEXT NOT NULL,
  html TEXT NOT NULL,
  PRIMARY KEY (document_id, version),
  FOREIGN KEY(workspace_id,document_id) REFERENCES html_documents(workspace_id,id) ON DELETE CASCADE
);

-- Keep demo usage proportional to the changed rows. A transaction that rolls back
-- also rolls back its counters. Archived workspaces remain in the owner's sum.
ALTER TABLE workspaces ADD COLUMN content_bytes BIGINT NOT NULL DEFAULT 0;
CREATE FUNCTION account_workspace_bytes() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE delta bigint := 0;
BEGIN
  IF TG_OP <> 'DELETE' THEN
    delta := octet_length(pg_catalog.record_send(NEW));
    IF TG_TABLE_NAME IN ('projects','tasks') THEN
      delta := delta - coalesce(octet_length(NEW.deleted_at),0);
    ELSIF TG_TABLE_NAME = 'activity' THEN
      IF NEW.action IN ('deleted','project_deleted','html_deleted') THEN delta := 0; END IF;
    ELSIF TG_TABLE_NAME = 'settings' THEN
      IF NEW.key='mcp_token_last_used_at' THEN delta := 0; END IF;
    END IF;
  END IF;
  IF TG_OP <> 'INSERT' THEN
    IF TG_TABLE_NAME IN ('projects','tasks') THEN
      delta := delta - octet_length(pg_catalog.record_send(OLD)) + coalesce(octet_length(OLD.deleted_at),0);
    ELSIF TG_TABLE_NAME='activity' THEN
      IF OLD.action NOT IN ('deleted','project_deleted','html_deleted') THEN delta := delta - octet_length(pg_catalog.record_send(OLD)); END IF;
    ELSIF TG_TABLE_NAME='settings' THEN
      IF OLD.key <> 'mcp_token_last_used_at' THEN delta := delta - octet_length(pg_catalog.record_send(OLD)); END IF;
    ELSE
      delta := delta - octet_length(pg_catalog.record_send(OLD));
    END IF;
  END IF;
  IF delta <> 0 THEN
    UPDATE workspaces SET content_bytes=content_bytes+delta WHERE id=coalesce(NEW.workspace_id,OLD.workspace_id);
  END IF;
  RETURN NULL;
END;
$$;
DO $$
DECLARE tbl text;
BEGIN
  FOREACH tbl IN ARRAY ARRAY['projects','tasks','statuses','labels','custom_fields','saved_views','notes','activity','settings','pages','issue_reports','html_documents','html_revisions']
  LOOP
    EXECUTE format('CREATE TRIGGER account_bytes AFTER INSERT OR UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION account_workspace_bytes()',tbl);
  END LOOP;
END;
$$;
