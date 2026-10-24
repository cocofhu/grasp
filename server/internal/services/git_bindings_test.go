package services

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path"
	"reflect"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func serviceGitSelection(ids ...string) *models.GitBindings {
	selection := &models.GitBindings{Items: []models.GitCredentialBinding{}}
	for _, id := range ids {
		selection.Items = append(selection.Items, models.GitCredentialBinding{CredentialID: id})
	}
	if len(ids) > 0 {
		selection.PreferredCredentialID = ids[0]
	}
	return selection
}

func serviceGitValue(id string) (string, string, bool) {
	switch id {
	case "gh":
		return "GITHUB_TOKEN", "selected-gh-token", true
	case "gl":
		return "GITLAB_TOKEN", "selected-gl-token", true
	default:
		return "", "", false
	}
}

func TestAgentGitBindingsRoundtripZIPAndClearedReferences(t *testing.T) {
	s := NewAgentService(t.TempDir())
	selection := serviceGitSelection("gh", "gl")
	selection.Items[1].Scope = " https://GIT.Example.com/ "
	legacy := "old"
	a := Agent{Name: "git-agent", AcpBackend: "cursor", GitBindings: selection, GitCredentialType: "github_https", GitCredentialID: "old", SshHostsCredentialID: "old-hosts", CredentialBindings: map[string]*string{"GITHUB_TOKEN": &legacy, "OTHER": &legacy}, Env: map[string]string{"GITHUB_URL": "https://old", "GIT_REPOS": "repo|https://git.example/repo"}}
	if err := s.Save(a); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(a.Name)
	if got.GitBindings == nil || got.GitBindings.Items[1].Scope != "https://git.example.com" || got.GitCredentialID != "" || got.GitCredentialType != "" || got.SshHostsCredentialID != "" || got.CredentialBindings["GITHUB_TOKEN"] != nil || got.CredentialBindings["OTHER"] == nil || got.Env["GITHUB_URL"] != "" || got.Env["GIT_REPOS"] == "" {
		t.Fatalf("selection did not replace legacy slots: %+v", got)
	}
	if a.GitCredentialID != "old" || a.CredentialBindings["GITHUB_TOKEN"] == nil || a.Env["GITHUB_URL"] == "" {
		t.Fatal("Save mutated caller")
	}
	raw, err := s.ExportZIP(a.Name)
	if err != nil {
		t.Fatal(err)
	}
	copy, err := NewAgentService(t.TempDir()).ImportZIP(raw, "git-copy", ImportZIPCreate)
	if err != nil || !reflect.DeepEqual(copy.GitBindings, got.GitBindings) || copy.RepoIDs != nil {
		t.Fatalf("ZIP selection = %+v, %v", copy.GitBindings, err)
	}
	s.ClearOpenCodeCredentialSelection("gh")
	after, _ := s.Get(a.Name)
	if !reflect.DeepEqual(after.GitBindings, got.GitBindings) {
		t.Fatal("clearing a secret silently replaced or removed its explicit reference")
	}
	if err := s.Save(Agent{Name: "legacy", AcpBackend: "cursor", GitCredentialID: "old", GitCredentialType: "github_https"}); err != nil {
		t.Fatal(err)
	}
	if legacy, _ := s.Get("legacy"); legacy.GitBindings != nil || legacy.GitCredentialID != "old" {
		t.Fatal("legacy selection migrated implicitly")
	}
	if err := s.Save(Agent{Name: "none", AcpBackend: "cursor", GitBindings: serviceGitSelection()}); err != nil {
		t.Fatal(err)
	}
	if none, _ := s.Get("none"); none.GitBindings == nil || len(none.GitBindings.Items) != 0 {
		t.Fatal("explicit empty lost")
	}
	if err := s.Save(Agent{Name: "bad", AcpBackend: "cursor", GitBindings: &models.GitBindings{Items: []models.GitCredentialBinding{{CredentialID: "gh"}}}}); !errors.Is(err, ErrInvalidAgent) || s.Exists("bad") {
		t.Fatalf("invalid selection saved: %v", err)
	}
}

