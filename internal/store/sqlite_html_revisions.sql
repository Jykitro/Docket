-- Raw TEXT remains authoritative for existing revisions. New large revisions
-- store one gzip payload and its uncompressed byte count instead.
ALTER TABLE html_revisions ADD COLUMN html_gzip BLOB;
ALTER TABLE html_revisions ADD COLUMN html_raw_bytes INTEGER CHECK (
  (html_gzip IS NULL AND html_raw_bytes IS NULL) OR
  (typeof(html_gzip) = 'blob' AND html = '' AND typeof(html_raw_bytes) = 'integer'
   AND html_raw_bytes BETWEEN 1 AND 2097152)
);

-- Older readers must refuse this file before they can return empty revision bodies.
CREATE TABLE plandillo_installation_v3(version INTEGER PRIMARY KEY CHECK(version=3), last_backup_at TEXT);
INSERT INTO plandillo_installation_v3(version,last_backup_at) SELECT 3,last_backup_at FROM plandillo_installation;
DROP TABLE plandillo_installation;
ALTER TABLE plandillo_installation_v3 RENAME TO plandillo_installation;
