-- A deleted project goes to the trash (hidden everywhere, restorable) until the owner empties the trash.
ALTER TABLE projects ADD COLUMN deleted_at TEXT;
