// Package jsonstore keeps Argus's data in JSON files on disk.
//
// This is the default backend, and for one team it is enough: a year is
// about twenty-six sprints, so the whole dataset is a few hundred rows.
// It needs no database, no container and no connection string, which
// keeps the tool something a stranger can try in one command.
//
// Writes are atomic - a temporary file renamed over the target - so an
// interrupted write cannot leave a half-written file behind. A single
// mutex serialises access, because the HTTP server is threaded and these
// files are small enough that lock contention is not a concern.
//
// The directory holding these files is excluded from version control.
// See the package comment in store for why that matters.
package jsonstore

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/store"
)

// Store implements store.Store over a directory of JSON files.
type Store struct {
	dir string
	mu  sync.RWMutex
}

// New returns a store rooted at dir, creating it if absent.
func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("creating %s: %w", dir, err)
	}
	return &Store{dir: dir}, nil
}

func (s *Store) path(name string) string { return filepath.Join(s.dir, name+".json") }

// readInto loads a file into v. A missing file is not an error: it means
// nothing has been stored yet, which is the normal state on first run.
func (s *Store) readInto(name string, v any) error {
	b, err := os.ReadFile(s.path(name))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading %s: %w", name, err)
	}
	if len(b) == 0 {
		return nil
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("parsing %s: %w", name, err)
	}
	return nil
}

