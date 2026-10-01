package backlog

import (
	"context"

	"github.com/Tzomily-Anvar/argus/internal/jira"
)

// User resolves an account id to a person, which the team import needs:
// the team API answers with ids and nothing else. Read-only.
func (s *Service) User(_ context.Context, accountID string) (jira.User, error) {
	return s.client.User(accountID)
}
