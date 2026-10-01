-- The labels a team cares about, for the backlog tool's bulk bar.
--
-- Jira holds hundreds of labels across a site, most of them typed once
-- and never again. The bulk bar offers the few this team actually
-- applies, chosen behind the gear, so adding or removing one is a pick
-- rather than a spelling. The list is the team's own; nothing here
-- creates or deletes a label in Jira.

-- +goose Up
-- +goose StatementBegin

CREATE TABLE backlog_labels (
    name       TEXT PRIMARY KEY CHECK (name <> ''),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE backlog_labels IS
    'The labels this team applies from the backlog tool''s bulk bar. A choice of what to offer, not a record of what Jira holds.';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS backlog_labels;
-- +goose StatementEnd
