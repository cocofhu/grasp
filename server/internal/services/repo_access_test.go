package services

import (
	"testing"

	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
)

func TestResolveRepoCredentialsKeepsSelectionOrder(t *testing.T) {
	db := newTestDB(t)
	for _, p := range []models.Project{
		{ID: "p1", Name: "one", RepoURL: "https://github.com/acme/one.git", GitCredentialType: "github_https", GitCredentialID: "gh1"},
		{ID: "p2", Name: "two", RepoURL: "https://github.com/acme/two.git", GitCredentialType: "github_https", GitCredentialID: "gh2"},
	} {
		if err := db.Create(&p).Error; err != nil {
			t.Fatal(err)
		}
	}
	resolve := func(id string) (runtime.GitCredentialValue, bool) {
		return runtime.GitCredentialValue{EnvKey: envauth.EnvGitHubToken, Secret: "tok-" + id, Host: "https://github.com"}, true
	}
	got := ResolveRepoCredentials(db, []string{"p2", "deleted", "p1"}, resolve)
	if len(got) != 2 || got[0].Secret != "tok-gh2" || got[1].Secret != "tok-gh1" {
		t.Fatalf("got %+v", got)
	}
}
