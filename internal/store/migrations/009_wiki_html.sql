ALTER TABLE pages ADD COLUMN body_html TEXT CHECK (body_html IS NULL OR body_md = '');
