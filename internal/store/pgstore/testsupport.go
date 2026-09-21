package pgstore

import "context"

// Truncate empties every table. It exists for the conformance suite,
// which needs a clean database per subtest; nothing in the application
// calls it, and retention uses Prune instead.
func (s *Store) Truncate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx,
		`TRUNCATE write_log, sprint_stats, capacity, sprints, people RESTART IDENTITY CASCADE`)
	return err
}
