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

// normalizeRepoIDsPtr keeps nil (inherit) distinct from an explicit list.
func normalizeRepoIDsPtr(ids *[]string) *[]string {
	if ids == nil {
		return nil
	}
	out := NormalizeRepoIDs(*ids)
	return &out
}

// ResolveRepoCredentials loads the selected repositories in selection order
// and resolves their own Git credentials. Deleted repositories are skipped.
func ResolveRepoCredentials(db *gorm.DB, ids []string, resolve runtime.SelectedCredentialFunc) []sandbox.RepoCredential {
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

// repoCredentials resolves an effective Agent's repository selection.
func (s *SandboxService) repoCredentials(agent Agent) []sandbox.RepoCredential {
	if s == nil || agent.RepoIDs == nil {
		return nil
	}
	return ResolveRepoCredentials(s.db, *agent.RepoIDs, s.selectedCredential)
}

// mergeRepoCredentials appends extra entries, replacing any earlier one with
// the same repository URL (bound Solo repo wins over the Agent list).
func mergeRepoCredentials(base, extra []sandbox.RepoCredential) []sandbox.RepoCredential {
	if len(extra) == 0 {
		return base
	}
	key := func(url string) string {
		return strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(url), "/"), ".git")
	}
	byURL := map[string]int{}
	out := make([]sandbox.RepoCredential, 0, len(base)+len(extra))
	for _, r := range base {
		byURL[key(r.URL)] = len(out)
		out = append(out, r)
	}
	for _, r := range extra {
		k := key(r.URL)
		if i, ok := byURL[k]; ok {
			out[i] = r
			continue
		}
		byURL[k] = len(out)
		out = append(out, r)
	}
	return out
}
