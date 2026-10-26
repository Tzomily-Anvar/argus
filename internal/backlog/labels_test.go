package backlog

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/sweep"
)

func TestLabelWords(t *testing.T) {
	cases := map[string][]string{
		"argus-api":        {"argus", "api"},
		"argus_api":        {"argus", "api"},
		"argusApi":         {"argus", "Api"},
		"ArgusApi":         {"Argus", "Api"},
		"Argus_API":        {"Argus", "API"},
		"APIGateway":       {"API", "Gateway"},
		"s3-bucket.policy": {"s3", "bucket", "policy"},
		"v2Client":         {"v2", "Client"},
		"ops":              {"ops"},
		"--":               nil,
		"":                 nil,
	}
	for in, want := range cases {
		if got := LabelWords(in); !reflect.DeepEqual(got, want) {
			t.Errorf("LabelWords(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSpellLabel(t *testing.T) {
	cases := []struct{ in, style, want string }{
		{"Argus_API", "kebab", "argus-api"},
		{"argus-api", "snake", "argus_api"},
		{"argus-api", "camel", "argusApi"},
		{"argus-api", "pascal", "ArgusApi"},
		{"APIGateway", "kebab", "api-gateway"},
		{"Argus_API", "any", "Argus_API"},
		{"Operations", "kebab", "operations"},
		{"ops", "pascal", "Ops"},
		{"--", "kebab", "--"},
		{"auth-api", "train", "Auth-Api"},
		{"Auth-API", "train", "Auth-API"},
		{"HTTPClient", "train", "HTTP-Client"},
		{"billing_report", "train", "Billing-Report"},
	}
	for _, c := range cases {
		got := SpellLabel(c.in, c.style)
		if got != c.want {
			t.Errorf("SpellLabel(%q, %s) = %q, want %q", c.in, c.style, got, c.want)
		}
		// Respelling never changes which label it is.
		if LabelKey(got) != LabelKey(c.in) {
			t.Errorf("SpellLabel(%q, %s) changed the key: %q", c.in, c.style, got)
		}
	}
	if !LabelFits("ops", "kebab") || !LabelFits("ops", "camel") || LabelFits("ops", "pascal") {
		t.Error("a one-word lower-case label should fit kebab and camel, not pascal")
	}
	if !LabelFits("Auth-API", "train") || LabelFits("PaymentGateway", "train") || !LabelFits("DevOps", "pascal") {
		t.Error("train keeps an acronym and wants hyphens between words")
	}
}

func TestLabelSpelling(t *testing.T) {
	inUse := []LabelCount{
		{Name: "argus-api", Count: 9},
		{Name: "argusApi", Count: 3},
		{Name: "Argus_API", Count: 1},
		{Name: "web_app", Count: 4},
		{Name: "Operations", Count: 6},
		{Name: "operations", Count: 2},
		{Name: "Billing", Count: 5},
		{Name: "infra", Count: 7},
	}

	t.Run("kebab", func(t *testing.T) {
		got := LabelSpelling(inUse, "kebab", []string{"Operations"}, nil)
		want := []SpellingFinding{
			// Most tickets to change first: five, then four, four, two.
			{Target: "billing", Reason: "style", Variants: []LabelCount{{Name: "Billing", Count: 5}}},
			{Target: "argus-api", Reason: "spellings", Variants: []LabelCount{{Name: "argusApi", Count: 3}, {Name: "Argus_API", Count: 1}}},
			{Target: "web-app", Reason: "style", Variants: []LabelCount{{Name: "web_app", Count: 4}}},
			// The request label is the spelling to keep, against the style.
			{Target: "Operations", Reason: "spellings", Variants: []LabelCount{{Name: "operations", Count: 2}}},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got  %+v\nwant %+v", got, want)
		}
	})

	t.Run("any style still finds two spellings", func(t *testing.T) {
		got := LabelSpelling(inUse, "any", nil, nil)
		if len(got) != 2 || got[0].Target != "argus-api" || got[1].Target != "Operations" {
			t.Errorf("got %+v, want the two multi-spelt labels kept at their most used", got)
		}
	})

	t.Run("no spelling fits the style", func(t *testing.T) {
		got := LabelSpelling([]LabelCount{{Name: "argusApi", Count: 2}, {Name: "Argus_API", Count: 1}}, "snake", nil, nil)
		if len(got) != 1 || got[0].Target != "argus_api" || len(got[0].Variants) != 2 {
			t.Errorf("got %+v, want both migrated to a new argus_api", got)
		}
	})

	t.Run("a repository's own name is kept before a more used spelling", func(t *testing.T) {
		got := LabelSpelling([]LabelCount{{Name: "WidgetStore", Count: 9}, {Name: "Widgetstore", Count: 1}}, "any", nil, []string{"Widgetstore"})
		if len(got) != 1 || got[0].Target != "Widgetstore" {
			t.Errorf("got %+v, want WidgetStore moved to the repository's Widgetstore", got)
		}
	})

	t.Run("a chosen label is an exception to the style", func(t *testing.T) {
		got := LabelSpelling([]LabelCount{{Name: "DevOps", Count: 19}, {Name: "OrderTracker", Count: 3}}, "train", []string{"DevOps"}, nil)
		if len(got) != 1 || got[0].Target != "Order-Tracker" {
			t.Errorf("got %+v, want only OrderTracker respelt", got)
		}
	})
}

func TestTallyLabelStyles(t *testing.T) {
	got := TallyLabelStyles([]LabelCount{
		{Name: "argus-api"}, {Name: "web-app"}, {Name: "web_app"}, {Name: "argusApi"},
		{Name: "ArgusApi"}, {Name: "Argus_API"}, {Name: "infra"}, {Name: "Billing"},
		{Name: "Auth-API"}, {Name: "Billing-Report"},
	})
	want := StyleTally{Kebab: 2, Snake: 1, Camel: 1, Pascal: 1, Train: 2, Other: 1}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestRepoLabels(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	repos := []sweep.Repo{
		{Name: "argus-api", PushedAt: now.Add(-24 * time.Hour)},
		{Name: "web-app", PushedAt: now.Add(-10 * 24 * time.Hour)},
		{Name: "billing-service", PushedAt: now.Add(-48 * time.Hour)},
		{Name: "old-thing", PushedAt: now.Add(-200 * 24 * time.Hour)},
	}
	inUse := []LabelCount{{Name: "argusApi", Count: 3}, {Name: "argus-api", Count: 9}}
	site := []string{"WebApp"}

	active := ActiveRepos(repos, 90, now)
	if strings.Join(active, ",") != "argus-api,web-app,billing-service" {
		t.Errorf("active = %q, want all but the one untouched for 200 days", active)
	}
	got := RepoLabels(active, inUse, site, "kebab")
	want := []RepoLabel{
		// The team's spelling is already in use, so it is the one offered.
		{Repo: "argus-api", Label: "argus-api", Count: 9},
		// Nobody has used it; it is offered in the style.
		{Repo: "billing-service", Label: "billing-service"},
		// Only the site has it, spelt otherwise: that spelling, not a third.
		{Repo: "web-app", Label: "WebApp"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
	if got := ActiveRepos(repos, 0, now); got == nil || len(got) != 0 {
		t.Errorf("0 days should offer none, got %+v", got)
	}
}

func TestLabelsElsewhere(t *testing.T) {
	got := LabelsElsewhere([]string{"zeta", "alpha", "infra", "Alpha"}, []LabelCount{{Name: "infra"}})
	// Case apart, the order is still fixed: capitals first.
	if want := []string{"Alpha", "alpha", "zeta"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}
