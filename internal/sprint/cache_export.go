package sprint

// CachedReport returns the report already built for a sprint, by the
// sprint's own number, and false when nobody has opened it.
//
// It is what a publish previews from. Nothing is asked of Jira: a
// preview against a report the person has not seen would be a preview
// of nothing they recognise, which is the same reason Propose refuses.
// The page package sits above this one and cannot be imported here, so
// the accessor is exported rather than the publish being done inside.
func (s *Service) CachedReport(sprintNumber int) (Report, bool) {
	entry, found := s.cachedSprint(sprintNumber)
	return entry.report, found
}
