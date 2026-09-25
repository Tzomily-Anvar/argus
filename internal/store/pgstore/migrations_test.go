package pgstore

// Migrations are applied automatically on startup, against a database
// that already holds a team's roster, their baselines and years of
// absence records. That convenience is only defensible if the upgrade
// path is tested, not just the fresh install: a migration that works on
// an empty database and drops a column on a populated one would look
// perfectly healthy in CI and destroy the only copy of data nobody can
// reconstruct.
//
// So these tests cover both directions a real database arrives from:
// nothing at all, and an installation sitting on an earlier migration.
//
// Each test gets its own Postgres schema, so it neither sees nor disturbs
// the conformance suite running in the same database.
//
// Skipped without ARGUS_TEST_DATABASE_URL, matching the convention in
// pgstore_test.go, so `go test ./...` still works with no database about.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/Tzomily-Anvar/argus/internal/store"
)

func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("ARGUS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set ARGUS_TEST_DATABASE_URL to run the Postgres migration tests")
	}
	return dsn
}

// isolated opens a store confined to a schema of its own, so a migration
// test can create and drop the whole schema without touching anything
// else in the database.
func isolated(t *testing.T) *Store {
	t.Helper()
	dsn := testDSN(t)
	ctx := context.Background()

	schema := fmt.Sprintf("argus_mig_%d", time.Now().UnixNano())

	admin, err := openAt(dsn, "")
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	if _, err := admin.db.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		admin.Close()
		t.Fatalf("creating schema %s: %v", schema, err)
	}

	s, err := openAt(dsn, schema)
	if err != nil {
		admin.Close()
		t.Fatalf("connecting to schema %s: %v", schema, err)
	}
	t.Cleanup(func() {
		_ = s.Close()
		if _, err := admin.db.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("dropping schema %s: %v", schema, err)
		}
		admin.Close()
	})
	return s
}

