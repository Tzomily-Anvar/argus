package sprint_test

// The merge is the part of the import that can lose something, so it is
// the part with tests: a baseline someone thought about, or a person who
// left the team and whose delivery still has to be nameable.

import (
	"context"
	"errors"
	"testing"

	"github.com/Tzomily-Anvar/argus/internal/jira"
	"github.com/Tzomily-Anvar/argus/internal/sprint"
	"github.com/Tzomily-Anvar/argus/internal/store"
)

func candidate(t *testing.T, in sprint.Import, accountID string) sprint.Candidate {
	t.Helper()
	for _, c := range in.Candidates {
		if c.AccountID == accountID {
			return c
		}
	}
	t.Fatalf("%s is not among the candidates: %+v", accountID, in.Candidates)
	return sprint.Candidate{}
}

func TestMergeProposesSomebodyNew(t *testing.T) {
	in := sprint.MergeRoster(
		[]jira.User{{AccountID: "acc-new", DisplayName: "New Person", Active: true, AccountType: "atlassian"}},
		nil,
	)

	c := candidate(t, in, "acc-new")
	if c.State != sprint.CandidateNew {
		t.Errorf("state = %q, want %q", c.State, sprint.CandidateNew)
	}
	if !c.Suggested {
		t.Error("an active person who is not on the roster is the whole point of an import; it should be ticked")
	}
	if c.Baseline != 0 {
		t.Errorf("baseline = %v, want nothing invented", c.Baseline)
	}
}

// Somebody already on the roster must appear once, and their baseline
// must be reported rather than replaced: an import that reset a
// considered figure to zero would be worse than no import.
func TestMergeDoesNotDuplicateOrOverwrite(t *testing.T) {
	in := sprint.MergeRoster(
		[]jira.User{{AccountID: "acc-known", DisplayName: "Known Person", Active: true, AccountType: "atlassian"}},
		[]store.Person{{AccountID: "acc-known", Name: "Known Person", Baseline: 9, Active: true}},
	)

	if len(in.Candidates) != 1 {
		t.Fatalf("%d candidates, want one: %+v", len(in.Candidates), in.Candidates)
	}
	c := in.Candidates[0]
	if c.State != sprint.CandidateOnRoster {
		t.Errorf("state = %q, want %q", c.State, sprint.CandidateOnRoster)
	}
	if c.Baseline != 9 {
		t.Errorf("baseline = %v, want the stored 9 reported back", c.Baseline)
	}
	if c.Suggested {
		t.Error("somebody already on the roster should not be ticked; there is nothing to do")
	}
}

// Leaving the Atlassian team is not leaving the history. The person is
// raised for a decision and never removed here.
func TestMergeFlagsSomebodyWhoHasLeftTheTeam(t *testing.T) {
	in := sprint.MergeRoster(
		[]jira.User{{AccountID: "acc-stays", DisplayName: "Stays", Active: true, AccountType: "atlassian"}},
		[]store.Person{
			{AccountID: "acc-stays", Name: "Stays", Baseline: 10, Active: true},
			{AccountID: "acc-gone", Name: "Gone", Baseline: 8, Active: true},
		},
	)

	c := candidate(t, in, "acc-gone")
	if c.State != sprint.CandidateDeparted {
		t.Errorf("state = %q, want %q", c.State, sprint.CandidateDeparted)
	}
	if c.Suggested {
		t.Error("a departure is a decision for a person, not a box to tick for them")
	}
	if c.Baseline != 8 {
		t.Errorf("baseline = %v, want the stored figure kept", c.Baseline)
	}
}

// Somebody already opted out and no longer on the team is a settled
// question. Raising it every import is how a list stops being read.
func TestMergeIsQuietAboutSomebodyAlreadyOptedOut(t *testing.T) {
	in := sprint.MergeRoster(
		nil,
		[]store.Person{{AccountID: "acc-past", Name: "Past", Baseline: 8, Active: false}},
	)
	if len(in.Candidates) != 0 {
		t.Errorf("candidates = %+v, want nothing raised", in.Candidates)
	}
}

