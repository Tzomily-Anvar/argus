package publish

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/page"
)

// previews holds what has been previewed and not yet published or
// expired. In memory, like the sprint service's change sets and for the
// same reason: a preview is an in-flight intention rather than a record
// of anything, and losing it on a restart is right. The clock is a
// function so a test can move it.
type previews struct {
	mu   sync.Mutex
	held map[string]Preview
	now  func() time.Time
}

func newPreviews(now func() time.Time) *previews {
	return &previews{held: map[string]Preview{}, now: now}
}

// Put assigns the preview a random id and holds it.
func (h *previews) Put(p Preview) Preview {
	p.ID = newID()
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sweep()
	h.held[p.ID] = p
	return p
}

// Get returns a held preview, or nil once it has expired or been taken.
// A copy, so a caller cannot change what a later publish would send.
func (h *previews) Get(id string) *Preview {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sweep()
	p, ok := h.held[id]
	if !ok {
		return nil
	}
	return &p
}

// Take returns a held preview and removes it, so it can be published
// once. A second Take of the same id gets nil.
func (h *previews) Take(id string) *Preview {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sweep()
	p, ok := h.held[id]
	if !ok {
		return nil
	}
	delete(h.held, id)
	return &p
}

// sweep drops what has expired, on every access with the lock held.
func (h *previews) sweep() {
	now := h.now()
	for id, p := range h.held {
		if !now.Before(p.ExpiresAt) {
			delete(h.held, id)
		}
	}
}

// newID is 128 random bits: unguessable, because the id is the handle a
// later publish will accept.
func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("publish: reading random bytes: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// switches lists every section of the page in its fixed order, marking
// the ones a standing choice includes. Nil means all of them.
func switches(chosen []string) []SectionSwitch {
	on := map[string]bool{}
	for _, id := range chosen {
		on[id] = true
	}
	labels := page.Labels()
	out := make([]SectionSwitch, 0, len(page.All))
	for _, id := range page.All {
		out = append(out, SectionSwitch{ID: id, Label: labels[id], On: chosen == nil || on[id]})
	}
	return out
}
