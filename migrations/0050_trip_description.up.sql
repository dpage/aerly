-- A free-form, markdown-enabled description for a trip (issue #112): links to
-- photo albums, blog posts, packing lists, and any other notes that don't fit
-- the structured fields. Optional; empty means "no description".
ALTER TABLE trips ADD COLUMN description TEXT NOT NULL DEFAULT '';
