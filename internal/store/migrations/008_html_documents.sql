CREATE TABLE html_documents (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id INTEGER REFERENCES tasks(id) ON DELETE CASCADE,
  page_id INTEGER REFERENCES pages(id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  title_key TEXT NOT NULL,
  version INTEGER NOT NULL DEFAULT 0,
  CHECK ((task_id IS NULL) <> (page_id IS NULL)),
  UNIQUE (task_id, title_key),
  UNIQUE (page_id, title_key)
);
CREATE TABLE html_revisions (
  document_id INTEGER NOT NULL REFERENCES html_documents(id) ON DELETE CASCADE,
  version INTEGER NOT NULL,
  author TEXT NOT NULL,
  created_at TEXT NOT NULL,
  html TEXT NOT NULL,
  PRIMARY KEY (document_id, version)
);
