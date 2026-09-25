-- Give each write-log row the context a reversal needs.
--
-- A bulk edit is one action to the person who approved it and a dozen
-- rows to the log. Without something tying the rows together, reading
-- the edit back afterwards means guessing from timestamps, and reversing
-- it means guessing which rows belonged to it. change_set is that tie.
--
-- outcome records whether the write landed. A log that only holds
-- successes cannot say what happened to a half-applied batch, which is
-- precisely when somebody wants to know; so a write that was skipped by
-- its guard or rejected by Jira leaves a row too.
--
-- Both are nullable rather than defaulted to '', because a row written
-- before this migration genuinely has neither: it was not part of a
-- change set and its outcome was never recorded. The store reads NULL
-- as the empty string, so the two backends agree.

-- +goose Up
-- +goose StatementBegin

ALTER TABLE write_log ADD COLUMN change_set TEXT;
ALTER TABLE write_log ADD COLUMN outcome    TEXT;

COMMENT ON COLUMN write_log.change_set IS
    'Groups the rows of one bulk edit so they can be read or reversed as one action. NULL for a write that was not part of a batch.';
COMMENT ON COLUMN write_log.outcome IS
    'applied, skipped or failed. NULL on rows written before the outcome was recorded.';

-- Reversal reads a whole change set at once, so it is looked up by this
-- and not by time.
CREATE INDEX write_log_change_set ON write_log (change_set);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS write_log_change_set;
ALTER TABLE write_log DROP COLUMN outcome;
ALTER TABLE write_log DROP COLUMN change_set;
-- +goose StatementEnd
