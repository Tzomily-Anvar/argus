package sprint

// Importing the roster from an Atlassian team.
//
// The roster is durable setup: who is on the team, and what each of them
// is expected to deliver in a full sprint. It has always been typed in,
// which works and is tedious, and which means an account id gets copied
// by hand.
//
// So it can be proposed from an Atlassian team instead. Proposed, not
// applied: an import that silently replaced the roster would be a way to
// lose a baseline somebody thought about, and would quietly drop the
// people who deliver work without being on that team. What this produces
// is a list of candidates with a state each, and the answer to every one
// of them is a person ticking a box.
//
// Three rules the merge exists to enforce:
//
//   - Somebody already on the roster is never proposed twice, and their
//     baseline is never overwritten by an import.
//   - Somebody who has left the Atlassian team is pointed out, never
//     removed. What they delivered still happened, and a report of an
//     earlier sprint still has to be able to name them.
//   - An inactive or non-human Atlassian account is shown as such and is
//     not ticked by default.

import (
	"context"
	"sort"
	"sync"

	"github.com/Tzomily-Anvar/argus/internal/jira"
	"github.com/Tzomily-Anvar/argus/internal/store"
)

// Candidate states.
const (
	// CandidateNew is on the Atlassian team and not on the roster. This is
	// the one an import is for.
	CandidateNew = "new"

	// CandidateOnRoster is on both. Nothing to do, shown so the list is
	// the whole team rather than the difference, which is easier to check.
	CandidateOnRoster = "on_roster"

	// CandidateDeparted is on the roster and no longer on the Atlassian
	// team. Flagged for a human to decide about; never removed here.
	CandidateDeparted = "departed"
)

// Candidate is one person an import found, and what is known about them.
type Candidate struct {
	AccountID string `json:"account_id"`
	Name      string `json:"name"`
	State     string `json:"state"`

	// Baseline and OptedIn are what the roster already holds, so the panel
	// can show that adding this person would change nothing.
	Baseline float64 `json:"baseline"`
	OptedIn  bool    `json:"opted_in"`

	// AccountActive is the Atlassian account's own state, which is not the
	// same thing as being opted in here: a deactivated account cannot
	// deliver anything, so proposing one is almost always a mistake.
	AccountActive bool `json:"account_active"`

	// Human is false for an app or a service desk account.
	Human bool `json:"human"`

	// Suggested is the box's default state. Only a new, active, human
	// account is ticked for you; everything else is a decision somebody
	// should make deliberately.
	Suggested bool `json:"suggested"`
}

// Import is the whole answer to "who is on the Atlassian team".
type Import struct {
	// Configured is false when the organisation or team id is unset. The
	// panel then explains what to set instead of offering a button that
	// cannot work.
	Configured bool   `json:"configured"`
	Reason     string `json:"reason,omitempty"`

	// TeamName names what is being imported from, so the panel shows the
	// team rather than an id.
	TeamName   string      `json:"team_name,omitempty"`
	Candidates []Candidate `json:"candidates"`
}

// TeamDirectory is the Atlassian team, as this package needs it.
type TeamDirectory interface {
	Team(ctx context.Context) (jira.Team, error)
	MemberIDs(ctx context.Context) ([]string, error)
}

// UserDirectory resolves an account id to a person. The team API answers
// with ids and nothing else, so without this an import would be a list of
// identifiers nobody can tick with any confidence.
type UserDirectory interface {
	User(ctx context.Context, accountID string) (jira.User, error)
}

// resolveConcurrency is how many user lookups run at once. The team is
// people, not thousands of rows, and the Jira client has its own limit
// underneath this one.
const resolveConcurrency = 6

// TeamImport fetches the team, resolves its members to names, and merges
// them with the roster already stored.
func TeamImport(ctx context.Context, dir TeamDirectory, users UserDirectory, roster []store.Person) (Import, error) {
	team, err := dir.Team(ctx)
	if err != nil {
		return Import{}, err
	}
	ids, err := dir.MemberIDs(ctx)
	if err != nil {
		return Import{}, err
	}

	members := resolveAll(ctx, users, ids)
	out := MergeRoster(members, roster)
	out.Configured = true
	out.TeamName = team.DisplayName
	return out, nil
}

