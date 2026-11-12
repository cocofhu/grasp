package services

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func projectErrCode(err error) string {
	var se *SoloError
	if errors.As(err, &se) {
		return se.Code
	}
	return ""
}

func TestProjectValidatesCredentialAndCommitAuthor(t *testing.T) {
	s := NewProjectService(newTestDB(t))
	base := ProjectInput{Name: "web", RepoURL: "https://github.com/team/web.git"}
	if _, err := s.Create(ProjectInput{Name: "x", RepoURL: base.RepoURL, CredentialID: "gh"}, ""); projectErrCode(err) != "credential_unavailable" {
		t.Fatalf("missing resolver = %v", err)
	}
	s.SetGitCredentialResolver(serviceGitValue)
	for _, tc := range []struct {
		in   ProjectInput
		code string
	}{
		{ProjectInput{CredentialID: "gone"}, "credential_invalid"},
		{ProjectInput{CredentialID: "gl"}, "credential_invalid"},
		{ProjectInput{CommitName: "Ada <ada@example.com>"}, "commit_name_invalid"},
		{ProjectInput{CommitName: "Ada\nLovelace"}, "commit_name_invalid"},
		{ProjectInput{CommitName: strings.Repeat("a", 121)}, "commit_name_invalid"},
		{ProjectInput{CommitEmail: "not-an-email"}, "commit_email_invalid"},
		{ProjectInput{CommitEmail: "a b@example.com"}, "commit_email_invalid"},
	} {
		in := base
		in.CredentialID, in.CommitName, in.CommitEmail = tc.in.CredentialID, tc.in.CommitName, tc.in.CommitEmail
		if _, err := s.Create(in, ""); projectErrCode(err) != tc.code {
			t.Fatalf("%+v: err = %v, want %s", tc.in, err, tc.code)
		}
	}
	in := base
	in.CredentialID, in.CommitName, in.CommitEmail = " gh ", " Ada Lovelace ", " ada@example.com "
	p, err := s.Create(in, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.CredentialID != "gh" || p.CommitName != "Ada Lovelace" || p.CommitEmail != "ada@example.com" {
		t.Fatalf("project = %+v", p)
	}
	in.RepoURL = "git@github.com:team/web.git"
	if _, err := s.Update(p.ID, in); projectErrCode(err) != "credential_invalid" {
		t.Fatalf("token kept on an SSH address: %v", err)
	}
	in.CredentialID = ""
	if updated, err := s.Update(p.ID, in); err != nil || updated.CredentialID != "" {
		t.Fatalf("clearing the credential = %+v, %v", updated, err)
	}
}

func TestProjectDeleteUnbindsWorkflows(t *testing.T) {
	db := newTestDB(t)
	projects := NewProjectService(db)
	workflows := NewWorkflowService(db)
	web, err := projects.Create(ProjectInput{Name: "web", RepoURL: "https://github.com/team/web.git"}, "")
	if err != nil {
		t.Fatal(err)
	}
	api, err := projects.Create(ProjectInput{Name: "api", RepoURL: "https://github.com/team/api.git"}, "")
	if err != nil {
		t.Fatal(err)
	}
	wf := models.WorkflowDef{ID: "wf-repos", Name: "repos", Graph: models.Graph{
		Nodes: []models.Node{{ID: "in", Type: "input"}, {ID: "out", Type: "output"}},
		Edges: []models.Edge{{ID: "e", Source: "in", Target: "out"}},
	}, Defaults: &models.WorkflowDefaults{InheritWorkspace: true, Repos: []models.RepoChoice{{ProjectID: web.ID, Branch: "main"}, {ProjectID: api.ID}}}}
	if err := workflows.Save(&wf); err != nil {
		t.Fatal(err)
	}
	if err := projects.Delete(web.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := workflows.Get(wf.ID)
	if got.Defaults == nil || !got.Defaults.InheritWorkspace || len(got.Defaults.Repos) != 1 || got.Defaults.Repos[0].ProjectID != api.ID {
		t.Fatalf("defaults after delete = %+v", got.Defaults)
	}
	if _, err := projects.Get(web.ID); projectErrCode(err) != "project_not_found" {
		t.Fatalf("project not deleted: %v", err)
	}
}

func TestWorkflowSaveValidatesBoundRepos(t *testing.T) {
	db := newTestDB(t)
	projects := NewProjectService(db)
	workflows := NewWorkflowService(db)
	web, err := projects.Create(ProjectInput{Name: "web", RepoURL: "https://github.com/team/web.git"}, "")
	if err != nil {
		t.Fatal(err)
	}
	graph := models.Graph{
		Nodes: []models.Node{{ID: "in", Type: "input"}, {ID: "out", Type: "output"}},
		Edges: []models.Edge{{ID: "e", Source: "in", Target: "out"}},
	}
	wf := models.WorkflowDef{ID: "wf-bind", Name: "bind", Graph: graph, Defaults: &models.WorkflowDefaults{Repos: []models.RepoChoice{
		{ProjectID: " " + web.ID + " ", Branch: " dev "}, {ProjectID: web.ID}, {ProjectID: " "},
	}}}
	if err := workflows.Save(&wf); err != nil {
		t.Fatal(err)
	}
	got, _ := workflows.Get(wf.ID)
	if len(got.Defaults.Repos) != 1 || got.Defaults.Repos[0] != (models.RepoChoice{ProjectID: web.ID, Branch: "dev"}) {
		t.Fatalf("repos = %+v", got.Defaults.Repos)
	}
	for _, bad := range []models.RepoChoice{{ProjectID: "missing"}, {ProjectID: web.ID, Branch: "a b"}} {
		next := got
		next.Defaults = &models.WorkflowDefaults{Repos: []models.RepoChoice{bad}}
		if err := workflows.Save(&next); !errors.Is(err, ErrWorkflowRepo) {
			t.Fatalf("%+v: err = %v", bad, err)
		}
	}
	if _, err := projects.Snapshot([]models.RepoChoice{{ProjectID: "missing"}}); err == nil {
		t.Fatal("snapshot of a missing repository succeeded")
	} else if se := (*SoloError)(nil); errors.As(err, &se) && se.Status != http.StatusNotFound {
		t.Fatalf("snapshot error = %v", err)
	}
}
