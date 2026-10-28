-- Pull requests marked seen on the dashboard.
--
-- The same watermark as a backlog acknowledgement, under a third kind:
-- the key is repo#number and the watermark the head commit the person
-- looked at, so a push brings the pull request back as changed. Nothing
-- is written to GitHub for it.

-- +goose Up
-- +goose StatementBegin

ALTER TABLE acks DROP CONSTRAINT acks_kind_check;
ALTER TABLE acks ADD CONSTRAINT acks_kind_check CHECK (kind IN ('ticket', 'inbox', 'pr'));

COMMENT ON TABLE acks IS
    'What one person has acknowledged: a backlog item''s updated timestamp as Jira gave it, or a pull request''s head commit, so the item hides until it changes.';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM acks WHERE kind = 'pr';
ALTER TABLE acks DROP CONSTRAINT acks_kind_check;
ALTER TABLE acks ADD CONSTRAINT acks_kind_check CHECK (kind IN ('ticket', 'inbox'));
-- +goose StatementEnd