// resolveAll turns account ids into people, a few at a time.
//
// Each worker writes to its own index of a slice sized up front, so there
// is no shared map and no mutex around the results. A lookup that fails -
// a deactivated account the token cannot read, most often - leaves the id
// with no name rather than failing the whole import: one unreadable
// account should not stop the other nine being offered.
func resolveAll(ctx context.Context, users UserDirectory, ids []string) []jira.User {
	out := make([]jira.User, len(ids))
	var wg sync.WaitGroup
	work := make(chan int)

	workers := resolveConcurrency
	if len(ids) < workers {
		workers = len(ids)
	}
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				u, err := users.User(ctx, ids[i])
				if err != nil || u.AccountID == "" {
					// Keep the id so the person can still be ticked, and
					// let the merge decide how to name them.
					out[i] = jira.User{AccountID: ids[i]}
					continue
				}
				out[i] = u
			}
		}()
	}
	for i := range ids {
		work <- i
	}
	close(work)
	wg.Wait()
	return out
}

// MergeRoster is the decision, kept pure so it can be tested without an
// Atlassian anywhere near it.
func MergeRoster(members []jira.User, roster []store.Person) Import {
	known := make(map[string]store.Person, len(roster))
	for _, p := range roster {
		known[p.AccountID] = p
	}

	out := Import{Candidates: make([]Candidate, 0, len(members)+len(roster))}
	onTeam := make(map[string]bool, len(members))

	for _, m := range members {
		if m.AccountID == "" {
			continue
		}
		onTeam[m.AccountID] = true

		c := Candidate{
			AccountID:     m.AccountID,
			Name:          m.DisplayName,
			AccountActive: m.Active,
			Human:         m.IsPerson(),
			State:         CandidateNew,
		}
		if p, ok := known[m.AccountID]; ok {
			c.State = CandidateOnRoster
			c.Baseline = p.Baseline
			c.OptedIn = p.Active
			if c.Name == "" {
				c.Name = p.Name
			}
		}
		if c.Name == "" {
			// The account exists on the team but this token cannot read
			// it. Better a row that says so than a bare identifier.
			c.Name = "Unnamed Atlassian account"
		}
		c.Suggested = c.State == CandidateNew && c.AccountActive && c.Human
		out.Candidates = append(out.Candidates, c)
	}

	// Anyone on the roster the team no longer has. Only the opted-in are
	// worth raising: somebody already opted out is a decision that has
	// been made, and repeating it every import would train people to
	// ignore the list.
	for _, p := range roster {
		if onTeam[p.AccountID] || !p.Active {
			continue
		}
		out.Candidates = append(out.Candidates, Candidate{
			AccountID: p.AccountID,
			Name:      p.Name,
			State:     CandidateDeparted,
			Baseline:  p.Baseline,
			OptedIn:   p.Active,
			// Nothing is known about the account itself here - the team no
			// longer lists it, so it was never looked up.
			Human: true,
		})
	}

	// New people first, because they are what there is to do; then the
	// departures, which need a decision; then the rest, which is context.
	rank := map[string]int{CandidateNew: 0, CandidateDeparted: 1, CandidateOnRoster: 2}
	sort.SliceStable(out.Candidates, func(i, j int) bool {
		a, b := out.Candidates[i], out.Candidates[j]
		if rank[a.State] != rank[b.State] {
			return rank[a.State] < rank[b.State]
		}
		return a.Name < b.Name
	})
	return out
}

// NotConfigured is the answer when no Atlassian team is set. It is an
// ordinary answer rather than an error: most people running Argus will
// never set one, and the panel shows the explanation in place of the
// button.
func NotConfigured(reason string) Import {
	return Import{Configured: false, Reason: reason, Candidates: []Candidate{}}
}
