package services

import (
	"testing"

	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func TestExtendOverlayRepoIDs(t *testing.T) {
	shared := SharedAgentConfig{RepoIDs: []string{"a", "b"}}
	inherit := ExtendOverlay(shared, Agent{Name: "x"})
	if inherit.RepoIDs == nil || len(*inherit.RepoIDs) != 2 || (*inherit.RepoIDs)[1] != "b" {
		t.Fatalf("inherit = %v", inherit.RepoIDs)
	}
	own := []string{"c", " c ", ""}
	override := ExtendOverlay(shared, Agent{Name: "x", RepoIDs: &own})
	if override.RepoIDs == nil || len(*override.RepoIDs) != 1 || (*override.RepoIDs)[0] != "c" {
		t.Fatalf("override = %v", override.RepoIDs)
	}
	none := []string{}
	empty := ExtendOverlay(shared, Agent{Name: "x", RepoIDs: &none})
	if empty.RepoIDs == nil || len(*empty.RepoIDs) != 0 {
		t.Fatalf("explicit empty list must not inherit: %v", empty.RepoIDs)
	}
}

func TestAgentRepoIDsRoundTrip(t *testing.T) {
	svc := NewAgentService(t.TempDir())
	if err := svc.Save(Agent{Name: "inherit", AcpBackend: AcpBackendCursor}); err != nil {
		t.Fatal(err)
	}
	if a, _ := svc.Get("inherit"); a.RepoIDs != nil {
		t.Fatalf("unset must stay nil (inherit), got %v", *a.RepoIDs)
	}
	none := []string{}
	if err := svc.Save(Agent{Name: "none", AcpBackend: AcpBackendCursor, RepoIDs: &none}); err != nil {
		t.Fatal(err)
	}
	if a, _ := svc.Get("none"); a.RepoIDs == nil || len(*a.RepoIDs) != 0 {
		t.Fatalf("explicit empty list lost: %v", a.RepoIDs)
	}
	ids := []string{"r2", "r1", "r2"}
	if err := svc.Save(Agent{Name: "some", AcpBackend: AcpBackendCursor, RepoIDs: &ids}); err != nil {
		t.Fatal(err)
	}
	if a, _ := svc.Get("some"); a.RepoIDs == nil || len(*a.RepoIDs) != 2 || (*a.RepoIDs)[0] != "r2" {
		t.Fatalf("ids = %v", a.RepoIDs)
	}

	shared := NewSharedAgentService(t.TempDir())
	if err := shared.Save(SharedAgentConfig{RepoIDs: []string{"r1", " ", "r1"}}); err != nil {
		t.Fatal(err)
	}
	if got := shared.Get().RepoIDs; len(got) != 1 || got[0] != "r1" {
		t.Fatalf("shared ids = %v", got)
	}
}

func TestMergeRepoCredentialsBoundRepoWins(t *testing.T) {
	base := []sandbox.RepoCredential{{URL: "https://github.com/acme/app.git", Kind: "github_https", Secret: "agent"}}
	extra := []sandbox.RepoCredential{
		{URL: "https://github.com/acme/app", Kind: "github_https", Secret: "bound"},
		{URL: "https://github.com/acme/other.git", Kind: "github_https", Secret: "other"},
	}
	got := mergeRepoCredentials(base, extra)
	if len(got) != 2 || got[0].Secret != "bound" || got[1].Secret != "other" {
		t.Fatalf("got %+v", got)
	}
}

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
	resolve := func(id string) (string, string, bool) {
		return envauth.EnvGitHubToken, "tok-" + id, true
	}
	got := ResolveRepoCredentials(db, []string{"p2", "deleted", "p1"}, resolve)
	if len(got) != 2 || got[0].Secret != "tok-gh2" || got[1].Secret != "tok-gh1" {
		t.Fatalf("got %+v", got)
	}
}
