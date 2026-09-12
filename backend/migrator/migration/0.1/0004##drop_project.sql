-- The project model was removed: there is no project API, no project message
-- and no Go caller of the table, and every database row carries the same
-- "default" project.
--
-- db.project is dropped first because it is the only foreign key into
-- project(resource_id); the column and its index go away with it. Anything that
-- read the column (the database/instance/user project filters, the
-- `Database.project` API field, DeleteInstance's move-to-default-project step)
-- was removed in the same change.
ALTER TABLE db DROP COLUMN IF EXISTS project;
DROP TABLE IF EXISTS project;
