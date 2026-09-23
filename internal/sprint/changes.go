package sprint

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrNoReport is returned when a preview is asked for against a sprint
// whose report has not been built. A change set is computed from the
// cached report and the issues behind it, so without one there is
// nothing to propose against and nothing to guard.
var ErrNoReport = errors.New("no report has been built for that sprint")

// changeSets holds proposals in memory until they are applied or expire.
//
// In memory rather than in the store: a proposal is an in-flight
// intention rather than a record of anything, it holds nothing that is
// not already on the screen, and losing it on a restart is the correct
// outcome. The clock is a function so a test can move it.
type changeSets struct {
	mu   sync.Mutex
	sets map[string]ChangeSet
	now  func() time.Time
}

func newChangeSets(now func() time.Time) *changeSets {
	return &changeSets{sets: map[string]ChangeSet{}, now: now}
}

// Put assigns the set a random id and holds it. The id is assigned here
// rather than by Propose so that Propose stays a pure function.
func (h *changeSets) Put(cs ChangeSet) ChangeSet {
	cs.ID = newID()
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sweep()
	h.sets[cs.ID] = cs
	return cs
}

// Get returns a held set, or nil once it has expired or been taken. A
// copy, so a caller cannot change what a later apply would compare.
func (h *changeSets) Get(id string) *ChangeSet {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sweep()
	cs, ok := h.sets[id]
	if !ok {
		return nil
	}
	return &cs
}

// Take returns a held set and removes it, so it can be applied once. A
// second Take of the same id gets nil, which is what stops a double-click
// writing twice.
func (h *changeSets) Take(id string) *ChangeSet {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sweep()
	cs, ok := h.sets[id]
	if !ok {
		return nil
	}
	delete(h.sets, id)
	return &cs
}

// sweep drops what has expired. Called on every access with the lock
// held, which is as much housekeeping as a map of a few entries needs.
func (h *changeSets) sweep() {
	now := h.now()
	for id, cs := range h.sets {
		if !now.Before(cs.ExpiresAt) {
			delete(h.sets, id)
		}
	}
}

// newID is 128 random bits. Unguessable rather than merely unique,
// because the id is the handle a future apply will accept.
func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// The system's random source failing is not something this code
		// can recover from; a time-based id would be guessable.
		panic("sprint: reading random bytes: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// held is this service's pending change sets, made on first use for a
// Service built as a literal in a test rather than through NewService.
func (s *Service) held() *changeSets {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.changes == nil {
		s.changes = newChangeSets(func() time.Time { return time.Now().UTC() })
	}
	return s.changes
}

// Propose builds a preview against the cached report for a sprint and
// holds it under a fresh id. Nothing is asked of Jira: the report and the
// issues it was built from are already in memory, and the roster comes
// from the store. A sprint nobody has opened yet is ErrNoReport, because
// a preview against a report the person has not seen would be a preview
// of nothing they recognise.
func (s *Service) Propose(ctx context.Context, sprintNumber int, reqs []ChangeRequest) (ChangeSet, error) {
	s.mu.RLock()
	var entry cached
	found := false
	for _, c := range s.reports {
		// By the sprint's own number rather than through sprintIDs, which
		// is only filled by a resolve; the cached entry knows which
		// sprint it is.
		if sprintNumber > 0 && c.sprint.Number == sprintNumber && c.buildErr == nil && len(c.issues) > 0 {
			entry, found = *c, true
		}
	}
	s.mu.RUnlock()
	if !found {
		return ChangeSet{}, fmt.Errorf("%w: sprint %d; open its report first", ErrNoReport, sprintNumber)
	}

	people, err := s.store.ListPeople(ctx, true)
	if err != nil {
		return ChangeSet{}, err
	}
	opens, closes := Inputs{Sprint: entry.sprint, PreviousClose: entry.previous}.window()
	in := ProposalInputs{
		Requests: reqs, People: people, Rules: s.cfg.Rules,
		HoursPerPoint: s.cfg.HoursPerPoint,
		SprintOpens:   opens, SprintCloses: closes,
		Now: time.Now().UTC(),
	}
	if entry.fields != nil {
		in.PointsField = entry.fields.points
	}
	in.Actor = s.cfg.Actor

	return s.held().Put(Propose(entry.report, entry.issues, in)), nil
}

// ChangeSet returns a held preview by id, nil once it has expired. It is
// what lets a reload of the page show the same preview.
func (s *Service) ChangeSet(id string) *ChangeSet { return s.held().Get(id) }

// TakeChangeSet returns a held preview and removes it, for the apply
// that does not exist yet. Kept beside Propose so that when an apply
// arrives it has one place to take its input from.
func (s *Service) TakeChangeSet(id string) *ChangeSet { return s.held().Take(id) }
