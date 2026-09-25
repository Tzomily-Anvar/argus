-- Remember which Confluence page a sprint's report was published to.
--
-- One page per sprint, found by title under a parent page the first
-- time and by this id thereafter, so a page somebody renames is still
-- found and a second page is never created by accident. Nullable: a
-- sprint never published has no page, and every sprint already in the
-- table is one of those.
--
-- The page's earlier versions are not kept here. Confluence keeps them
-- itself, and copying them would put the report's per-person figures
-- into a second place.

-- +goose Up
-- +goose StatementBegin

ALTER TABLE sprints ADD COLUMN confluence_page_id TEXT;

COMMENT ON COLUMN sprints.confluence_page_id IS
    'The Confluence page this sprint''s report was published to; NULL while it never has been.';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE sprints DROP COLUMN IF EXISTS confluence_page_id;
-- +goose StatementEnd
