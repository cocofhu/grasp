package services

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"path"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func serviceRepoLookup(projects ...models.Project) func(string) (*models.Project, bool) {
	byID := map[string]*models.Project{}
	for i := range projects {
		byID[projects[i].ID] = &projects[i]
	}
	return func(id string) (*models.Project, bool) { p, ok := byID[id]; return p, ok }
}

func serviceGitValue(id string) (runtime.GitCredentialValue, bool) {
	switch id {
	case "gh":
		return runtime.GitCredentialValue{EnvKey: "GITHUB_TOKEN", Secret: "selected-gh-token", Host: "https://github.com"}, true
	case "gl":
		return runtime.GitCredentialValue{EnvKey: "GITLAB_TOKEN", Secret: "selected-gl-token", Host: "https://gitlab.com"}, true
	default:
		return runtime.GitCredentialValue{}, false
	}
}

func TestAgentSaveDropsGitAuthorization(t *testing.T) {
	s := NewAgentService(t.TempDir())
	a := Agent{Name: "git-agent", AcpBackend: "cursor", ModelBinding: sandboxTestModelBinding(),
		Env: map[string]string{"GITLAB_URL": "https://gitlab.example", "FEATURE": "keep"}}
	if err := s.Save(a); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(a.Name)
	if got.Env["GITLAB_URL"] != "" || got.Env["FEATURE"] != "keep" {
		t.Fatalf("env = %+v", got.Env)
	}
	if withoutGitAuthorizationBindings(nil) != nil || withoutGitAuthorizationEnv(nil) != nil {
		t.Fatal("nil input changed")
	}
}

func TestRepoCredentialsInteractiveAndTestStartsStageEachRepository(t *testing.T) {
	for _, mode := range []string{"interactive", "test"} {
		t.Run(mode, func(t *testing.T) {
			db := newTestDB(t)
			acp, port := fakeACPServer(t)
			defer acp.Close()
			ds := &dockerState{acpPort: port}
			s := newSandboxService(t, db, ds)
			s.gitCredential = serviceGitValue
			s.project = serviceRepoLookup(models.Project{ID: "p-web", CredentialID: "gh", CommitName: "Bot", CommitEmail: "bot@example.com"}, models.Project{ID: "p-api", CredentialID: "gh"})
			repos := []models.SessionRepo{
				{ProjectID: "p-web", RepoURL: "https://github.com/team/web.git", RepoDirName: "web"},
				{ProjectID: "p-api", RepoURL: "https://github.com/team/api.git", RepoDirName: "api"},
			}
			previous := config.GetConfig()
			config.StoreConfig(&config.Config{Server: config.ServerConfig{MCPAdvertise: "http://control.example"}})
			defer config.StoreConfig(previous)
			store := sandbox.NewBundleStore()
			s.mgr = sandbox.NewManager(ds.fg.Client(), sandbox.ManagerOptions{InjectStore: store, InjectAdvertise: "http://control.example", WorkspaceDir: "/root/workspace"})
			agent := Agent{Name: "agentA", AcpBackend: "cursor", ModelBinding: sandboxTestModelBinding(), Env: map[string]string{"GITLAB_TOKEN": "unselected"}}
			row := models.Sandbox{Name: "git-stage", Profile: agent.Name, Purpose: "test", Status: "creating", Token: "test-password"}
			if err := db.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			if mode == "interactive" {
				s.startAgentContainer(row.ID, row.Name, agent.Name, "git-thread", "git-run", row.Token, nil, agent, agentContainerExtra{Repos: repos})
			} else {
				s.startContainer(row.ID, row.Name, agent.Name, "git-run", row.Token, repos, agent, "")
			}
			var saved models.Sandbox
			if err := db.First(&saved, row.ID).Error; err != nil {
				t.Fatal(err)
			}
			if saved.Status != "running" {
				t.Fatalf("sandbox failed: %s %s", saved.Status, saved.Error)
			}
			defer func() { _ = s.Destroy(context.Background(), row.ID) }()
			env := ds.fg.LastCreateEnv()
			if env["GITHUB_TOKEN"] != "" || env["GITLAB_TOKEN"] != "" {
				t.Fatal("catalog secrets leaked into env")
			}
			if env["GIT_REPOS"] != "web|https://github.com/team/web.git,api|https://github.com/team/api.git" {
				t.Fatalf("GIT_REPOS = %q", env["GIT_REPOS"])
			}
			if authors := sandbox.DecodeRepoAuthors(env[sandbox.EnvRepoAuthors]); len(authors) != 1 || authors["web"] != [2]string{"Bot", "bot@example.com"} {
				t.Fatalf("authors = %+v", authors)
			}
			part := strings.Split(strings.Split(env["SANDBOX_INJECT"], ",")[0], "|")[0]
			token := strings.TrimPrefix(env["SANDBOX_INJECT_HEADERS"], "Authorization: Bearer ")
			blob, ok := store.Get(path.Base(part), token)
			if !ok {
				t.Fatal("missing authorization stage")
			}
			gz, err := gzip.NewReader(bytes.NewReader(blob))
			if err != nil {
				t.Fatal(err)
			}
			defer gz.Close()
			tr := tar.NewReader(gz)
			files := map[string]string{}
			for {
				hdr, err := tr.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(tr)
				if err != nil {
					t.Fatal(err)
				}
				files[hdr.Name] = string(body)
			}
			var catalog []struct {
				ID    string `json:"id"`
				Scope string `json:"scope"`
			}
			if err := json.Unmarshal([]byte(files["git/manifest.json"]), &catalog); err != nil || len(catalog) != 2 || catalog[0].ID != "repo:web" || catalog[0].Scope != "https://github.com/team/web" || catalog[1].ID != "repo:api" || strings.TrimSpace(files["git/0.secret"]) != "selected-gh-token" || strings.TrimSpace(files["git/1.secret"]) != "selected-gh-token" {
				t.Fatalf("repository credentials not staged: %+v %v", catalog, err)
			}
		})
	}
}

func TestRepoCredentialFailureBlocksBothServiceStarts(t *testing.T) {
	for _, mode := range []string{"interactive", "test"} {
		t.Run(mode, func(t *testing.T) {
			db := newTestDB(t)
			ds := &dockerState{}
			s := newSandboxService(t, db, ds)
			s.gitCredential = serviceGitValue
			s.project = serviceRepoLookup(models.Project{ID: "p", CredentialID: "gone"})
			repos := []models.SessionRepo{{ProjectID: "p", RepoURL: "https://github.com/team/web.git", RepoDirName: "web"}}
			agent := Agent{Name: "agentA", AcpBackend: "cursor", ModelBinding: sandboxTestModelBinding()}
			row := models.Sandbox{Name: "git-invalid", Status: "creating"}
			if err := db.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			if mode == "interactive" {
				s.startAgentContainer(row.ID, row.Name, agent.Name, "thread", "run", "token", nil, agent, agentContainerExtra{Repos: repos})
			} else {
				s.startContainer(row.ID, row.Name, agent.Name, "run", "token", repos, agent, "")
			}
			var saved models.Sandbox
			if err := db.First(&saved, row.ID).Error; err != nil {
				t.Fatal(err)
			}
			if saved.Status != "error" || !strings.Contains(saved.Error, "web") || len(ds.fg.LastCreateEnv()) != 0 {
				t.Fatalf("invalid repository credential did not fail before create: %+v", saved)
			}
		})
	}
}