func openAt(dsn, schema string) (*Store, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	if schema != "" {
		cfg.RuntimeParams["search_path"] = schema
	}
	db := stdlib.OpenDB(*cfg)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// latestMigration is the highest version on disk, so adding 003 does not
// mean editing the assertion below - only that the tests then insist 003
// is applied too.
func latestMigration(t *testing.T) int64 {
	t.Helper()
	versions := migrationVersions(t)
	return versions[len(versions)-1]
}

func migrationVersions(t *testing.T) []int64 {
	t.Helper()
	entries, err := fs.ReadDir(migrations, "migrations")
	if err != nil {
		t.Fatalf("reading embedded migrations: %v", err)
	}
	var out []int64
	for _, e := range entries {
		prefix, _, ok := strings.Cut(e.Name(), "_")
		if !ok {
			t.Fatalf("migration %q is not named <version>_<name>.sql", e.Name())
		}
		v, err := strconv.ParseInt(prefix, 10, 64)
		if err != nil {
			t.Fatalf("migration %q has no numeric version: %v", e.Name(), err)
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		t.Fatal("no migrations are embedded in the binary")
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func gooseTo(t *testing.T, s *Store, version int64) {
	t.Helper()
	goose.SetBaseFS(migrations)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("setting dialect: %v", err)
	}
	if err := goose.UpToContext(context.Background(), s.db, "migrations", version); err != nil {
		t.Fatalf("migrating up to %d: %v", version, err)
	}
}

func dbVersion(t *testing.T, s *Store) int64 {
	t.Helper()
	goose.SetBaseFS(migrations)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("setting dialect: %v", err)
	}
	v, err := goose.GetDBVersionContext(context.Background(), s.db)
	if err != nil {
		t.Fatalf("reading the applied version: %v", err)
	}
	return v
}

func hasColumn(t *testing.T, s *Store, table, column string) bool {
	t.Helper()
	var n int
	err := s.db.QueryRowContext(context.Background(), `
		SELECT count(*) FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = $1 AND column_name = $2`, table, column).Scan(&n)
	if err != nil {
		t.Fatalf("looking up %s.%s: %v", table, column, err)
	}
	return n == 1
}

func hasTable(t *testing.T, s *Store, table string) bool {
	t.Helper()
	var n int
	err := s.db.QueryRowContext(context.Background(), `
		SELECT count(*) FROM information_schema.tables
		WHERE table_schema = current_schema() AND table_name = $1`, table).Scan(&n)
	if err != nil {
		t.Fatalf("looking up table %s: %v", table, err)
	}
	return n == 1
}

func hasIndex(t *testing.T, s *Store, index string) bool {
	t.Helper()
	var n int
	err := s.db.QueryRowContext(context.Background(), `
		SELECT count(*) FROM pg_indexes
		WHERE schemaname = current_schema() AND indexname = $1`, index).Scan(&n)
	if err != nil {
		t.Fatalf("looking up index %s: %v", index, err)
	}
	return n == 1
}

// Migration versions must be unique and gapless. A duplicated number is
// the kind of merge accident that leaves one of the two migrations
// silently never applied.
func TestMigrationsAreNumberedSequentially(t *testing.T) {
	versions := migrationVersions(t)
	for i, v := range versions {
		if want := int64(i + 1); v != want {
			t.Errorf("migration versions are %v: expected %d at position %d. "+
				"A duplicated or skipped number means goose can consider a "+
				"migration applied that never ran.", versions, want, i)
		}
	}
}

// The fresh install: nothing in the database at all.
func TestMigrateFromEmpty(t *testing.T) {
	s := isolated(t)
	ctx := context.Background()

	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("migrating an empty database: %v", err)
	}
	if got, want := dbVersion(t, s), latestMigration(t); got != want {
		t.Errorf("applied version %d, want %d: a migration on disk was not applied", got, want)
	}

	for _, table := range []string{"people", "sprints", "capacity", "sprint_stats", "write_log", "drafts"} {
		var n int
		if err := s.db.QueryRowContext(ctx, `
			SELECT count(*) FROM information_schema.tables
			WHERE table_schema = current_schema() AND table_name = $1`, table).Scan(&n); err != nil {
			t.Fatalf("looking up %s: %v", table, err)
		}
		if n != 1 {
			t.Errorf("table %s was not created", table)
		}
	}
	if !hasColumn(t, s, "sprints", "capacity_reviewed_at") {
		t.Error("sprints.capacity_reviewed_at missing: 002 was not applied")
	}
	for _, col := range []string{"change_set", "outcome"} {
		if !hasColumn(t, s, "write_log", col) {
			t.Errorf("write_log.%s missing: 003 was not applied", col)
		}
	}
	if !hasIndex(t, s, "write_log_change_set") {
		t.Error("write_log_change_set index missing: 003 was not applied")
	}
	if !hasColumn(t, s, "sprints", "confluence_page_id") {
		t.Error("sprints.confluence_page_id missing: 005 was not applied")
	}

	// Startup runs this every time, so running it twice must be a no-op.
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("migrating a second time: %v", err)
	}
	if got, want := dbVersion(t, s), latestMigration(t); got != want {
		t.Errorf("version moved to %d on a repeat migration, want %d", got, want)
	}
}