func TestSharedGitBindingsPreserveExplicitLegacyAndRepositoryGrants(t *testing.T) {
	s := NewSharedAgentService(t.TempDir())
	selection := serviceGitSelection("gh")
	if err := s.Save(SharedAgentConfig{AcpBackend: "cursor", GitBindings: selection, RepoIDs: []string{"independent-repo"}, Env: map[string]string{"GITLAB_URL": "https://old", "FEATURE": "keep"}}); err != nil {
		t.Fatal(err)
	}
	shared := s.Get()
	if !reflect.DeepEqual(shared.GitBindings, selection) || shared.Env["GITLAB_URL"] != "" || shared.Env["FEATURE"] != "keep" {
		t.Fatalf("shared = %+v", shared)
	}
	s.ClearCredentialSelection("gh")
	if !reflect.DeepEqual(s.Get().GitBindings, selection) {
		t.Fatal("clear substituted an explicit shared reference")
	}
	if inherited := ExtendOverlay(shared, Agent{Name: "inherit"}); inherited.GitBindings != shared.GitBindings || inherited.RepoIDs == nil || len(*inherited.RepoIDs) != 1 {
		t.Fatal("workspace model/Git/repo overlay lost")
	}
	for _, legacy := range []Agent{{GitCredentialID: "old"}, {SshHostsCredentialID: "old-hosts"}, {CredentialBindings: map[string]*string{"GITHUB_TOKEN": nil}}} {
		if merged := ExtendOverlay(shared, legacy); merged.GitBindings != nil {
			t.Fatal("shared list replaced explicit legacy Agent authorization")
		}
	}
	none := serviceGitSelection()
	ownRepos := []string{}
	if own := ExtendOverlay(shared, Agent{GitBindings: none, RepoIDs: &ownRepos}); own.GitBindings != none || len(*own.RepoIDs) != 0 {
		t.Fatal("explicit none did not override inherited grants")
	}
	if err := s.Save(SharedAgentConfig{GitBindings: &models.GitBindings{PreferredCredentialID: "missing"}}); !errors.Is(err, ErrInvalidAgent) {
		t.Fatalf("invalid shared selection accepted: %v", err)
	}
	if withoutLegacyGitBindings(nil) != nil || withoutLegacyGitEnv(nil) != nil {
		t.Fatal("nil map compatibility lost")
	}
}

