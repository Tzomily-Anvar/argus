package backlog

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Previews held in memory until they are applied or expire, and the
// digest that ties an apply to the preview a person saw. The same shape
// as the sprint report's change sets, for the same reasons: a preview is
// an in-flight intention rather than a record of anything, it holds
// nothing that is not already on the screen, and losing it on a restart
// is the correct outcome.

// ---- held previews ------------------------------------------------------

// hold assigns the preview a random id, counts its rows, digests it and
// keeps it until it expires or is taken. In memory rather than in the
// store: a preview is an in-flight intention, not a record of anything.
func (b *Batch) hold(p BatchPreview) BatchPreview {
	for _, r := range p.Rows {
		if r.Skipped == "" {
			p.Changes++
		} else {
			p.Skipped++
		}
	}
	p.ID = newID()
	p.ExpiresAt = b.now().Add(PreviewLifetime)
	p.Allowed = b.cfg.EnableWrites
	p.DeleteAllowed = b.cfg.EnableWrites && b.cfg.AllowDelete
	p.Digest = digest(p)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sweep()
	b.previews[p.ID] = p
	return p
}

// Get returns a held preview, or nil once it has expired or been taken.
func (b *Batch) Get(id string) *BatchPreview {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sweep()
	p, ok := b.previews[id]
	if !ok {
		return nil
	}
	return &p
}

// take returns a held preview and removes it, so it can be applied once.
func (b *Batch) take(id string) *BatchPreview {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sweep()
	p, ok := b.previews[id]
	if !ok {
		return nil
	}
	delete(b.previews, id)
	return &p
}

// sweep drops what has expired. Called with the lock held.
func (b *Batch) sweep() {
	now := b.now()
	for id, p := range b.previews {
		if !now.Before(p.ExpiresAt) {
			delete(b.previews, id)
		}
	}
}

// newID is 128 random bits: unguessable, because the id is the handle a
// future apply will accept.
func newID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		panic("backlog: reading random bytes: " + err.Error())
	}
	return hex.EncodeToString(buf)
}

// digested is the part of a preview the digest covers: the action, what
// it undoes, the parameters, and per row what will be written to which
// ticket in which state. Summaries and types are for reading and are
// left out, so a renamed ticket cannot invalidate a preview somebody is
// looking at.
type digested struct {
	Action   string         `json:"action"`
	Reverses string         `json:"reverses"`
	Params   map[string]any `json:"params"`
	Rows     []struct {
		Key, Before, After, Skipped string
		Guard                       Guard
	} `json:"rows"`
}

// digest is a sha256 over the canonical JSON of the digested part, in
// row order. The browser sends it back with an apply, and a mismatch
// means what it rendered is not what the server proposed.
func digest(p BatchPreview) string {
	d := digested{Action: p.Action, Reverses: p.Reverses, Params: p.params}
	for _, r := range p.Rows {
		d.Rows = append(d.Rows, struct {
			Key, Before, After, Skipped string
			Guard                       Guard
		}{r.Key, r.Before, r.After, r.Skipped, r.Guard})
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return "" // matches nothing, which is the safe failure
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
