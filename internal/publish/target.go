package publish

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"text/template"

	"github.com/Tzomily-Anvar/argus/internal/jira"
	"github.com/Tzomily-Anvar/argus/internal/sprint"
	"github.com/Tzomily-Anvar/argus/internal/store"
)

// Finding the sprint's page. The stored id first, because a page
// somebody has renamed is still that page; then a search by title under
// the parent, live title first, the way the pages were always found.
// Every request is a GET through the client, which the gate allows as a
// read like any other.

// found is one page as the lookup returns it.
type found struct {
	id, title, body, url string
	version              int
}

// pageAnswer is the v2 API's page, reduced to what a publish reads.
// Confluence spells every id as a string, and the web link relative to
// /wiki.
type pageAnswer struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	ParentID string `json:"parentId"`
	Version  struct {
		Number int `json:"number"`
	} `json:"version"`
	Body struct {
		Storage struct {
			Value string `json:"value"`
		} `json:"storage"`
	} `json:"body"`
	Links struct {
		WebUI string `json:"webui"`
	} `json:"_links"`
}

// find returns the page for a sprint, or a zero found when there is none
// yet. A stored id whose page has gone is ErrPageGone rather than a
// reason to create another: a page that disappears was somebody's
// decision, and a second copy appearing is not the answer to it.
func (s *Service) find(ctx context.Context, rep sprint.Report) (found, error) {
	if sp, err := s.store.GetSprint(ctx, rep.Sprint.JiraID); err == nil && sp.ConfluencePageID != "" {
		f, err := s.read(sp.ConfluencePageID)
		if jira.StatusCode(err) == http.StatusNotFound {
			return found{}, fmt.Errorf("%w: page %s, recorded for sprint %d; remove the record or restore the page",
				ErrPageGone, sp.ConfluencePageID, rep.Sprint.Number)
		}
		return f, err
	}
	live, err := s.title(rep, true)
	if err != nil {
		return found{}, err
	}
	final, err := s.title(rep, false)
	if err != nil {
		return found{}, err
	}
	for _, title := range []string{live, final} {
		id, err := s.search(title)
		if err != nil {
			return found{}, err
		}
		if id != "" {
			return s.read(id)
		}
	}
	return found{}, nil
}

// search returns the id of the page with this title under the parent, or
// nothing. The API filters by space and title; the parent is checked
// here, because the same title can exist under another parent and that
// page is not this sprint's.
func (s *Service) search(title string) (string, error) {
	var answer struct {
		Results []pageAnswer `json:"results"`
	}
	params := url.Values{"space-id": {s.cfg.SpaceID}, "title": {title}}
	if err := s.client.Get("/wiki/api/v2/pages", params, &answer); err != nil {
		return "", fmt.Errorf("searching for %q: %w", title, err)
	}
	for _, r := range answer.Results {
		if r.ParentID == "" || r.ParentID == s.cfg.ParentPageID {
			return r.ID, nil
		}
	}
	return "", nil
}

// read fetches one page with its storage body, which is what a splice
// and a diff both need, and its version, which is what the conditional
// write needs.
func (s *Service) read(id string) (found, error) {
	var answer pageAnswer
	if err := s.client.Get("/wiki/api/v2/pages/"+id, url.Values{"body-format": {"storage"}}, &answer); err != nil {
		return found{}, err
	}
	return found{
		id: answer.ID, title: answer.Title, version: answer.Version.Number,
		body: answer.Body.Storage.Value, url: s.pageURL(answer.Links.WebUI),
	}, nil
}

// pageURL is where the page opens in a browser: the site, /wiki, and the
// relative link the API gives.
func (s *Service) pageURL(webui string) string {
	if webui == "" {
		return ""
	}
	return s.client.BaseURL() + "/wiki" + webui
}

// title renders the configured template for a sprint, with the live
// suffix when asked. The environment cannot hold a leading space, so one
// is put in when the suffix does not begin with one.
func (s *Service) title(rep sprint.Report, live bool) (string, error) {
	tmpl, err := template.New("title").Parse(s.cfg.Title)
	if err != nil {
		return "", fmt.Errorf("ARGUS_CONFLUENCE_TITLE: %w", err)
	}
	var b strings.Builder
	err = tmpl.Execute(&b, struct {
		Number  int
		Name    string
		Project string
	}{rep.Sprint.Number, rep.Sprint.Name, s.cfg.Project})
	if err != nil {
		return "", fmt.Errorf("ARGUS_CONFLUENCE_TITLE: %w", err)
	}
	title := strings.TrimSpace(b.String())
	if title == "" {
		return "", errors.New("ARGUS_CONFLUENCE_TITLE renders to nothing")
	}
	if !live || s.cfg.LiveSuffix == "" {
		return title, nil
	}
	suffix := s.cfg.LiveSuffix
	if !strings.HasPrefix(suffix, " ") {
		suffix = " " + suffix
	}
	return title + suffix, nil
}

// remember stores the page id on the sprint, so the next preview finds
// the page by id even if it has been renamed. The sprint row is written
// by every sweep, so it is nearly always there; the fallback is for the
// store that somehow lost it.
func (s *Service) remember(ctx context.Context, p Preview, pageID string) error {
	sp, err := s.store.GetSprint(ctx, p.SprintJiraID)
	if err != nil {
		sp = store.Sprint{JiraID: p.SprintJiraID, Number: p.Sprint, Label: p.name}
	}
	sp.ConfluencePageID = pageID
	return s.store.PutSprint(ctx, sp)
}
