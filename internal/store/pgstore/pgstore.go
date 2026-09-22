// Package pgstore keeps Argus's data in Postgres.
//
// This is the opt-in backend, for keeping years of sprint history and
// querying across it. The default remains files, so nobody has to run a
// database to use the tool.
//
// Migrations are embedded in the binary and applied on startup, so there
// is nothing to install and no separate step to remember: point Argus at
// a database and it brings the schema up to date itself.
package pgstore

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/Tzomily-Anvar/argus/internal/store"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Postgres error codes we translate rather than surface raw.
const (
	codeForeignKeyViolation = "23503"
)

// Store implements store.Store over Postgres.
type Store struct {
	db *sql.DB
}

// New opens a pool against a libpq-style connection string.
func New(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parsing DATABASE_URL: %w", err)
	}
	db := stdlib.OpenDB(*cfg)

	// A dashboard makes a handful of queries at a time; an unbounded pool
	// would be a way to exhaust the server's connection limit for no gain.
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connecting to Postgres: %w", err)
	}
	return &Store{db: db}, nil
}

// Migrate applies any unapplied migrations. Safe to call on every start.
func (s *Store) Migrate(ctx context.Context) error {
	goose.SetBaseFS(migrations)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("setting dialect: %w", err)
	}
	if err := goose.UpContext(ctx, s.db, "migrations"); err != nil {
		return fmt.Errorf("applying migrations: %w", err)
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

// refErr turns a foreign-key violation into the interface's own error, so
// callers get the same behaviour here as from the file backend.
func refErr(err error, what string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == codeForeignKeyViolation {
		return fmt.Errorf("%s: %w", what, store.ErrUnknownReference)
	}
	return err
}

// ---- people ----------------------------------------------------------

func (s *Store) PutPerson(ctx context.Context, p store.Person) error {
	if p.AccountID == "" {
		return fmt.Errorf("person needs an account id")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO people (account_id, name, baseline, active, updated_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (account_id) DO UPDATE SET
			name = EXCLUDED.name,
			baseline = EXCLUDED.baseline,
			active = EXCLUDED.active,
			updated_at = now()`,
		p.AccountID, p.Name, p.Baseline, p.Active)
	return err
}

func (s *Store) GetPerson(ctx context.Context, accountID string) (store.Person, error) {
	var p store.Person
	err := s.db.QueryRowContext(ctx, `
		SELECT account_id, name, baseline, active, updated_at
		FROM people WHERE account_id = $1`, accountID).
		Scan(&p.AccountID, &p.Name, &p.Baseline, &p.Active, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Person{}, store.ErrNotFound
	}
	return p, err
}

func (s *Store) ListPeople(ctx context.Context, includeInactive bool) ([]store.Person, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT account_id, name, baseline, active, updated_at
		FROM people
		WHERE $1 OR active
		ORDER BY name`, includeInactive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []store.Person{}
	for rows.Next() {
		var p store.Person
		if err := rows.Scan(&p.AccountID, &p.Name, &p.Baseline, &p.Active, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) DeletePerson(ctx context.Context, accountID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM people WHERE account_id = $1`, accountID)
	return err
}

// ---- sprints ---------------------------------------------------------

func (s *Store) PutSprint(ctx context.Context, sp store.Sprint) error {
	if sp.JiraID == 0 {
		return fmt.Errorf("sprint needs a Jira id")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sprints (jira_id, label, number, starts_at, ends_at, state, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, now())
		ON CONFLICT (jira_id) DO UPDATE SET
			label = EXCLUDED.label, number = EXCLUDED.number,
			starts_at = EXCLUDED.starts_at, ends_at = EXCLUDED.ends_at,
			state = EXCLUDED.state, updated_at = now()`,
		sp.JiraID, sp.Label, sp.Number, sp.StartsAt, sp.EndsAt, sp.State)
	return err
}

func (s *Store) GetSprint(ctx context.Context, jiraID int64) (store.Sprint, error) {
	var sp store.Sprint
	err := s.db.QueryRowContext(ctx, `
		SELECT jira_id, label, number, starts_at, ends_at, state, updated_at
		FROM sprints WHERE jira_id = $1`, jiraID).
		Scan(&sp.JiraID, &sp.Label, &sp.Number, &sp.StartsAt, &sp.EndsAt, &sp.State, &sp.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Sprint{}, store.ErrNotFound
	}
	return sp, err
}

func (s *Store) ListSprints(ctx context.Context, limit int) ([]store.Sprint, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT jira_id, label, number, starts_at, ends_at, state, updated_at
		FROM sprints
		ORDER BY number DESC
		LIMIT NULLIF($1, 0)`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []store.Sprint{}
	for rows.Next() {
		var sp store.Sprint
		if err := rows.Scan(&sp.JiraID, &sp.Label, &sp.Number, &sp.StartsAt, &sp.EndsAt, &sp.State, &sp.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

// ---- capacity --------------------------------------------------------

func (s *Store) PutCapacity(ctx context.Context, c store.Capacity) error {
	if c.SprintJiraID == 0 || c.AccountID == "" {
		return fmt.Errorf("capacity needs a sprint id and an account id")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO capacity
			(sprint_jira_id, account_id, planned_days_off, unplanned_days_off, reviewed, note, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, now())
		ON CONFLICT (sprint_jira_id, account_id) DO UPDATE SET
			planned_days_off = EXCLUDED.planned_days_off,
			unplanned_days_off = EXCLUDED.unplanned_days_off,
			reviewed = EXCLUDED.reviewed,
			note = EXCLUDED.note,
			updated_at = now()`,
		c.SprintJiraID, c.AccountID, c.PlannedDaysOff, c.UnplannedDaysOff, c.Reviewed, c.Note)
	return refErr(err, fmt.Sprintf("sprint %d / person %s", c.SprintJiraID, c.AccountID))
}

func (s *Store) ListCapacity(ctx context.Context, sprintJiraID int64) ([]store.Capacity, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT sprint_jira_id, account_id, planned_days_off, unplanned_days_off,
		       reviewed, note, updated_at
		FROM capacity
		WHERE sprint_jira_id = $1
		ORDER BY account_id`, sprintJiraID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []store.Capacity{}
	for rows.Next() {
		var c store.Capacity
		if err := rows.Scan(&c.SprintJiraID, &c.AccountID, &c.PlannedDaysOff,
			&c.UnplannedDaysOff, &c.Reviewed, &c.Note, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Capacity reviews sit on the sprint, not on capacity: "everybody was
// available" is a statement about the sprint, and there is no per-person
// row to hang it on. The sprint upsert above deliberately leaves the
// column alone, so a sweep cannot clear a review.

func (s *Store) SetCapacityReviewed(ctx context.Context, sprintJiraID int64, reviewed bool) error {
	if sprintJiraID == 0 {
		return fmt.Errorf("a capacity review needs a sprint id")
	}
	var at *time.Time
	if reviewed {
		now := time.Now().UTC()
		at = &now
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE sprints SET capacity_reviewed_at = $2 WHERE jira_id = $1`,
		sprintJiraID, at)
	if err != nil {
		return err
	}
	// No foreign key to violate here, so the missing sprint has to be
	// spotted from the row count to match the file backend.
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return fmt.Errorf("sprint %d: %w", sprintJiraID, store.ErrUnknownReference)
	}
	return nil
}

func (s *Store) CapacityReviewedAt(ctx context.Context, sprintJiraID int64) (time.Time, error) {
	var at *time.Time
	err := s.db.QueryRowContext(ctx,
		`SELECT capacity_reviewed_at FROM sprints WHERE jira_id = $1`, sprintJiraID).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, fmt.Errorf("sprint %d: %w", sprintJiraID, store.ErrUnknownReference)
	}
	if err != nil {
		return time.Time{}, err
	}
	if at == nil {
		return time.Time{}, nil
	}
	return at.UTC(), nil
}