// The upgrade path, which is the one that can lose data: a database
// already holding a team's roster, sitting on 001, is brought to the
// latest migration.
func TestUpgradeFrom001PreservesData(t *testing.T) {
	s := isolated(t)
	ctx := context.Background()

	gooseTo(t, s, 1)
	if got := dbVersion(t, s); got != 1 {
		t.Fatalf("expected to be on 001, got %d", got)
	}
	if hasColumn(t, s, "sprints", "capacity_reviewed_at") {
		t.Fatal("001 should not have the 002 column; the test is not testing an upgrade")
	}
	if hasColumn(t, s, "write_log", "change_set") {
		t.Fatal("001 should not have the 003 columns; the test is not testing an upgrade")
	}

	// Written with raw SQL rather than through the store, because the
	// store's queries already know about 002 - and the point is to leave
	// behind exactly what an installation on 001 would have.
	starts := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	ends := time.Date(2026, 1, 16, 17, 0, 0, 0, time.UTC)
	mustExec(t, s, `INSERT INTO people (account_id, name, baseline, active)
		VALUES ('acc-a', 'Person A', 12.5, true), ('acc-b', 'Person B', 8, false)`)
	mustExec(t, s, `INSERT INTO sprints (jira_id, label, number, starts_at, ends_at, state)
		VALUES ($1, 'Sprint 21', 21, $2, $3, 'closed')`, int64(744), starts, ends)
	mustExec(t, s, `INSERT INTO capacity
		(sprint_jira_id, account_id, planned_days_off, unplanned_days_off, reviewed, note)
		VALUES (744, 'acc-a', 2, 1.5, true, 'Two days booked leave, then off sick.')`)
	mustExec(t, s, `INSERT INTO sprint_stats
		(sprint_jira_id, baseline_total, capacity_total, delivered_total,
		 planned_days_off, unplanned_days_off, promised, injected, completed,
		 stories_done, story_points_done, by_epic_class)
		VALUES (744, 20.5, 17, 16, 2, 1.5, 9, 3, 8, 4, 13, '{"Build":11,"Run":5}'::jsonb)`)
	mustExec(t, s, `INSERT INTO write_log (operation, target, before, after, actor)
		VALUES ('set_points', 'ABC-123', '3', '5', 'argus')`)

	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("upgrading a populated 001 database: %v", err)
	}
	if got, want := dbVersion(t, s), latestMigration(t); got != want {
		t.Errorf("applied version %d, want %d", got, want)
	}

	people, err := s.ListPeople(ctx, true)
	if err != nil {
		t.Fatalf("ListPeople: %v", err)
	}
	if len(people) != 2 {
		t.Fatalf("the roster did not survive the upgrade: %d people", len(people))
	}
	if people[0].AccountID != "acc-a" || people[0].Baseline != 12.5 || !people[0].Active {
		t.Errorf("person A changed across the upgrade: %+v", people[0])
	}
	if people[1].Active {
		t.Errorf("person B should still be inactive: %+v", people[1])
	}

	sp, err := s.GetSprint(ctx, 744)
	if err != nil {
		t.Fatalf("GetSprint: %v", err)
	}
	if sp.Label != "Sprint 21" || sp.Number != 21 || sp.State != "closed" {
		t.Errorf("the sprint changed across the upgrade: %+v", sp)
	}
	if sp.StartsAt == nil || !sp.StartsAt.Equal(starts) || sp.EndsAt == nil || !sp.EndsAt.Equal(ends) {
		t.Errorf("sprint dates changed across the upgrade: %+v", sp)
	}

	rows, err := s.ListCapacity(ctx, 744)
	if err != nil {
		t.Fatalf("ListCapacity: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("capacity did not survive the upgrade: %d rows", len(rows))
	}
	if rows[0].PlannedDaysOff != 2 || rows[0].UnplannedDaysOff != 1.5 || !rows[0].Reviewed || rows[0].Note == "" {
		t.Errorf("capacity changed across the upgrade: %+v", rows[0])
	}

	stats, err := s.ListStats(ctx, 0)
	if err != nil {
		t.Fatalf("ListStats: %v", err)
	}
	if len(stats) != 1 || stats[0].DeliveredTotal != 16 || stats[0].ByEpicClass["Build"] != 11 {
		t.Errorf("stats changed across the upgrade: %+v", stats)
	}

	writes, err := s.ListWrites(ctx, 0)
	if err != nil {
		t.Fatalf("ListWrites: %v", err)
	}
	if len(writes) != 1 || writes[0].Target != "ABC-123" || writes[0].After != "5" {
		t.Errorf("the write log changed across the upgrade: %+v", writes)
	}
	// A row from before 003 has no change set and no recorded outcome,
	// and must say so rather than invent either.
	if writes[0].ChangeSet != "" || writes[0].Outcome != "" {
		t.Errorf("a write carried over from 001 should have empty context, got %+v", writes[0])
	}

	// A sprint that predates the column reads as never reviewed, which is
	// the truthful answer: nobody had anywhere to record it.
	at, err := s.CapacityReviewedAt(ctx, 744)
	if err != nil {
		t.Fatalf("CapacityReviewedAt: %v", err)
	}
	if !at.IsZero() {
		t.Errorf("a sprint carried over from 001 should read as unreviewed, got %v", at)
	}

	// And the new column has to actually work on the upgraded database,
	// not just exist on it.
	if err := s.SetCapacityReviewed(ctx, 744, true); err != nil {
		t.Fatalf("SetCapacityReviewed after the upgrade: %v", err)
	}
	if at, err := s.CapacityReviewedAt(ctx, 744); err != nil || at.IsZero() {
		t.Errorf("the review did not stick after the upgrade: %v, err %v", at, err)
	}
	if err := s.AppendWrite(ctx, store.WriteRecord{
		Operation: "points.set", Target: "ABC-124", After: "3", Actor: "account-a",
		ChangeSet: "cs-1", Outcome: store.OutcomeApplied,
	}); err != nil {
		t.Fatalf("AppendWrite after the upgrade: %v", err)
	}
	writes, err = s.ListWrites(ctx, 0)
	if err != nil {
		t.Fatalf("ListWrites after the upgrade: %v", err)
	}
	if len(writes) != 2 || writes[0].ChangeSet != "cs-1" || writes[0].Outcome != store.OutcomeApplied {
		t.Errorf("the 003 columns did not take a write on the upgraded database: %+v", writes)
	}

	// 004's table has to take a draft against the sprint carried over,
	// and the foreign key has to hold: a draft for a sprint the database
	// does not know is refused the way capacity for one is.
	if _, err := s.GetDraft(ctx, 744); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a sprint carried over from 001 should have no draft, got %v", err)
	}
	three := 3.0
	if err := s.PutDraft(ctx, store.Draft{SprintJiraID: 744, Requests: []store.DraftRequest{
		{Key: "ABC-125", Op: "points.set", Points: &three},
	}}); err != nil {
		t.Fatalf("PutDraft after the upgrade: %v", err)
	}
	d, err := s.GetDraft(ctx, 744)
	if err != nil || len(d.Requests) != 1 || d.Requests[0].Points == nil || *d.Requests[0].Points != 3 {
		t.Errorf("the draft did not round-trip on the upgraded database: %+v, err %v", d, err)
	}
	if err := s.PutDraft(ctx, store.Draft{SprintJiraID: 999}); !errors.Is(err, store.ErrUnknownReference) {
		t.Errorf("a draft for an unknown sprint: want ErrUnknownReference, got %v", err)
	}

	// 005's column: a sprint carried over reads as never published, the
	// id sticks once written, and a later re-sweep of the sprint - which
	// carries no id - leaves it alone.
	if sp.ConfluencePageID != "" {
		t.Errorf("a sprint carried over from 001 should have no page id, got %q", sp.ConfluencePageID)
	}
	sp.ConfluencePageID = "123"
	if err := s.PutSprint(ctx, sp); err != nil {
		t.Fatalf("PutSprint with a page id after the upgrade: %v", err)
	}
	sp.ConfluencePageID = ""
	if err := s.PutSprint(ctx, sp); err != nil {
		t.Fatalf("PutSprint from a re-sweep after the upgrade: %v", err)
	}
	if again, err := s.GetSprint(ctx, 744); err != nil || again.ConfluencePageID != "123" {
		t.Errorf("the page id did not survive a re-sweep on the upgraded database: %+v, err %v", again, err)
	}
}

