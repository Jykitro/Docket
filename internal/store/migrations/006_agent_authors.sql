-- Replace the inline actor checks without rebuilding task relationships or FTS triggers.

ALTER TABLE tasks ADD COLUMN agent_author TEXT NOT NULL DEFAULT 'mcp' CHECK (length(agent_author) BETWEEN 1 AND 48);
UPDATE tasks SET agent_author = created_by;
ALTER TABLE tasks DROP COLUMN created_by;
ALTER TABLE tasks RENAME COLUMN agent_author TO created_by;

ALTER TABLE notes ADD COLUMN agent_author TEXT NOT NULL DEFAULT 'mcp' CHECK (length(agent_author) BETWEEN 1 AND 48);
UPDATE notes SET agent_author = author;
ALTER TABLE notes DROP COLUMN author;
ALTER TABLE notes RENAME COLUMN agent_author TO author;

ALTER TABLE activity ADD COLUMN agent_author TEXT NOT NULL DEFAULT 'mcp' CHECK (length(agent_author) BETWEEN 1 AND 48);
UPDATE activity SET agent_author = actor;
ALTER TABLE activity DROP COLUMN actor;
ALTER TABLE activity RENAME COLUMN agent_author TO actor;
