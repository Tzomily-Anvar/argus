// Package publish puts a sprint report on a Confluence page.
//
// One page per sprint, under a parent the operator names: created the
// first time, updated in place after that, with only the block between
// Argus's two markers ever touched. The page package renders the block;
// this package finds the page, splices the block in, shows the person
// the difference, and - once, on approval, with writes switched on -
// sends it through the Jira client's gate, which lists the two page
// writes and nothing else under /wiki/.
//
// It sits above both sprint and page, because page renders a sprint
// report and sprint cannot import what renders it.
package publish

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/jira"
	"github.com/Tzomily-Anvar/argus/internal/page"
	"github.com/Tzomily-Anvar/argus/internal/sprint"
	"github.com/Tzomily-Anvar/argus/internal/store"
)

// Lifetime is how long a preview stays valid: the same fifteen minutes
// as a change set, for the same reason.
const Lifetime = 15 * time.Minute

// The refusals. Each leaves nothing written and, except for a preview
// already taken, leaves the preview held.
var (
	ErrNotConfigured  = errors.New("publishing is not configured; set ARGUS_CONFLUENCE_SPACE_ID and ARGUS_CONFLUENCE_PARENT_PAGE_ID")
	ErrWritesDisabled = errors.New("writes are off for this deployment; set ARGUS_SPRINT_ALLOW_WRITES to switch them on")
	ErrExpired        = errors.New("that preview has expired or was already published; build it again")
	ErrDigestMismatch = errors.New("that is not the preview the server built; build it again")
	ErrMoved          = errors.New("the page changed since the preview; build it again")
	ErrPageGone       = errors.New("the page recorded for this sprint no longer exists")
)

// Config is everything the publisher is told once, at startup.
type Config struct {
	SpaceID      string
	ParentPageID string

	// Title is a text/template over the sprint's Number, Name and
	// Project; LiveSuffix is added while the sprint is open.
	Title      string
	LiveSuffix string

	// Sections is the team's standing choice, nil for all of them.
	Sections []string

	Project     string
	Actor       string // the account the write is made as
	Conventions page.Conventions

	// EnableWrites is ARGUS_SPRINT_ALLOW_WRITES, read once by the caller,
	// exactly as the sprint service holds it. Publishing shares the
	// setting because it is a write like the others.
	EnableWrites bool
}

// Service is what the HTTP layer talks to.
type Service struct {
	client  *jira.Client
	reports *sprint.Service
	store   store.Store
	cfg     Config
	held    *previews
	now     func() time.Time
}

// New wires a publisher. Nothing is checked here: an unconfigured
// publisher still answers Status, which is how the panel learns what to
// set.
func New(client *jira.Client, reports *sprint.Service, st store.Store, cfg Config) *Service {
	if len(cfg.Sections) == 0 {
		cfg.Sections = append([]string{}, page.All...)
	}
	now := func() time.Time { return time.Now().UTC() }
	return &Service{client: client, reports: reports, store: st, cfg: cfg, held: newPreviews(now), now: now}
}

// Configured reports whether a space and a parent page are set. One
// without the other is a mistake, and publishing stays hidden rather
// than failing at the moment it is pressed.
func (s *Service) Configured() bool { return s.cfg.SpaceID != "" && s.cfg.ParentPageID != "" }

// DiffLine is one line of the preview's difference: what the page's
// block holds now against what it will hold.
type DiffLine struct {
	Kind string `json:"kind"` // same | add | del
	Text string `json:"text"`
}

// Preview is a publish waiting for approval. Everything the person sees
// is here; the whole page body it would send is held beside it and never
// serialised, because the block is what changes and the diff shows it.
type Preview struct {
	ID           string     `json:"id"`
	Sprint       int        `json:"sprint"`
	SprintJiraID int64      `json:"sprint_jira_id"`
	Action       string     `json:"action"` // create | update
	Title        string     `json:"title"`
	TitleChanges bool       `json:"title_changes"`
	PageID       string     `json:"page_id,omitempty"`
	Version      int        `json:"version"` // the version an update replaces; 0 for a create
	URL          string     `json:"url,omitempty"`
	Block        string     `json:"block"`
	Diff         []DiffLine `json:"diff"`
	Sections     []string   `json:"sections"`
	Digest       string     `json:"digest"`
	ExpiresAt    time.Time  `json:"expires_at"`
	How          string     `json:"how"` // replaced | migrated | appended | created
	Actor        string     `json:"actor"`

	body string // the page as it will be sent
	name string // the sprint's name, for the store row if it is missing
}

