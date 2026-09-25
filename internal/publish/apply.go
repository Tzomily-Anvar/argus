package publish

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/Tzomily-Anvar/argus/internal/jira"
	"github.com/Tzomily-Anvar/argus/internal/store"
)

// Result is what a publish did.
type Result struct {
	Action  string `json:"action"`
	PageID  string `json:"page_id"`
	URL     string `json:"url"`
	Version int    `json:"version"`
}

// Status is what the panel needs before it offers anything: whether
// publishing is configured, whether this deployment may write, and the
// section switches with the standing choice pre-filled.
type Status struct {
	Configured bool            `json:"configured"`
	Reason     string          `json:"reason,omitempty"`
	Allowed    bool            `json:"allowed"`
	Setting    string          `json:"setting"`
	Sections   []SectionSwitch `json:"sections"`
}

// SectionSwitch is one section of the page and whether the standing
// choice includes it.
type SectionSwitch struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	On    bool   `json:"on"`
}

const setting = "ARGUS_SPRINT_ALLOW_WRITES"

// Status describes this publisher to the panel.
func (s *Service) Status() Status {
	st := Status{Configured: s.Configured(), Allowed: s.cfg.EnableWrites, Setting: setting, Sections: switches(s.cfg.Sections)}
	switch {
	case !st.Configured:
		st.Reason = ErrNotConfigured.Error()
	case !st.Allowed:
		st.Reason = "Writes are off for this deployment. The preview still renders; set " + setting + " to publish."
	default:
		st.Reason = "Publishing is switched on by " + setting + ". Every page is previewed first, and only a previewed page can be published."
	}
	return st
}

// NotConfigured is the status of a deployment with no publisher wired at
// all, for the server to answer with rather than a 500.
func NotConfigured(reason string, allowed bool) Status {
	return Status{Reason: reason, Allowed: allowed, Setting: setting, Sections: switches(nil)}
}

// Publish sends a held preview to Confluence, once.
//
// Everything that can refuse does so before a request is made: writes
// off, a preview expired or already taken, a digest that is not the one
// the server computed. Only then is the preview taken, so a double-click
// finds nothing to publish. An update re-reads the page's version first
// and refuses if it moved; Confluence would refuse a stale version
// itself, and that is the real guard, but checking first makes the
// message ours. One audit row is written whatever happens after that.
func (s *Service) Publish(ctx context.Context, id, digest string) (Result, error) {
	if !s.cfg.EnableWrites {
		return Result{}, ErrWritesDisabled
	}
	p := s.held.Get(id)
	if p == nil {
		return Result{}, ErrExpired
	}
	if digest == "" || digest != p.Digest {
		return Result{}, ErrDigestMismatch
	}
	if p = s.held.Take(id); p == nil {
		return Result{}, ErrExpired
	}

	if p.Action == "update" {
		cur, err := s.read(p.PageID)
		if err != nil {
			return Result{}, fmt.Errorf("re-reading page %s: %w", p.PageID, err)
		}
		if cur.version != p.Version {
			s.record(ctx, *p, p.PageID, store.OutcomeSkipped, 0,
				fmt.Sprintf("skipped: the page is at version %d, the preview was built against %d", cur.version, p.Version))
			return Result{}, fmt.Errorf("%w: it is at version %d, the preview was built against %d", ErrMoved, cur.version, p.Version)
		}
	}

	method, path, body := s.request(*p)
	var answer pageAnswer
	if err := s.client.Write(method, path, "", body, &answer); err != nil {
		s.record(ctx, *p, p.PageID, store.OutcomeFailed, 0, "failed: "+err.Error())
		if jira.StatusCode(err) == http.StatusConflict {
			return Result{}, fmt.Errorf("%w: %v", ErrMoved, err)
		}
		return Result{}, err
	}
	res := Result{Action: p.Action, PageID: answer.ID, Version: answer.Version.Number, URL: s.pageURL(answer.Links.WebUI)}
	if res.PageID == "" {
		res.PageID = p.PageID
	}
	if err := s.remember(ctx, *p, res.PageID); err != nil {
		// The page is there; only the shortcut to it is missing, and the
		// title search finds it next time. Worth a note, not a failure.
		s.record(ctx, *p, res.PageID, store.OutcomeApplied, res.Version, "the page id could not be stored: "+err.Error())
		return res, nil
	}
	s.record(ctx, *p, res.PageID, store.OutcomeApplied, res.Version, "")
	return res, nil
}

// request is the exact request a preview becomes, in the shape the gate
// accepts and nothing more. The version message names the digest so
// Confluence's own history says which preview each version came from.
func (s *Service) request(p Preview) (method, path string, body any) {
	storage := map[string]any{"representation": "storage", "value": p.body}
	if p.Action == "create" {
		return http.MethodPost, "/wiki/api/v2/pages", map[string]any{
			"spaceId": s.cfg.SpaceID, "status": "current", "title": p.Title,
			"parentId": s.cfg.ParentPageID, "body": storage,
		}
	}
	return http.MethodPut, "/wiki/api/v2/pages/" + p.PageID, map[string]any{
		"id": p.PageID, "status": "current", "title": p.Title, "body": storage,
		"version": map[string]any{"number": p.Version + 1, "message": "Argus sprint report, digest " + short(p.Digest)},
	}
}

// record leaves the audit row: the operation, the page, the version
// replaced and the version written, and the digest and title so the row
// can be matched to the preview that was approved. The block's previous
// content is not stored - Confluence keeps the page's history itself,
// and a copy here would put the per-person figures in a second place.
func (s *Service) record(ctx context.Context, p Preview, pageID, outcome string, written int, detail string) {
	var before, after string
	if p.Action == "update" {
		before = strconv.Itoa(p.Version)
	}
	if written > 0 {
		after = strconv.Itoa(written)
	}
	note := "digest " + p.Digest + "; title " + p.Title
	if detail != "" {
		note += "; " + detail
	}
	_ = s.store.AppendWrite(ctx, store.WriteRecord{
		Operation: "page." + p.Action, Target: pageID, Before: before, After: after,
		Actor: s.cfg.Actor, Note: note, ChangeSet: p.ID, Outcome: outcome,
	})
}

func short(digest string) string {
	if len(digest) > 12 {
		return digest[:12]
	}
	return digest
}