// ---- stats -----------------------------------------------------------

func (s *Store) PutStats(ctx context.Context, st store.SprintStats) error {
	if st.SprintJiraID == 0 {
		return fmt.Errorf("stats need a sprint id")
	}
	byEpic, err := json.Marshal(st.ByEpicClass)
	if err != nil {
		return fmt.Errorf("encoding epic classes: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO sprint_stats
			(sprint_jira_id, baseline_total, capacity_total, delivered_total,
			 planned_days_off, unplanned_days_off, promised, injected, completed,
			 stories_done, story_points_done, by_epic_class, recorded_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12, now())
		ON CONFLICT (sprint_jira_id) DO UPDATE SET
			baseline_total = EXCLUDED.baseline_total,
			capacity_total = EXCLUDED.capacity_total,
			delivered_total = EXCLUDED.delivered_total,
			planned_days_off = EXCLUDED.planned_days_off,
			unplanned_days_off = EXCLUDED.unplanned_days_off,
			promised = EXCLUDED.promised, injected = EXCLUDED.injected,
			completed = EXCLUDED.completed, stories_done = EXCLUDED.stories_done,
			story_points_done = EXCLUDED.story_points_done,
			by_epic_class = EXCLUDED.by_epic_class,
			recorded_at = now()`,
		st.SprintJiraID, st.BaselineTotal, st.CapacityTotal, st.DeliveredTotal,
		st.PlannedDaysOff, st.UnplannedDaysOff, st.Promised, st.Injected, st.Completed,
		st.StoriesDone, st.StoryPointsDone, byEpic)
	return refErr(err, fmt.Sprintf("sprint %d", st.SprintJiraID))
}

func (s *Store) ListStats(ctx context.Context, limit int) ([]store.SprintStats, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT sprint_jira_id, baseline_total, capacity_total, delivered_total,
		       planned_days_off, unplanned_days_off, promised, injected, completed,
		       stories_done, story_points_done, by_epic_class, recorded_at
		FROM sprint_stats
		ORDER BY sprint_jira_id DESC
		LIMIT NULLIF($1, 0)`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []store.SprintStats{}
	for rows.Next() {
		var st store.SprintStats
		var byEpic []byte
		if err := rows.Scan(&st.SprintJiraID, &st.BaselineTotal, &st.CapacityTotal,
			&st.DeliveredTotal, &st.PlannedDaysOff, &st.UnplannedDaysOff,
			&st.Promised, &st.Injected, &st.Completed, &st.StoriesDone,
			&st.StoryPointsDone, &byEpic, &st.RecordedAt); err != nil {
			return nil, err
		}
		if len(byEpic) > 0 {
			if err := json.Unmarshal(byEpic, &st.ByEpicClass); err != nil {
				return nil, fmt.Errorf("decoding epic classes: %w", err)
			}
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// ---- write audit -----------------------------------------------------

func (s *Store) AppendWrite(ctx context.Context, w store.WriteRecord) error {
	at := w.At
	if at.IsZero() {
		at = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO write_log (at, operation, target, before, after, actor, note)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		at, w.Operation, w.Target, w.Before, w.After, w.Actor, w.Note)
	return err
}

func (s *Store) ListWrites(ctx context.Context, limit int) ([]store.WriteRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, at, operation, target, before, after, actor, note
		FROM write_log
		ORDER BY at DESC, id DESC
		LIMIT NULLIF($1, 0)`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []store.WriteRecord{}
	for rows.Next() {
		var w store.WriteRecord
		if err := rows.Scan(&w.ID, &w.At, &w.Operation, &w.Target,
			&w.Before, &w.After, &w.Actor, &w.Note); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// ---- retention -------------------------------------------------------

// Prune drops per-person rows for sprints that ended before the cutoff and
// write-log entries older than it. sprint_stats is deliberately untouched:
// it holds no personal data and is what the trends are drawn from.
func (s *Store) Prune(ctx context.Context, before time.Time) (store.PruneResult, error) {
	var res store.PruneResult

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return res, err
	}
	defer func() { _ = tx.Rollback() }()

	cap, err := tx.ExecContext(ctx, `
		DELETE FROM capacity
		WHERE sprint_jira_id IN (
			SELECT jira_id FROM sprints WHERE ends_at IS NOT NULL AND ends_at < $1
		)`, before)
	if err != nil {
		return res, fmt.Errorf("pruning capacity: %w", err)
	}
	n, _ := cap.RowsAffected()
	res.CapacityRows = int(n)

	wl, err := tx.ExecContext(ctx, `DELETE FROM write_log WHERE at < $1`, before)
	if err != nil {
		return res, fmt.Errorf("pruning write log: %w", err)
	}
	n, _ = wl.RowsAffected()
	res.WriteRows = int(n)

	return res, tx.Commit()
}