// Preview renders the block from the cached report, finds the sprint's
// page, splices the block in, and holds the result under a fresh id.
// Nothing is written. A sprint nobody has opened is sprint.ErrNoReport,
// because a preview against a report the person has not seen would be a
// preview of nothing they recognise.
func (s *Service) Preview(ctx context.Context, sprintNumber int, sections []string) (Preview, error) {
	if !s.Configured() {
		return Preview{}, ErrNotConfigured
	}
	rep, ok := s.reports.CachedReport(sprintNumber)
	if !ok {
		return Preview{}, fmt.Errorf("%w: sprint %d; open its report first", sprint.ErrNoReport, sprintNumber)
	}
	sections, err := s.sections(sections)
	if err != nil {
		return Preview{}, err
	}
	now := s.now()
	block, err := page.Render(rep, page.Inputs{
		Sections: sections, Notes: s.notes(ctx, rep.Sprint.JiraID),
		Conventions: s.cfg.Conventions, GeneratedAt: now,
	})
	if err != nil {
		return Preview{}, fmt.Errorf("rendering the page: %w", err)
	}
	title, err := s.title(rep, rep.Sprint.State != "closed")
	if err != nil {
		return Preview{}, err
	}
	found, err := s.find(ctx, rep)
	if err != nil {
		return Preview{}, err
	}

	p := Preview{
		Sprint: rep.Sprint.Number, SprintJiraID: rep.Sprint.JiraID, Title: title,
		Block: block, Sections: sections, Actor: s.cfg.Actor, name: rep.Sprint.Name,
	}
	if found.id != "" {
		body, how, err := page.Splice(found.body, block)
		if err != nil {
			return Preview{}, fmt.Errorf("page %s: %w", found.id, err)
		}
		p.Action, p.How, p.body = "update", how, body
		p.PageID, p.Version, p.URL = found.id, found.version, found.url
		p.TitleChanges = found.title != title
		p.Diff = diffLines(inner(found.body), inner(block))
	} else {
		p.Action, p.How, p.body = "create", "created", page.Scaffold(rep, block)
		p.Diff = diffLines("", inner(block))
	}
	// The gate will refuse a body without the markers; saying so here,
	// before anything is held, gives a message about the page rather
	// than about a request.
	if err := marked(p.body); err != nil {
		return Preview{}, fmt.Errorf("the page body cannot be sent: %w", err)
	}
	p.Digest = digest(p)
	p.ExpiresAt = now.Add(Lifetime)
	return s.held.Put(p), nil
}

// Held returns a held preview by id, nil once it has expired, so a
// reload of the panel shows the same preview.
func (s *Service) Held(id string) *Preview { return s.held.Get(id) }

// sections is the sections this publish renders: the ones asked for, or
// the standing choice, each of them one the page knows.
func (s *Service) sections(asked []string) ([]string, error) {
	if len(asked) == 0 {
		asked = s.cfg.Sections
	}
	known := map[string]bool{}
	for _, id := range page.All {
		known[id] = true
	}
	out := make([]string, 0, len(asked))
	for _, id := range asked {
		id = strings.TrimSpace(id)
		if !known[id] {
			return nil, fmt.Errorf("%q is not a section of the page; the sections are %s", id, strings.Join(page.All, ", "))
		}
		out = append(out, id)
	}
	return out, nil
}

// notes are the Reasons, read from the capacity rows as they stand now
// rather than from the report, so a line typed in the Reasons step a
// moment ago is the one that lands. Keyed by account id.
func (s *Service) notes(ctx context.Context, sprintJiraID int64) map[string]string {
	out := map[string]string{}
	rows, err := s.store.ListCapacity(ctx, sprintJiraID)
	if err != nil {
		return out
	}
	for _, c := range rows {
		if strings.TrimSpace(c.Note) != "" {
			out[c.AccountID] = c.Note
		}
	}
	return out
}

// marked is the rule the gate applies to a body, stated here as well so
// a preview can refuse a page in its own words.
func marked(body string) error {
	return page.Marked(body)
}

// inner is what sits between a body's markers - Argus's own, or the old
// tool's - with the surrounding line breaks dropped. Empty when there
// are none, which is what a page about to have a block appended holds.
func inner(body string) string {
	if got := page.Inner(body); got != "" {
		return got
	}
	i, j := strings.Index(body, page.LegacyStart), strings.Index(body, page.LegacyEnd)
	if i < 0 || j < i {
		return ""
	}
	return strings.Trim(body[i+len(page.LegacyStart):j], "\n")
}

// digest is a sha256 over what will be sent and what it replaces. The
// client sends it back with the publish, and a mismatch means what the
// browser rendered is not what the server built.
func digest(p Preview) string {
	raw, err := json.Marshal(struct {
		Action, PageID, Title, Body string
		Version                     int
	}{p.Action, p.PageID, p.Title, p.body, p.Version})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
