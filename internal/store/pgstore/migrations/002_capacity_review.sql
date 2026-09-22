-- Record that a sprint's capacity was reviewed and needed no adjustments.
--
-- A sprint where everybody was available produces no capacity rows, and
-- so does a sprint nobody has opened yet. Those are different facts, and
-- before this column the schema could not tell them apart.
--
-- It sits on sprints rather than on capacity because it is a statement
-- about the sprint: there is no per-person row to hang "needed no
-- adjustment" on for somebody who needed none. It is deliberately not
-- touched by the sprint upsert, which runs on every sweep and would
-- otherwise clear it.

-- +goose Up
-- +goose StatementBegin

ALTER TABLE sprints ADD COLUMN capacity_reviewed_at TIMESTAMPTZ;

COMMENT ON COLUMN sprints.capacity_reviewed_at IS
    'When a human last confirmed this sprint''s availability. NULL means nobody has; a timestamp with no capacity rows means everyone was at their baseline.';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE sprints DROP COLUMN capacity_reviewed_at;
-- +goose StatementEnd
