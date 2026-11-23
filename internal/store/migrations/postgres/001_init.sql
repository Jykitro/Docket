CREATE TABLE projects (
  id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  slug        TEXT NOT NULL UNIQUE,
  key         TEXT NOT NULL UNIQUE CHECK (key ~ '^[A-Z][A-Z0-9]{1,4}$'),
  name        TEXT NOT NULL,
  icon        TEXT NOT NULL DEFAULT 'board',
  color       TEXT NOT NULL DEFAULT '#2D5CC9',
  description TEXT NOT NULL DEFAULT '',
  task_seq    BIGINT NOT NULL DEFAULT 0,
  position    DOUBLE PRECISION NOT NULL,
  archived_at TEXT,
  created_at  TEXT NOT NULL DEFAULT (to_char(clock_timestamp() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"')),
  updated_at  TEXT NOT NULL DEFAULT (to_char(clock_timestamp() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"'))
);

CREATE TABLE statuses (
  id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  project_id    BIGINT NOT NULL REFERENCES projects(id),
  key           TEXT NOT NULL,
  name          TEXT NOT NULL,
  color         TEXT NOT NULL,
  category      TEXT NOT NULL CHECK (category IN ('todo','doing','done')),
  show_on_board BIGINT NOT NULL DEFAULT 1,
  position      DOUBLE PRECISION NOT NULL,
  UNIQUE (project_id, key)
);

CREATE TABLE labels (
  id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  project_id BIGINT NOT NULL REFERENCES projects(id),
  name       TEXT NOT NULL,
  color      TEXT NOT NULL,
  UNIQUE (project_id, name)
);

CREATE TABLE custom_fields (
  id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  project_id   BIGINT NOT NULL REFERENCES projects(id),
  name         TEXT NOT NULL,
  type         TEXT NOT NULL CHECK (type IN ('number','url','select')),
  options      TEXT NOT NULL DEFAULT '[]',
  show_on_card BIGINT NOT NULL DEFAULT 0,
  position     DOUBLE PRECISION NOT NULL,
  UNIQUE (project_id, name)
);

CREATE TABLE tasks (
  id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  project_id     BIGINT NOT NULL REFERENCES projects(id),
  number         BIGINT NOT NULL,
  parent_id      BIGINT REFERENCES tasks(id),
  title          TEXT NOT NULL,
  description_md TEXT NOT NULL DEFAULT '',
  status_id      BIGINT NOT NULL REFERENCES statuses(id),
  priority       TEXT NOT NULL DEFAULT 'P1' CHECK (priority IN ('P0','P1','P2')),
  area           TEXT NOT NULL DEFAULT '',
  planned_for    TEXT,
  position       DOUBLE PRECISION NOT NULL,
  custom         TEXT NOT NULL DEFAULT '{}',
  external_id    TEXT UNIQUE,
  created_by     TEXT NOT NULL CHECK (created_by IN ('denis','claude')),
  created_at     TEXT NOT NULL DEFAULT (to_char(clock_timestamp() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"')),
  updated_at     TEXT NOT NULL DEFAULT (to_char(clock_timestamp() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"')),
  done_at        TEXT,
  deleted_at     TEXT,
  UNIQUE (project_id, number)
);
CREATE INDEX idx_tasks_board ON tasks(project_id, status_id, position) WHERE deleted_at IS NULL;
CREATE INDEX idx_tasks_day   ON tasks(planned_for) WHERE deleted_at IS NULL;

CREATE TABLE saved_views (
  id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  project_id  BIGINT REFERENCES projects(id),
  name        TEXT NOT NULL,
  layout      TEXT NOT NULL CHECK (layout IN ('board','list')),
  filter_json TEXT NOT NULL DEFAULT '{}',
  group_by    TEXT NOT NULL DEFAULT 'status',
  sort        TEXT NOT NULL DEFAULT 'manual',
  pinned      BIGINT NOT NULL DEFAULT 0,
  position    DOUBLE PRECISION NOT NULL
);

CREATE TABLE notes (
  id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  task_id    BIGINT NOT NULL REFERENCES tasks(id),
  author     TEXT NOT NULL CHECK (author IN ('denis','claude')),
  body_md    TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (to_char(clock_timestamp() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"'))
);

CREATE TABLE activity (
  id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  task_id    BIGINT,
  project_id BIGINT,
  actor      TEXT NOT NULL CHECK (actor IN ('denis','claude')),
  action     TEXT NOT NULL,
  diff_json  TEXT NOT NULL DEFAULT '{}',
  at         TEXT NOT NULL DEFAULT (to_char(clock_timestamp() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"'))
);
CREATE INDEX idx_activity_task ON activity(task_id, at);

CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);

CREATE TABLE mcp_calls (
  id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  at          TEXT NOT NULL,
  tool        TEXT NOT NULL,
  args_summary TEXT NOT NULL,
  duration_ms BIGINT NOT NULL,
  status      TEXT NOT NULL CHECK (status IN ('ok','error')),
  error_code  TEXT,
  actor       TEXT NOT NULL DEFAULT 'claude'
);
CREATE INDEX idx_mcp_calls_at ON mcp_calls(at);

-- The simple dictionary keeps Russian, Ukrainian and English prefixes without stemming.
CREATE INDEX idx_tasks_search ON tasks USING gin(to_tsvector('simple', title || ' ' || description_md));
CREATE INDEX idx_tasks_title_search ON tasks USING gin(to_tsvector('simple', title));
CREATE INDEX idx_tasks_description_search ON tasks USING gin(to_tsvector('simple', description_md));
