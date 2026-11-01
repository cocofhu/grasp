package services

import (
	"context"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"

	"gorm.io/gorm/clause"
)

// ReferenceScope stands in for a Solo session outside Solo: the sender, the
// Agent whose Git credential checks GitHub access, and the repositories that
// GitHub search covers. Workflow launches and run chats build one per request.
func ReferenceScope(user, agentName string, repoURLs []string) *models.SoloSession {
	row := &models.SoloSession{UserID: user, AgentName: strings.TrimSpace(agentName)}
	for i, u := range repoURLs {
		if u = strings.TrimSpace(u); u != "" {
			row.Repos = append(row.Repos, models.SessionRepo{Position: i, RepoURL: u})
		}
	}
	return row
}

// SearchScopedReferences lists @ candidates inside scope: the sender's own
// sessions, and issues / PRs of the scope repositories.
func (s *SoloService) SearchScopedReferences(ctx context.Context, scope *models.SoloSession, q ReferenceSearchQuery) (ReferenceSearchResult, error) {
	return s.searchReferences(ctx, scope, q)
}

// PrepareScopedReferences validates @ picks sent with a workflow message, with
// the same rules as a Solo turn. Each result carries the sender in UserID.
func (s *SoloService) PrepareScopedReferences(ctx context.Context, scope *models.SoloSession, inputs []ReferenceInput) ([]models.MessageReference, error) {
	refs, err := s.prepareReferences(ctx, scope, inputs)
	if err != nil {
		return nil, err
	}
	for i := range refs {
		refs[i].UserID = scope.UserID
	}
	return refs, nil
}

// PinRunReferences fixes the snapshot of each Solo reference when a workflow
// turn begins and records every reference with its sender, so read_reference
// can authorize later reads of this run.
func (s *SoloService) PinRunReferences(runID, nodeID, userID string, refs []models.MessageReference) []models.MessageReference {
	if len(refs) == 0 {
		return nil
	}
	out := s.pinReferences(&models.SoloSession{UserID: userID}, refs)
	rows := make([]models.RunReference, 0, len(out))
	for i := range out {
		out[i].UserID = userID
		ref := out[i]
		created := ref.CreatedAt
		if created.IsZero() {
			created = time.Now()
		}
		rows = append(rows, models.RunReference{ID: ref.ID, RunID: runID, NodeID: nodeID, UserID: userID,
			Kind: ref.Kind, Target: ref.Target, Title: ref.Title, Anchor: ref.Anchor, CreatedAt: created})
	}
	// A turn restored after a restart pins again; the latest anchor wins.
	if err := s.db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&rows).Error; err != nil {
		for i := range out {
			if out[i].Kind == models.ReferenceKindSolo && out[i].Error == "" {
				out[i].Error = "snapshot_unavailable"
			}
		}
	}
	return out
}

// RenderReferenceBlock is the prompt block for pinned references, the same
// block a Solo turn receives.
func (s *SoloService) RenderReferenceBlock(refs []models.MessageReference) string {
	return s.renderReferenceBlock(refs)
}
