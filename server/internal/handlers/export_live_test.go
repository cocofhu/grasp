package handlers

import "github.com/cocofhu/grasp/internal/models"

// HandlersPublicLiveSessionForTest exposes publicLiveSession to handlers_test.
func HandlersPublicLiveSessionForTest() map[string]any {
	return publicLiveSession(&models.LiveSession{ID: "sid001", RunID: "run-secret", NodeID: "p1", Owner: "user:a", State: models.LiveStateReady})
}
