package backlog

// What the sweep has to learn about the site before it can read a
// ticket: the custom field ids, the board, and which issue types sit at
// which level of the hierarchy. Resolved once and kept, because none of
// it changes while the process runs.

import (
	"fmt"
	"net/url"
)

// Fields is what the sweep learned about the site, resolved once: the
// custom field ids, the board, and which issue types sit at which level
// of the hierarchy.
type Fields struct {
	Points   string
	Estimate string
	Sprint   string
	BoardID  int64

	// EpicTypes are the issue types above the standard level - Epic on
	// most sites - which is what "the parent is an epic" means.
	EpicTypes map[string]bool

	// HasContainer reports that one of the container types exists at the
	// standard level of this project's hierarchy. Without one there is no
	// Story for a Task to be under, and the groups that ask are skipped
	// rather than listing every ticket.
	HasContainer bool
}

// resolve looks up the site-specific ids once and keeps them. A read
// that fails is not kept, so the next sweep tries again; a site that
// simply lacks something is kept with a warning, because asking again
// would only return the same answer.
func (s *Service) resolve() (*Fields, []string, error) {
	s.mu.RLock()
	f := s.fields
	s.mu.RUnlock()
	if f != nil {
		return f, nil, nil
	}

	f = &Fields{EpicTypes: map[string]bool{"Epic": true}, HasContainer: len(s.cfg.ContainerTypes) > 0}
	var warnings []string
	retry := false

	f.Sprint = s.cfg.SprintField
	if f.Sprint == "" {
		id, err := s.client.FieldID(s.cfg.SprintFieldName)
		if err != nil {
			return nil, nil, fmt.Errorf("resolving the %s field: %w", s.cfg.SprintFieldName, err)
		}
		if id == "" {
			return nil, nil, fmt.Errorf("no field named %s on this Jira site", s.cfg.SprintFieldName)
		}
		f.Sprint = id
	}

	// The board is where the sprints to assign to live, and where the
	// points field is declared. A project without a scrum board has
	// neither, which is a shape rather than a failure.
	board, err := s.board()
	switch {
	case err != nil:
		warnings = append(warnings, fmt.Sprintf("could not read the project's board: %v", err))
		retry = true
	case board == 0:
		warnings = append(warnings, "no scrum board is associated with the project, so there are no sprints to assign to")
	default:
		f.BoardID = board
	}

	f.Points = s.cfg.PointsField
	if f.Points == "" && f.BoardID > 0 {
		if b, err := s.client.BoardConfiguration(f.BoardID); err != nil {
			warnings = append(warnings, fmt.Sprintf("could not read the board's estimation field: %v", err))
			retry = true
		} else {
			f.Points = b.PointsField()
		}
	}
	if f.Points == "" {
		if f.Points, err = s.client.FieldID(s.cfg.PointsFieldName); err != nil {
			return nil, nil, fmt.Errorf("resolving the %s field: %w", s.cfg.PointsFieldName, err)
		}
	}
	if f.Points == "" {
		warnings = append(warnings, fmt.Sprintf("no points field found by the board or by the name %q, so only the estimate says whether a ticket is sized", s.cfg.PointsFieldName))
	}
	f.Estimate = s.cfg.EstimateField
	if f.Estimate == "" {
		if f.Estimate, err = s.client.FieldID(s.cfg.EstimateFieldName); err != nil {
			return nil, nil, fmt.Errorf("resolving the %s field: %w", s.cfg.EstimateFieldName, err)
		}
	}

	if err := s.hierarchy(f); err != nil {
		warnings = append(warnings, fmt.Sprintf("could not read the project's issue types, so Epic is taken to be the epic type: %v", err))
		retry = true
	}

	if !retry {
		s.mu.Lock()
		if s.fields == nil {
			s.fields = f
		}
		s.mu.Unlock()
	}
	return f, warnings, nil
}

// board finds the project's scrum board, zero when it has none.
func (s *Service) board() (int64, error) {
	var page struct {
		Values []struct {
			ID int64 `json:"id"`
		} `json:"values"`
	}
	params := url.Values{"projectKeyOrId": {s.cfg.Project}, "type": {"scrum"}, "maxResults": {"1"}}
	if err := s.client.Agile("/board", params, &page); err != nil {
		return 0, err
	}
	if len(page.Values) == 0 {
		return 0, nil
	}
	return page.Values[0].ID, nil
}

// hierarchy reads which of the project's issue types sit above the
// standard level and whether a container type sits on it.
func (s *Service) hierarchy(f *Fields) error {
	var project struct {
		IssueTypes []struct {
			Name           string `json:"name"`
			HierarchyLevel int    `json:"hierarchyLevel"`
		} `json:"issueTypes"`
	}
	if err := s.client.Get("/rest/api/3/project/"+url.PathEscape(s.cfg.Project), nil, &project); err != nil {
		return err
	}
	if len(project.IssueTypes) == 0 {
		return nil
	}
	epics := map[string]bool{}
	hasContainer := false
	for _, t := range project.IssueTypes {
		if t.HierarchyLevel >= 1 {
			epics[t.Name] = true
		}
		if t.HierarchyLevel == 0 && hasFold(s.cfg.ContainerTypes, t.Name) {
			hasContainer = true
		}
	}
	if len(epics) > 0 {
		f.EpicTypes = epics
	}
	f.HasContainer = hasContainer
	return nil
}
