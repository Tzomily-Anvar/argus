-- Argus sprint report, initial schema.
--
-- Constraints are deliberate. The application could check most of these
-- itself, but a database that rejects impossible data is one fewer thing
-- to get right in Go, and these rules outlive any particular version of
-- the code.

-- +goose Up
-- +goose StatementBegin

CREATE TABLE people (
    account_id TEXT PRIMARY KEY,
    name       TEXT NOT NULL CHECK (name <> ''),

    -- Story points expected in a full sprint. A planning figure, not a
    -- target, and never compared between people.
    baseline   NUMERIC(6,2) NOT NULL CHECK (baseline >= 0),

    -- Someone who has left is deactivated rather than deleted, so their
    -- sprint history stays intact.
    active     BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Keyed on Jira's own sprint id: a sprint number is not unique, because
-- boards renumber and two boards can each have a "Sprint 21". The number
-- is a label, and the only one of the two a published report shows.
CREATE TABLE sprints (
    jira_id    BIGINT PRIMARY KEY,
    label      TEXT NOT NULL,
    number     INTEGER NOT NULL,
    starts_at  TIMESTAMPTZ,
    ends_at    TIMESTAMPTZ,
    state      TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CHECK (ends_at IS NULL OR starts_at IS NULL OR ends_at >= starts_at)
);

CREATE INDEX sprints_by_number ON sprints (number DESC);
CREATE INDEX sprints_by_end    ON sprints (ends_at);

-- One person's availability for one sprint.
--
-- Absence is split by whether it was foreseeable when the baseline was
-- set, never by why it happened. Planned leave should already be in the
-- plan; unplanned absence is what explains a shortfall. Storing the
-- reason would add no analytical signal and would make this health data.
CREATE TABLE capacity (
    sprint_jira_id     BIGINT NOT NULL REFERENCES sprints(jira_id)  ON DELETE CASCADE,
    account_id         TEXT   NOT NULL REFERENCES people(account_id) ON DELETE CASCADE,
    planned_days_off   NUMERIC(5,2) NOT NULL DEFAULT 0 CHECK (planned_days_off   >= 0),
    unplanned_days_off NUMERIC(5,2) NOT NULL DEFAULT 0 CHECK (unplanned_days_off >= 0),

    -- Capacity is pre-filled from the baseline, which is usually right and
    -- occasionally very wrong. Without this there is no way to tell a
    -- confirmed figure from an untouched default, and the publish preview
    -- warns when it is false.
    reviewed   BOOLEAN NOT NULL DEFAULT FALSE,

    -- Free text about a named person. Local by default; it reaches a
    -- published page only when explicitly chosen at publish time.
    note       TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (sprint_jira_id, account_id)
);

-- One row per sprint, so trends can be drawn without re-fetching years of
-- Jira history. Deliberately free of personal data: this table is what
-- survives retention, so it must stay safe to keep indefinitely.
CREATE TABLE sprint_stats (
    sprint_jira_id     BIGINT PRIMARY KEY REFERENCES sprints(jira_id) ON DELETE CASCADE,
    baseline_total     NUMERIC(8,2) NOT NULL DEFAULT 0,
    capacity_total     NUMERIC(8,2) NOT NULL DEFAULT 0,
    delivered_total    NUMERIC(8,2) NOT NULL DEFAULT 0,
    planned_days_off   NUMERIC(8,2) NOT NULL DEFAULT 0,
    unplanned_days_off NUMERIC(8,2) NOT NULL DEFAULT 0,
    promised           INTEGER NOT NULL DEFAULT 0,
    injected           INTEGER NOT NULL DEFAULT 0,
    completed          INTEGER NOT NULL DEFAULT 0,
    stories_done       INTEGER NOT NULL DEFAULT 0,
    story_points_done  NUMERIC(8,2) NOT NULL DEFAULT 0,

    -- Points per epic class (Run, Build, whatever a team configures).
    -- JSONB rather than a table because the classes are user-defined and
    -- only ever read as a whole; GIN indexing keeps it queryable anyway.
    by_epic_class      JSONB NOT NULL DEFAULT '{}'::jsonb,

    recorded_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX sprint_stats_epic_class ON sprint_stats USING GIN (by_epic_class);

-- Every write Argus makes to Jira or Confluence. Anything that alters
-- someone else's data should leave a record of what it altered, so a
-- bulk edit can be explained or reversed afterwards. Nothing writes yet:
-- the write surface is being built behind a gate that is off by default,
-- and this table is here so the first write has somewhere to land.
CREATE TABLE write_log (
    id        BIGSERIAL PRIMARY KEY,
    at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    operation TEXT NOT NULL CHECK (operation <> ''),
    target    TEXT NOT NULL,
    before    TEXT NOT NULL DEFAULT '',
    after     TEXT NOT NULL DEFAULT '',
    actor     TEXT NOT NULL DEFAULT '',
    note      TEXT NOT NULL DEFAULT ''
);

CREATE INDEX write_log_at ON write_log (at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS write_log;
DROP TABLE IF EXISTS sprint_stats;
DROP TABLE IF EXISTS capacity;
DROP TABLE IF EXISTS sprints;
DROP TABLE IF EXISTS people;
-- +goose StatementEnd
