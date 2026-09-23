-- Keep a sprint's close-out draft between sittings.
--
-- Closing a sprint is several steps in one panel, and it gets interrupted.
-- Until now the queued changes lived in the browser, so a reload or a
-- restart lost them. One row per sprint holds the queue until it is
-- applied or discarded.
--
-- The queue is JSONB rather than a table of its own because it is only
-- ever read and written whole, by the panel that built it, and its rows
-- are the same shape the sprint package proposes from. Nothing queries
-- inside it.
--
-- It holds ticket keys and account ids, so it goes with the sprint: a
-- sprint that is dropped takes its draft with it, and retention prunes
-- it alongside the capacity rows.

-- +goose Up
-- +goose StatementBegin

CREATE TABLE drafts (
    sprint_jira_id BIGINT PRIMARY KEY REFERENCES sprints(jira_id) ON DELETE CASCADE,
    body           JSONB NOT NULL DEFAULT '[]'::jsonb,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE drafts IS
    'The changes queued for one sprint''s close-out and not yet applied. Working state, replaced whole on every save.';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS drafts;
-- +goose StatementEnd