func TestGitBindingsInteractiveReuseRejectsUnappliedOrInvalidSelection(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	s.selectedCredential = serviceGitValue
	selection := serviceGitSelection("gh")
	if err := s.skills.Save(Agent{Name: "agentA", AcpBackend: "cursor", GitBindings: selection}); err != nil {
		t.Fatal(err)
	}
	row := models.Sandbox{Name: "git-existing", Profile: "agentA", Purpose: SandboxPurposeAgent, ThreadID: "git-thread", Status: "running"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	ds.setStatus(row.Name, "running")
	opts := AgentSandboxOpenOpts{Profile: "agentA", ThreadID: "git-thread", Reuse: true}
	if _, _, err := s.OpenAgentSandbox(context.Background(), opts); err == nil || !strings.Contains(err.Error(), "重新打开") {
		t.Fatalf("unknown/unapplied binding reused: %v", err)
	}
	credentials, err := runtime.ResolveGitBindings(selection, serviceGitValue)
	if err != nil {
		t.Fatal(err)
	}
	s.live[row.ID] = &liveSandbox{gitBindingsFingerprint: sandbox.SpecFingerprint(sandbox.Spec{GitBindingsConfigured: true, GitCredentials: credentials})}
	if got, reused, err := s.OpenAgentSandbox(context.Background(), opts); err != nil || !reused || got.ID != row.ID {
		t.Fatalf("identical applied binding not reused: %+v %v %v", got, reused, err)
	}
	s.selectedCredential = func(string) (string, string, bool) { return "GITHUB_TOKEN", "rotated", true }
	if _, _, err := s.OpenAgentSandbox(context.Background(), opts); err == nil {
		t.Fatal("rotated secret reused without sync")
	}
	s.selectedCredential = func(string) (string, string, bool) { return "", "", false }
	if _, _, err := s.OpenAgentSandbox(context.Background(), opts); err == nil {
		t.Fatal("revoked authorization fell back to existing container")
	}
	s.selectedCredential = serviceGitValue
	if err := s.skills.Save(Agent{Name: "other", AcpBackend: "cursor", GitBindings: selection}); err != nil {
		t.Fatal(err)
	}
	opts.Profile = "other"
	if _, _, err := s.OpenAgentSandbox(context.Background(), opts); err == nil || !strings.Contains(err.Error(), "其他 Agent") {
		t.Fatalf("another Agent reused thread sandbox: %v", err)
	}
}

func TestGitBindingsInteractiveAndTestStartsStageAllSelectedSecrets(t *testing.T) {
	for _, mode := range []string{"interactive", "test"} {
		t.Run(mode, func(t *testing.T) {
			db := newTestDB(t)
			acp, port := fakeACPServer(t)
			defer acp.Close()
			ds := &dockerState{acpPort: port}
			s := newSandboxService(t, db, ds)
			s.selectedCredential = serviceGitValue
			previous := config.GetConfig()
			config.StoreConfig(&config.Config{Server: config.ServerConfig{MCPAdvertise: "http://control.example"}})
			defer config.StoreConfig(previous)
			store := sandbox.NewBundleStore()
			s.mgr = sandbox.NewManager(ds.fg.Client(), sandbox.ManagerOptions{InjectStore: store, InjectAdvertise: "http://control.example", WorkspaceDir: "/root/workspace"})
			agent := Agent{Name: "agentA", AcpBackend: "cursor", GitBindings: serviceGitSelection("gh", "gl")}
			row := models.Sandbox{Name: "git-stage", Profile: agent.Name, Purpose: "test", Status: "creating", Token: "test-password"}
			if err := db.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			if mode == "interactive" {
				s.startAgentContainer(row.ID, row.Name, agent.Name, "git-thread", "git-run", row.Token, nil, agent, agentContainerExtra{})
			} else {
				s.startContainer(row.ID, row.Name, agent.Name, "git-run", row.Token, nil, agent, "")
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
				ID        string `json:"id"`
				Preferred bool   `json:"preferred"`
			}
			if err := json.Unmarshal([]byte(files["git/manifest.json"]), &catalog); err != nil || len(catalog) != 2 || catalog[0].ID != "gh" || !catalog[0].Preferred || catalog[1].ID != "gl" || strings.TrimSpace(files["git/0.secret"]) != "selected-gh-token" || strings.TrimSpace(files["git/1.secret"]) != "selected-gl-token" {
				t.Fatalf("not all selected authorizations staged: %+v %v", catalog, err)
			}
		})
	}
}

func TestGitBindingsSelectedFailureBlocksBothServiceStarts(t *testing.T) {
	for _, mode := range []string{"interactive", "test"} {
		t.Run(mode, func(t *testing.T) {
			db := newTestDB(t)
			ds := &dockerState{}
			s := newSandboxService(t, db, ds)
			s.selectedCredential = serviceGitValue
			agent := Agent{Name: "agentA", AcpBackend: "cursor", GitBindings: serviceGitSelection("gone")}
			row := models.Sandbox{Name: "git-invalid", Status: "creating"}
			if err := db.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			if mode == "interactive" {
				s.startAgentContainer(row.ID, row.Name, agent.Name, "thread", "run", "token", nil, agent, agentContainerExtra{})
			} else {
				s.startContainer(row.ID, row.Name, agent.Name, "run", "token", nil, agent, "")
			}
			var saved models.Sandbox
			if err := db.First(&saved, row.ID).Error; err != nil {
				t.Fatal(err)
			}
			if saved.Status != "error" || !strings.Contains(saved.Error, "gone") || len(ds.fg.LastCreateEnv()) != 0 {
				t.Fatalf("invalid selection did not fail before create: %+v", saved)
			}
		})
	}
}
