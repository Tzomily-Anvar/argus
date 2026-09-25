package sprint

import (
	"context"
	"errors"

	"github.com/Tzomily-Anvar/argus/internal/store"
)

// clearApplied drops from the sprint's persisted draft the requests that
// an apply just carried out. The draft is what the panel queues from and
// what survives a reload; left as it was, the same rows would be proposed
// again at the next preview and, for a worklog entry, logged twice.
// Rows that were skipped or failed stay queued, because they are still
// to do.
func (s *Service) clearApplied(ctx context.Context, sprintID int64, changes []Change, rows []RowResult) {
	applied := map[string]bool{}
	for i, r := range rows {
		if r.Outcome == store.OutcomeApplied && i < len(changes) {
			applied[draftKey(changes[i].Key, changes[i].Op, changes[i].Person, changes[i].WorklogID)] = true
		}
	}
	if len(applied) == 0 {
		return
	}
	d, err := s.store.GetDraft(ctx, sprintID)
	if err != nil {
		return // no draft, or a store that cannot answer; nothing to clear
	}
	kept := d.Requests[:0]
	for _, r := range d.Requests {
		if !applied[draftKey(r.Key, r.Op, r.Person, r.WorklogID)] {
			kept = append(kept, r)
		}
	}
	if len(kept) == len(d.Requests) {
		return
	}
	d.Requests = kept
	if len(kept) == 0 {
		if err := s.store.DeleteDraft(ctx, sprintID); err != nil && !errors.Is(err, store.ErrNotFound) {
			return
		}
		return
	}
	_ = s.store.PutDraft(ctx, d)
}

// draftKey is what makes a queued request and an applied change the same
// thing: the ticket, the operation, and for a worklog entry the person or
// the entry it named.
func draftKey(key, op, person, worklogID string) string {
	return key + "\x00" + op + "\x00" + person + "\x00" + worklogID
}
