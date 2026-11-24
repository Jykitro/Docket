ALTER TABLE tasks DROP CONSTRAINT tasks_created_by_check,
  ADD CHECK (length(created_by) BETWEEN 1 AND 48),
  ALTER COLUMN created_by SET DEFAULT 'mcp';
ALTER TABLE notes DROP CONSTRAINT notes_author_check,
  ADD CHECK (length(author) BETWEEN 1 AND 48),
  ALTER COLUMN author SET DEFAULT 'mcp';
ALTER TABLE activity DROP CONSTRAINT activity_actor_check,
  ADD CHECK (length(actor) BETWEEN 1 AND 48),
  ALTER COLUMN actor SET DEFAULT 'mcp';
