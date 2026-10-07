package auth

import "github.com/cocofhu/grasp/internal/models"

// Test-only helpers (compiled with tests only) so auth_test can mint sessions
// without exporting production API surface.

func (s *Service) CreateSession(username string) (*models.Session, error) {
	s.cleanupExpired()
	return s.createSession(username)
}
