package jira

import (
	"net/url"
	"strconv"
)

// What a project's Jira declares about itself, as opposed to what its
// tickets contain. Three reads, every one of them configuration somebody
// set on purpose: the statuses a project's workflows use and the category
// each declares, the field a board estimates in, and the columns the team
// arranged those statuses into. A report that reads these is reading an
// answer rather than guessing at one.

// BoardConfig is the part of a board's configuration a report needs.
type BoardConfig struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`

	// Estimation says whether the board estimates in a field or by issue
	// count, and which field. Somebody chose this when they set the board
	// up, which is what makes it the authority on where points live.
	Estimation struct {
		Type  string `json:"type"` // field | issueCount
		Field struct {
			FieldID     string `json:"fieldId"`
			DisplayName string `json:"displayName"`
		} `json:"field"`
	} `json:"estimation"`

	// Columns are the team's own arrangement of statuses, which
	// corroborates the categories and is never the primary source: a
	// last-column rule silently drops a delivered status sitting in the
	// column before it.
	Columns []BoardColumn `json:"-"`

	ColumnConfig struct {
		Columns []struct {
			Name     string `json:"name"`
			Statuses []struct {
				ID string `json:"id"`
			} `json:"statuses"`
		} `json:"columns"`
	} `json:"columnConfig"`
}

// BoardColumn is one column and the ids of the statuses it holds.
type BoardColumn struct {
	Name      string   `json:"name"`
	StatusIDs []string `json:"status_ids"`
}

// EstimatesByCount reports whether the board counts issues rather than
// points, in which case there is no points field to read at all.
func (b BoardConfig) EstimatesByCount() bool { return b.Estimation.Type == "issueCount" }

// PointsField is the field the board estimates in, empty when it
// estimates by issue count or declares none.
func (b BoardConfig) PointsField() string {
	if b.EstimatesByCount() {
		return ""
	}
	return b.Estimation.Field.FieldID
}

// BoardConfiguration reads one board's configuration from the Agile API.
func (c *Client) BoardConfiguration(boardID int64) (BoardConfig, error) {
	var b BoardConfig
	if err := c.Agile("/board/"+strconv.FormatInt(boardID, 10)+"/configuration", nil, &b); err != nil {
		return BoardConfig{}, err
	}
	for _, col := range b.ColumnConfig.Columns {
		out := BoardColumn{Name: col.Name}
		for _, s := range col.Statuses {
			out.StatusIDs = append(out.StatusIDs, s.ID)
		}
		b.Columns = append(b.Columns, out)
	}
	return b, nil
}

// ProjectStatuses lists every status a project's issue types can be in,
// each with the category it declares. The answer comes grouped by issue
// type and the same status appears under several, so it is flattened
// and deduplicated by status id here - the id is the stable handle, a
// name can be changed without it moving.
func (c *Client) ProjectStatuses(project string) ([]Status, error) {
	var byType []struct {
		Statuses []Status `json:"statuses"`
	}
	if err := c.Get("/rest/api/3/project/"+url.PathEscape(project)+"/statuses", nil, &byType); err != nil {
		return nil, err
	}
	var out []Status
	seen := map[string]bool{}
	for _, t := range byType {
		for _, s := range t.Statuses {
			if s.ID == "" || seen[s.ID] {
				continue
			}
			seen[s.ID] = true
			out = append(out, s)
		}
	}
	return out, nil
}

// Statuses lists every status on the site with its category, which is
// the catalogue a status id on a changelog entry resolves through.
func (c *Client) Statuses() ([]Status, error) {
	var out []Status
	if err := c.Get("/rest/api/3/status", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DoneStatusNames picks out the names of the statuses that declare the
// done category, in the order Jira listed them.
func DoneStatusNames(statuses []Status) []string {
	var out []string
	for _, s := range statuses {
		if s.Category.Key == "done" {
			out = append(out, s.Name)
		}
	}
	return out
}
