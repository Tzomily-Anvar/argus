package jsonstore

// The backlog tool's two files: acks.json, one entry per acknowledged
// item, and ops.json, the operations roster. Both are small - a few
// hundred acknowledgements at the very most, a handful of people - so
// each is read and rewritten whole like everything else here.

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/store"
)

const (
	acksFile = "acks"
	opsFile  = "ops"
)

func (s *Store) PutAcks(_ context.Context, acks []store.Ack) error {
	if len(acks) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var all []store.Ack
	if err := s.readInto(acksFile, &all); err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, a := range acks {
		if a.Key == "" || a.Kind == "" {
			return fmt.Errorf("an acknowledgement needs a kind and a key")
		}
		if a.At.IsZero() {
			a.At = now
		}
		replaced := false
		for i := range all {
			if all[i].Kind == a.Kind && all[i].Key == a.Key {
				all[i] = a
				replaced = true
				break
			}
		}
		if !replaced {
			all = append(all, a)
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Kind != all[j].Kind {
			return all[i].Kind < all[j].Kind
		}
		return all[i].Key < all[j].Key
	})
	return s.write(acksFile, all)
}

func (s *Store) ListAcks(_ context.Context, kind string) ([]store.Ack, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var all []store.Ack
	if err := s.readInto(acksFile, &all); err != nil {
		return nil, err
	}
	out := make([]store.Ack, 0, len(all))
	for _, a := range all {
		if a.Kind == kind {
			out = append(out, a)
		}
	}
	return out, nil
}

func (s *Store) DeleteAcks(_ context.Context, kind string, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	drop := make(map[string]bool, len(keys))
	for _, k := range keys {
		drop[k] = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var all []store.Ack
	if err := s.readInto(acksFile, &all); err != nil {
		return err
	}
	kept := all[:0]
	for _, a := range all {
		if a.Kind == kind && drop[a.Key] {
			continue
		}
		kept = append(kept, a)
	}
	return s.write(acksFile, kept)
}

func (s *Store) ListOps(_ context.Context) ([]store.OpsMember, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var all []store.OpsMember
	if err := s.readInto(opsFile, &all); err != nil {
		return nil, err
	}
	if all == nil {
		all = []store.OpsMember{}
	}
	return all, nil
}

func (s *Store) PutOps(_ context.Context, members []store.OpsMember) error {
	now := time.Now().UTC()
	out := make([]store.OpsMember, 0, len(members))
	seen := map[string]bool{}
	for _, m := range members {
		if m.AccountID == "" {
			return fmt.Errorf("a roster member needs an account id")
		}
		// The same person listed twice is one person.
		if seen[m.AccountID] {
			continue
		}
		seen[m.AccountID] = true
		m.UpdatedAt = now
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.write(opsFile, out)
}

// pruneAcks drops acknowledgements older than the retention window and
// reports how many went.
func (s *Store) pruneAcks(before time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var all []store.Ack
	if err := s.readInto(acksFile, &all); err != nil {
		return 0, err
	}
	kept := make([]store.Ack, 0, len(all))
	for _, a := range all {
		if a.At.Before(before) {
			continue
		}
		kept = append(kept, a)
	}
	dropped := len(all) - len(kept)
	if dropped == 0 {
		return 0, nil
	}
	return dropped, s.write(acksFile, kept)
}
