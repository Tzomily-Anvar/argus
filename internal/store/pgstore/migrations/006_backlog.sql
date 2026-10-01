-- What the backlog tool remembers: acknowledgements and the requesters.
--
-- An acknowledgement is a watermark on one item - a ticket in the
-- backlog view or an entry in the personal inbox - holding the item's
-- updated timestamp as Jira gave it. The item stays hidden while the two
-- agree and comes back the moment it changes. Nothing is written to Jira
-- for it, which is why it has to live here.
--
-- The watermark is text rather than a timestamp on purpose: it is never
-- compared as a time, only for equality with the next reading, and
-- parsing it would only introduce a way for the two to disagree.
--
-- A row records what one person looked at and when, and its key is
-- somebody else's ticket or page. Retention drops rows older than ninety
-- days, on the row's own clock rather than the sprint cutoff.
--
-- The requesters are the people outside the team whose tickets are
-- requests, by account id - typically an operations or support team,
-- though nothing here assumes so. Kept in the app because the label that
-- should mark their tickets is applied by hand and forgotten, and the
-- reporter is the one signal that is not.

-- +goose Up
-- +goose StatementBegin

CREATE TABLE acks (
    kind      TEXT NOT NULL CHECK (kind IN ('ticket', 'inbox')),
    key       TEXT NOT NULL CHECK (key <> ''),
    watermark TEXT NOT NULL DEFAULT '',
    at        TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (kind, key)
);

COMMENT ON TABLE acks IS
    'What one person has acknowledged in the backlog tool: the item''s updated timestamp as Jira gave it, so the item hides until it changes.';

-- Retention deletes by age.
CREATE INDEX acks_by_at ON acks (at);

CREATE TABLE requesters (
    account_id TEXT PRIMARY KEY,
    name       TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE requesters IS
    'People outside the team by Jira account id. A ticket one of them reported is a request whether or not it carries the request label.';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS requesters;
DROP INDEX IF EXISTS acks_by_at;
DROP TABLE IF EXISTS acks;
-- +goose StatementEnd