// write replaces a file atomically, so a crash mid-write cannot corrupt it.
func (s *Store) write(name string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %s: %w", name, err)
	}
	b = append(b, '\n')

	tmp, err := os.CreateTemp(s.dir, "."+name+".*")
	if err != nil {
		return fmt.Errorf("creating temp file for %s: %w", name, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename below succeeds

	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return fmt.Errorf("writing %s: %w", name, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("syncing %s: %w", name, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", name, err)
	}
	if err := os.Chmod(tmpName, 0o640); err != nil {
		return fmt.Errorf("setting permissions on %s: %w", name, err)
	}
	if err := os.Rename(tmpName, s.path(name)); err != nil {
		return fmt.Errorf("replacing %s: %w", name, err)
	}
	return nil
}

// ---- people ----------------------------------------------------------

func (s *Store) PutPerson(_ context.Context, p store.Person) error {
	if p.AccountID == "" {
		return fmt.Errorf("person needs an account id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var people []store.Person
	if err := s.readInto("people", &people); err != nil {
		return err
	}
	p.UpdatedAt = time.Now().UTC()
	replaced := false
	for i := range people {
		if people[i].AccountID == p.AccountID {
			people[i] = p
			replaced = true
			break
		}
	}
	if !replaced {
		people = append(people, p)
	}
	sort.Slice(people, func(i, j int) bool { return people[i].Name < people[j].Name })
	return s.write("people", people)
}

func (s *Store) GetPerson(ctx context.Context, accountID string) (store.Person, error) {
	people, err := s.ListPeople(ctx, true)
	if err != nil {
		return store.Person{}, err
	}
	for _, p := range people {
		if p.AccountID == accountID {
			return p, nil
		}
	}
	return store.Person{}, store.ErrNotFound
}

func (s *Store) ListPeople(_ context.Context, includeInactive bool) ([]store.Person, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var people []store.Person
	if err := s.readInto("people", &people); err != nil {
		return nil, err
	}
	out := make([]store.Person, 0, len(people))
	for _, p := range people {
		if includeInactive || p.Active {
			out = append(out, p)
		}
	}
	return out, nil
}

func (s *Store) DeletePerson(_ context.Context, accountID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var people []store.Person
	if err := s.readInto("people", &people); err != nil {
		return err
	}
	out := people[:0]
	for _, p := range people {
		if p.AccountID != accountID {
			out = append(out, p)
		}
	}
	return s.write("people", out)
}

// ---- sprints ---------------------------------------------------------

func (s *Store) PutSprint(_ context.Context, sp store.Sprint) error {
	if sp.JiraID == 0 {
		return fmt.Errorf("sprint needs a Jira id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var sprints []store.Sprint
	if err := s.readInto("sprints", &sprints); err != nil {
		return err
	}
	sp.UpdatedAt = time.Now().UTC()
	replaced := false
	for i := range sprints {
		if sprints[i].JiraID == sp.JiraID {
			sprints[i] = sp
			replaced = true
			break
		}
	}
	if !replaced {
		sprints = append(sprints, sp)
	}
	// Newest first: every caller wants recent sprints.
	sort.Slice(sprints, func(i, j int) bool { return sprints[i].Number > sprints[j].Number })
	return s.write("sprints", sprints)
}

func (s *Store) GetSprint(ctx context.Context, jiraID int64) (store.Sprint, error) {
	sprints, err := s.ListSprints(ctx, 0)
	if err != nil {
		return store.Sprint{}, err
	}
	for _, sp := range sprints {
		if sp.JiraID == jiraID {
			return sp, nil
		}
	}
	return store.Sprint{}, store.ErrNotFound
}

func (s *Store) ListSprints(_ context.Context, limit int) ([]store.Sprint, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var sprints []store.Sprint
	if err := s.readInto("sprints", &sprints); err != nil {
		return nil, err
	}
	if limit > 0 && len(sprints) > limit {
		sprints = sprints[:limit]
	}
	return sprints, nil
}

// ---- capacity --------------------------------------------------------

// capacityFile is stored per sprint, so one sprint's edits never rewrite
// another's.
func capacityFile(sprintJiraID int64) string {
	return fmt.Sprintf("capacity-%d", sprintJiraID)
}

func (s *Store) PutCapacity(ctx context.Context, c store.Capacity) error {
	if c.SprintJiraID == 0 || c.AccountID == "" {
		return fmt.Errorf("capacity needs a sprint id and an account id")
	}
	// Postgres enforces this with foreign keys. Checking it here keeps the
	// two backends behaving identically, rather than one silently
	// accepting capacity for a person nobody registered.
	if _, err := s.GetSprint(ctx, c.SprintJiraID); err != nil {
		return fmt.Errorf("sprint %d: %w", c.SprintJiraID, store.ErrUnknownReference)
	}
	if _, err := s.GetPerson(ctx, c.AccountID); err != nil {
		return fmt.Errorf("person %s: %w", c.AccountID, store.ErrUnknownReference)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	name := capacityFile(c.SprintJiraID)
	var rows []store.Capacity
	if err := s.readInto(name, &rows); err != nil {
		return err
	}
	c.UpdatedAt = time.Now().UTC()
	replaced := false
	for i := range rows {
		if rows[i].AccountID == c.AccountID {
			rows[i] = c
			replaced = true
			break
		}
	}
	if !replaced {
		rows = append(rows, c)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].AccountID < rows[j].AccountID })
	return s.write(name, rows)
}

func (s *Store) ListCapacity(_ context.Context, sprintJiraID int64) ([]store.Capacity, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var rows []store.Capacity
	if err := s.readInto(capacityFile(sprintJiraID), &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// ---- stats -----------------------------------------------------------

func (s *Store) PutStats(_ context.Context, st store.SprintStats) error {
	if st.SprintJiraID == 0 {
		return fmt.Errorf("stats need a sprint id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var all []store.SprintStats
	if err := s.readInto("stats", &all); err != nil {
		return err
	}
	st.RecordedAt = time.Now().UTC()
	replaced := false
	for i := range all {
		if all[i].SprintJiraID == st.SprintJiraID {
			all[i] = st
			replaced = true
			break
		}
	}
	if !replaced {
		all = append(all, st)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].SprintJiraID > all[j].SprintJiraID })
	return s.write("stats", all)
}

func (s *Store) ListStats(_ context.Context, limit int) ([]store.SprintStats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var all []store.SprintStats
	if err := s.readInto("stats", &all); err != nil {
		return nil, err
	}
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

// ---- write audit -----------------------------------------------------

func (s *Store) AppendWrite(_ context.Context, w store.WriteRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var all []store.WriteRecord
	if err := s.readInto("writes", &all); err != nil {
		return err
	}
	w.ID = int64(len(all) + 1)
	if w.At.IsZero() {
		w.At = time.Now().UTC()
	}
	// Newest first, so a bulk edit's effects are the first thing visible.
	all = append([]store.WriteRecord{w}, all...)
	return s.write("writes", all)
}

func (s *Store) ListWrites(_ context.Context, limit int) ([]store.WriteRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var all []store.WriteRecord
	if err := s.readInto("writes", &all); err != nil {
		return nil, err
	}
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

// ---- lifecycle -------------------------------------------------------

// Prune drops per-person rows for sprints that ended before the cutoff,
// keeping the aggregate stats the trends are drawn from.
func (s *Store) Prune(ctx context.Context, before time.Time) (store.PruneResult, error) {
	var res store.PruneResult

	sprints, err := s.ListSprints(ctx, 0)
	if err != nil {
		return res, err
	}
	for _, sp := range sprints {
		// A sprint with no end date cannot be judged stale, so it is kept.
		if sp.EndsAt == nil || !sp.EndsAt.Before(before) {
			continue
		}
		rows, err := s.ListCapacity(ctx, sp.JiraID)
		if err != nil {
			return res, err
		}
		if len(rows) == 0 {
			continue
		}
		s.mu.Lock()
		err = os.Remove(s.path(capacityFile(sp.JiraID)))
		s.mu.Unlock()
		if err != nil && !os.IsNotExist(err) {
			return res, fmt.Errorf("pruning capacity for sprint %d: %w", sp.JiraID, err)
		}
		res.CapacityRows += len(rows)
	}

	writes, err := s.ListWrites(ctx, 0)
	if err != nil {
		return res, err
	}
	kept := make([]store.WriteRecord, 0, len(writes))
	for _, w := range writes {
		if w.At.Before(before) {
			res.WriteRows++
			continue
		}
		kept = append(kept, w)
	}
	if res.WriteRows > 0 {
		s.mu.Lock()
		err = s.write("writes", kept)
		s.mu.Unlock()
		if err != nil {
			return res, err
		}
	}
	return res, nil
}

// Migrate exists to satisfy the interface. Files have no schema to
// version; a field added to a struct simply reads as its zero value in
// records written before it existed.
func (s *Store) Migrate(context.Context) error { return nil }

func (s *Store) Close() error { return nil }
