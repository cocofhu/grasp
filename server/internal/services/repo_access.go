package services

import (
	"strings"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/sandbox"
	"gorm.io/gorm"
)

// NormalizeRepoIDs trims, drops empty and de-duplicates repository ids while
// keeping their order. The result is never nil.
func NormalizeRepoIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := map[string]struct{}{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// ResolveRepoCredentials loads the selected repositories in selection order
// and resolves their own Git credentials. Deleted repositories are skipped.
func ResolveRepoCredentials(db *gorm.DB, ids []string, resolve runtime.GitCredentialFunc) []sandbox.RepoCredential {
	ids = NormalizeRepoIDs(ids)
	if db == nil || len(ids) == 0 || resolve == nil {
		return nil
	}
	var rows []models.Project
	if err := db.Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil
	}
	byID := make(map[string]models.Project, len(rows))
	for _, p := range rows {
		byID[p.ID] = p
	}
	ordered := make([]models.Project, 0, len(rows))
	for _, id := range ids {
		if p, ok := byID[id]; ok {
			ordered = append(ordered, p)
		}
	}
	return runtime.RepoCredentialsFor(ordered, resolve)
}
