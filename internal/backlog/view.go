package backlog

import (
	"context"
	"sort"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/config"
	"github.com/Tzomily-Anvar/argus/internal/store"
)

// View is the whole backlog page in one answer. Every list is a list,
// never null: this crosses to a browser as JSON, and a null where an
// array was promised is a crash in the panel rather than an empty one.
type View struct {
	SweptAt    string         `json:"swept_at"`
	Building   bool           `json:"building"`
	Warnings   []string       `json:"warnings"`
	Rows       []Row          `json:"rows"`
	Groups     []Group        `json:"groups"`
	Epics      []Epic         `json:"epics"`
	Sprints    []SprintRef    `json:"sprints"`
	EpicsAll   []IssueRef     `json:"epics_all"`
	Stories    []Story        `json:"stories"`
	Operations OperationsView `json:"operations"`
	Settings   Settings       `json:"settings"`
}

// Story is one open container-type issue, for linking work beneath it.
type Story struct {
	Key     string `json:"key"`
	Summary string `json:"summary"`
	EpicKey string `json:"epic_key"`
}

// Settings is what the panel needs to know about the deployment: the
// label spellings, and which writes the other side of the tool may make.
type Settings struct {
	OperationsLabel string   `json:"operations_label"`
	LegacyLabels    []string `json:"legacy_labels"`
	AllowDelete     bool     `json:"allow_delete"`
	WritesAllowed   bool     `json:"writes_allowed"`
}

// Candidate is a reporter seen in the backlog, offered for the roster.
type Candidate struct {
	AccountID string `json:"account_id"`
	Label     string `json:"label"`
	Reported  int    `json:"reported"`
}

// View draws every view over the cached sweep and what the store holds.
func (s *Service) View(ctx context.Context) (View, error) {
	snap := s.Snapshot()
	acks, err := s.store.ListAcks(ctx, store.AckTicket)
	if err != nil {
		return View{}, err
	}
	members, err := s.store.ListOps(ctx)
	if err != nil {
		return View{}, err
	}
	watermarks := make(map[string]string, len(acks))
	for _, a := range acks {
		watermarks[a.Key] = a.Watermark
	}
	roster := make(map[string]bool, len(members))
	rosterRefs := make([]Ref, 0, len(members))
	for _, m := range members {
		roster[m.AccountID] = true
		rosterRefs = append(rosterRefs, Ref{AccountID: m.AccountID, Label: m.Name})
	}

	rows := Rows(snap, s.cfg, watermarks, roster, time.Now())
	epicsAll := make([]IssueRef, 0, len(snap.Epics))
	for _, e := range snap.Epics {
		epicsAll = append(epicsAll, IssueRef{Key: e.Key, Summary: e.Fields.Summary})
	}
	v := View{
		Building: snap.Building,
		Warnings: append([]string{}, snap.Warnings...),
		Rows:     rows,
		Groups:   Groups(rows, s.cfg, snap.Fields, roster),
		Epics:    Epics(rows, epicsAll, s.cfg),
		Sprints:  make([]SprintRef, 0, len(snap.Sprints)),
		EpicsAll: epicsAll,
		Stories:  []Story{},
		Settings: Settings{
			OperationsLabel: s.cfg.OperationsLabel,
			LegacyLabels:    append([]string{}, s.cfg.LegacyLabels...),
			AllowDelete:     config.BacklogDeleteAllowed(),
			WritesAllowed:   config.SprintWritesAllowed(),
		},
	}
	if snap.Error != "" {
		v.Warnings = append([]string{"the last sweep failed and this is the picture before it: " + snap.Error}, v.Warnings...)
	}
	if !snap.SweptAt.IsZero() {
		v.SweptAt = snap.SweptAt.Format(time.RFC3339)
	}
	for _, sp := range snap.Sprints {
		v.Sprints = append(v.Sprints, SprintRef{ID: sp.ID, Name: sp.Name, State: sp.State})
	}
	for _, r := range rows {
		if hasFold(s.cfg.ContainerTypes, r.Type) {
			st := Story{Key: r.Key, Summary: r.Summary}
			if r.Epic != nil {
				st.EpicKey = r.Epic.Key
			}
			v.Stories = append(v.Stories, st)
		}
	}
	v.Operations = Operations(rows)
	v.Operations.Roster = rosterRefs
	return v, nil
}

// InboxView is the inbox with dismissals applied.
func (s *Service) InboxView(ctx context.Context) ([]Item, []string, error) {
	snap := s.Snapshot()
	acks, err := s.store.ListAcks(ctx, store.AckInbox)
	if err != nil {
		return nil, nil, err
	}
	dismissed := make(map[string]string, len(acks))
	for _, a := range acks {
		dismissed[a.Key] = a.Watermark
	}
	warnings := append([]string{}, snap.Warnings...)
	if snap.Error != "" {
		warnings = append([]string{"the last sweep failed and this is the picture before it: " + snap.Error}, warnings...)
	}
	return Inbox(snap.Inbox, dismissed), warnings, nil
}

// Watermarks is the current updated timestamp of each ticket key, as
// the last sweep read it - what an acknowledgement records. A key the
// sweep does not hold is left out, so the caller can say so.
func (s *Service) Watermarks(keys []string) map[string]string {
	snap := s.Snapshot()
	have := make(map[string]string, len(snap.Issues))
	for _, is := range snap.Issues {
		have[is.Key] = rawString(is, "updated")
	}
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		if w, ok := have[k]; ok && w != "" {
			out[k] = w
		}
	}
	return out
}

// InboxWatermarks is the same for inbox item ids.
func (s *Service) InboxWatermarks(ids []string) map[string]string {
	snap := s.Snapshot()
	have := make(map[string]string, len(snap.Inbox))
	for _, it := range snap.Inbox {
		have[it.ID] = it.Watermark
	}
	out := make(map[string]string, len(ids))
	for _, id := range ids {
		if w, ok := have[id]; ok && w != "" {
			out[id] = w
		}
	}
	return out
}

// Reporters lists who reported the open backlog, most frequent first,
// as candidates for the operations roster.
func (s *Service) Reporters() []Candidate {
	snap := s.Snapshot()
	counts := map[string]*Candidate{}
	for _, is := range snap.Issues {
		u := is.Fields.Reporter
		if u == nil || u.AccountID == "" {
			continue
		}
		c, ok := counts[u.AccountID]
		if !ok {
			c = &Candidate{AccountID: u.AccountID, Label: u.DisplayName}
			counts[u.AccountID] = c
		}
		c.Reported++
	}
	out := make([]Candidate, 0, len(counts))
	for _, c := range counts {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Reported != out[j].Reported {
			return out[i].Reported > out[j].Reported
		}
		return out[i].Label < out[j].Label
	})
	return out
}