// Rolling the newest migration back must not take the rest of the schema
// with it, and rolling back one more must not either. A migration whose
// Down is wrong is only discovered when somebody needs it.
func TestMigrationsRollBackCleanly(t *testing.T) {
	s := isolated(t)
	ctx := context.Background()

	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	mustExec(t, s, `INSERT INTO people (account_id, name, baseline, active)
		VALUES ('acc-a', 'Person A', 12.5, true)`)

	goose.SetBaseFS(migrations)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("setting dialect: %v", err)
	}
	if err := goose.DownContext(ctx, s.db, "migrations"); err != nil {
		t.Fatalf("rolling back the newest migration: %v", err)
	}
	if got := dbVersion(t, s); got != latestMigration(t)-1 {
		t.Errorf("after one rollback the version is %d, want %d", got, latestMigration(t)-1)
	}
	if hasColumn(t, s, "sprints", "confluence_page_id") {
		t.Error("rolling back 005 left its column behind")
	}
	if !hasTable(t, s, "drafts") {
		t.Error("rolling back 005 took 004's table with it")
	}

	if err := goose.DownContext(ctx, s.db, "migrations"); err != nil {
		t.Fatalf("rolling back 004: %v", err)
	}
	if hasTable(t, s, "drafts") {
		t.Error("rolling back 004 left the drafts table behind")
	}
	if !hasColumn(t, s, "write_log", "change_set") || !hasColumn(t, s, "write_log", "outcome") {
		t.Error("rolling back 004 took 003's columns with it")
	}

	if err := goose.DownContext(ctx, s.db, "migrations"); err != nil {
		t.Fatalf("rolling back 003: %v", err)
	}
	for _, col := range []string{"change_set", "outcome"} {
		if hasColumn(t, s, "write_log", col) {
			t.Errorf("rolling back 003 left write_log.%s behind", col)
		}
	}
	if hasIndex(t, s, "write_log_change_set") {
		t.Error("rolling back 003 left its index behind")
	}
	if !hasColumn(t, s, "sprints", "capacity_reviewed_at") {
		t.Error("rolling back 003 took 002's column with it")
	}

	if err := goose.DownContext(ctx, s.db, "migrations"); err != nil {
		t.Fatalf("rolling back 002: %v", err)
	}
	if hasColumn(t, s, "sprints", "capacity_reviewed_at") {
		t.Error("rolling back 002 left its column behind")
	}

	var name string
	if err := s.db.QueryRowContext(ctx,
		`SELECT name FROM people WHERE account_id = 'acc-a'`).Scan(&name); err != nil {
		t.Fatalf("the roster should survive a rollback: %v", err)
	}
	if name != "Person A" {
		t.Errorf("name = %q after rollback", name)
	}

	// And forward again, because that is what happens next.
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("re-applying after a rollback: %v", err)
	}
	if !hasColumn(t, s, "sprints", "capacity_reviewed_at") {
		t.Error("re-applying did not restore 002's column")
	}
	if !hasColumn(t, s, "write_log", "change_set") || !hasColumn(t, s, "write_log", "outcome") {
		t.Error("re-applying did not restore 003's columns")
	}
	if !hasTable(t, s, "drafts") {
		t.Error("re-applying did not restore 004's table")
	}
	if !hasColumn(t, s, "sprints", "confluence_page_id") {
		t.Error("re-applying did not restore 005's column")
	}
}

// The Postgres backend must agree with the file backend about what a
// store holds, so the conformance suite runs here too - see
// pgstore_test.go. This only checks the schema a migrated database
// presents to it.
var _ store.Store = (*Store)(nil)

func mustExec(t *testing.T, s *Store, query string, args ...any) {
	t.Helper()
	if _, err := s.db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("executing %s: %v", strings.TrimSpace(strings.SplitN(query, "\n", 2)[0]), err)
	}
}
