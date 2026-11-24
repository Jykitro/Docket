ALTER TABLE tasks ADD COLUMN issue_kind TEXT NOT NULL DEFAULT '' CHECK (issue_kind IN ('','problem','unverified','decision','blocker'));
ALTER TABLE tasks ADD COLUMN attention_after TEXT;

CREATE TABLE issue_reports (
  issue_id BIGINT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  fingerprint TEXT NOT NULL,
  PRIMARY KEY (issue_id, fingerprint)
);
CREATE INDEX idx_tasks_attention ON tasks(project_id, issue_kind, area)
  WHERE deleted_at IS NULL AND (issue_kind <> '' OR area = 'issue');