func TestMergeShowsAnInactiveAccountAsSuch(t *testing.T) {
	in := sprint.MergeRoster(
		[]jira.User{
			{AccountID: "acc-off", DisplayName: "Deactivated", Active: false, AccountType: "atlassian"},
			{AccountID: "acc-bot", DisplayName: "Automation", Active: true, AccountType: "app"},
		},
		nil,
	)

	off := candidate(t, in, "acc-off")
	if off.AccountActive {
		t.Error("a deactivated account is reported as active")
	}
	if off.Suggested {
		t.Error("a deactivated account should not be ticked for you")
	}

	bot := candidate(t, in, "acc-bot")
	if bot.Human {
		t.Error("an app account is reported as a person")
	}
	if bot.Suggested {
		t.Error("an app should not be proposed as a member of the team")
	}
}

// ---- the whole import ------------------------------------------------

type fakeTeam struct {
	name    string
	ids     []string
	teamErr error
	memErr  error
}

func (f fakeTeam) Team(context.Context) (jira.Team, error) {
	return jira.Team{DisplayName: f.name}, f.teamErr
}
func (f fakeTeam) MemberIDs(context.Context) ([]string, error) { return f.ids, f.memErr }

type fakeUsers map[string]jira.User

func (f fakeUsers) User(_ context.Context, id string) (jira.User, error) {
	u, ok := f[id]
	if !ok {
		return jira.User{}, errors.New("no such user")
	}
	return u, nil
}

func TestTeamImportResolvesNames(t *testing.T) {
	dir := fakeTeam{name: "Platform", ids: []string{"acc-a", "acc-b", "acc-c"}}
	users := fakeUsers{
		"acc-a": {AccountID: "acc-a", DisplayName: "Person A", Active: true, AccountType: "atlassian"},
		"acc-b": {AccountID: "acc-b", DisplayName: "Person B", Active: true, AccountType: "atlassian"},
		// acc-c is deliberately absent: an account this token cannot read.
	}

	in, err := sprint.TeamImport(context.Background(), dir, users, nil)
	if err != nil {
		t.Fatalf("TeamImport: %v", err)
	}
	if !in.Configured || in.TeamName != "Platform" {
		t.Errorf("import = %+v, want the team named", in)
	}
	if len(in.Candidates) != 3 {
		t.Fatalf("%d candidates, want all three", len(in.Candidates))
	}
	if got := candidate(t, in, "acc-a").Name; got != "Person A" {
		t.Errorf("name = %q, want it resolved", got)
	}
	// One unreadable account must not cost the other two.
	unreadable := candidate(t, in, "acc-c")
	if unreadable.Name == "" {
		t.Error("an unresolved account has no name at all, so nothing can be ticked with confidence")
	}
	if unreadable.Suggested {
		t.Error("an account that could not be read should not be ticked for you")
	}
}

func TestTeamImportReportsAFailureRatherThanHalfATeam(t *testing.T) {
	_, err := sprint.TeamImport(context.Background(),
		fakeTeam{memErr: errors.New("403")}, fakeUsers{}, nil)
	if err == nil {
		t.Error("a failed member listing came back as an empty team, which reads as nobody being on it")
	}
}

// Missing configuration is an ordinary answer. The panel explains what to
// set instead of offering a button that cannot work.
func TestNotConfiguredCarriesTheReason(t *testing.T) {
	in := sprint.NotConfigured("set ARGUS_ATLASSIAN_ORG_ID and ARGUS_ATLASSIAN_TEAM_ID")
	if in.Configured {
		t.Error("configured = true with nothing configured")
	}
	if in.Reason == "" {
		t.Error("no reason, so the panel has nothing to say")
	}
	if in.Candidates == nil {
		t.Error("candidates is null rather than empty, which the browser has to special-case")
	}
}
