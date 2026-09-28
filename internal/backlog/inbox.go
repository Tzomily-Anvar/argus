package backlog

// The inbox: what has happened to me lately, across every project and
// space.
//
// Neither Jira nor Confluence offers a notifications feed to read, so
// this is assembled from what can be queried. Assignment and watching
// are exact in both. A mention is exact in Confluence, whose query
// language has a field for it, and a best-effort proxy in Jira, where
// text ~ currentUser() empirically finds issues that name you. Each
// source is fetched on its own and a failure becomes a warning on the
// result, so a query Jira stops accepting degrades one list rather than
// the sweep.

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/jira"
)

// Item is one thing in the inbox. ID is source:kind:key, so the same
// issue mentioned and assigned is two items, each dismissed on its own.
type Item struct {
	ID      string    `json:"id"`
	Source  string    `json:"source"`
	Kind    string    `json:"kind"`
	Title   string    `json:"title"`
	Summary string    `json:"summary"`
	URL     string    `json:"url"`
	Updated time.Time `json:"updated"`

	// Watermark is the last-modified timestamp as the source gave it,
	// which a dismissal records and compares against.
	Watermark string `json:"watermark"`
	Dismissed bool   `json:"dismissed"`
}

// The kinds an item can be.
const (
	KindMentioned = "mentioned"
	KindAssigned  = "assigned"
	KindWatching  = "watching"
)

// fetchInbox reads the five sources, newest first, with a warning for
// each one that failed.
func (s *Service) fetchInbox() ([]Item, []string) {
	var items []Item
	var warnings []string
	days := s.cfg.InboxDays

	jiraSources := []struct {
		kind, jql, note string
	}{
		{KindMentioned, "text ~ currentUser()", "Jira's text search is a best-effort proxy for mentions and it"},
		{KindAssigned, "assignee = currentUser()", "the Jira assigned-to-me query"},
		{KindWatching, "watcher = currentUser()", "the Jira watching query"},
	}
	for _, src := range jiraSources {
		jql := fmt.Sprintf("%s AND updated >= -%dd ORDER BY updated DESC", src.jql, days)
		issues, err := s.client.Search(jql, []string{"summary", "issuetype", "status", "updated"}, 0)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s failed, so the inbox lacks it: %v", src.note, err))
			continue
		}
		for _, is := range issues {
			items = append(items, s.jiraItem(is, src.kind))
		}
	}

	confluenceSources := []struct {
		kind, cql, note string
	}{
		{KindMentioned, "mention = currentUser()", "the Confluence mentions query"},
		{KindWatching, "watcher = currentUser()", "the Confluence watching query"},
	}
	for _, src := range confluenceSources {
		cql := fmt.Sprintf(`%s AND lastmodified >= now("-%dd")`, src.cql, days)
		pages, err := s.confluenceSearch(cql)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s failed, so the inbox lacks it: %v", src.note, err))
			continue
		}
		for _, p := range pages {
			items = append(items, s.confluenceItem(p, src.kind))
		}
	}

	sort.SliceStable(items, func(i, j int) bool { return items[i].Updated.After(items[j].Updated) })
	return items, warnings
}

func (s *Service) jiraItem(is jira.Issue, kind string) Item {
	return Item{
		ID: "jira:" + kind + ":" + is.Key, Source: "jira", Kind: kind,
		Title:   is.Key,
		Summary: is.Fields.Summary,
		URL:     s.cfg.BaseURL + "/browse/" + is.Key,
		Updated: is.Fields.Updated.Time, Watermark: rawString(is, "updated"),
	}
}

// confluencePage is the part of a content search result an item needs.
// The timestamp is kept twice: parsed for ordering, and as given for the
// watermark, since only equality with the next reading matters there.
type confluencePage struct {
	ID, Type, Title string
	Space           string
	When            time.Time
	WhenRaw         string
	WebUI           string
}

// confluenceSearch runs one CQL query against the content search, which
// lives beside Jira on the same site with the same credentials.
func (s *Service) confluenceSearch(cql string) ([]confluencePage, error) {
	var answer struct {
		Results []struct {
			ID    string `json:"id"`
			Type  string `json:"type"`
			Title string `json:"title"`
			Space struct {
				Name string `json:"name"`
			} `json:"space"`
			Version struct {
				When string `json:"when"`
			} `json:"version"`
			Links struct {
				WebUI string `json:"webui"`
			} `json:"_links"`
		} `json:"results"`
	}
	params := url.Values{"cql": {cql}, "limit": {"50"}, "expand": {"version,space"}}
	if err := s.client.Get("/wiki/rest/api/content/search", params, &answer); err != nil {
		return nil, err
	}
	out := make([]confluencePage, 0, len(answer.Results))
	for _, r := range answer.Results {
		var when jira.Time
		_ = when.UnmarshalJSON([]byte(strconv.Quote(r.Version.When)))
		out = append(out, confluencePage{
			ID: r.ID, Type: r.Type, Title: r.Title, Space: r.Space.Name,
			When: when.Time, WhenRaw: r.Version.When, WebUI: r.Links.WebUI,
		})
	}
	return out, nil
}

func (s *Service) confluenceItem(p confluencePage, kind string) Item {
	summary := p.Type
	if p.Space != "" {
		summary += " in " + p.Space
	}
	return Item{
		ID: "confluence:" + kind + ":" + p.ID, Source: "confluence", Kind: kind,
		Title:   p.Title,
		Summary: summary,
		URL:     s.cfg.BaseURL + "/wiki" + p.WebUI,
		Updated: p.When, Watermark: p.WhenRaw,
	}
}

// Inbox marks the items a dismissal still covers. A dismissed item whose
// watermark has moved comes back undismissed, which is the mechanism
// working as intended.
func Inbox(items []Item, dismissed map[string]string) []Item {
	out := make([]Item, 0, len(items))
	for _, it := range items {
		it.Dismissed = it.Watermark != "" && dismissed[it.ID] == it.Watermark
		out = append(out, it)
	}
	return out
}
