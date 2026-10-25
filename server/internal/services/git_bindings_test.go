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

func TestAgentGitBindingsRoundtripZIP(t *testing.T) {
	s := NewAgentService(t.TempDir())
	selection := serviceGitSelection("gh", "gl")
	selection.Items[1].Scope = " https://GIT.Example.com/ "
	a := Agent{Name: "git-agent", AcpBackend: "cursor", ModelBinding: sandboxTestModelBinding(), GitBindings: selection}
	if err := s.Save(a); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(a.Name)
	if got.GitBindings == nil || got.GitBindings.Items[1].Scope != "https://git.example.com" {
		t.Fatalf("selection = %+v", got.GitBindings)
	}
	if a.GitBindings.Items[1].Scope != " https://GIT.Example.com/ " {
		t.Fatal("Save mutated caller")
	}
	raw, err := s.ExportZIP(a.Name)
	if err != nil {
		t.Fatal(err)
	}
	copy, err := NewAgentService(t.TempDir()).ImportZIP(raw, "git-copy", ImportZIPCreate)
	if err != nil || !reflect.DeepEqual(copy.GitBindings, got.GitBindings) {
		t.Fatalf("ZIP selection = %+v, %v", copy.GitBindings, err)
	}
	for _, name := range []string{"omitted", "empty"} {
		agent := Agent{Name: name, AcpBackend: "cursor", ModelBinding: sandboxTestModelBinding()}
		if name == "empty" {
			agent.GitBindings = serviceGitSelection()
		}
		if err := s.Save(agent); err != nil {
			t.Fatal(err)
		}
		none, _ := s.Get(name)
		if none.GitBindings == nil || len(none.GitBindings.Items) != 0 {
			t.Fatal("omitted and empty Git authorizations must both grant none")
		}
	}
	if err := s.Save(Agent{Name: "legacy-preferred", AcpBackend: "cursor", ModelBinding: sandboxTestModelBinding(), GitBindings: &models.GitBindings{Items: []models.GitCredentialBinding{{CredentialID: "gh"}}, PreferredCredentialID: "other"}}); err != nil {
		t.Fatal(err)
	}
	legacy, _ := s.Get("legacy-preferred")
	if legacy.GitBindings == nil || legacy.GitBindings.PreferredCredentialID != "" || len(legacy.GitBindings.Items) != 1 || legacy.GitBindings.Items[0].CredentialID != "gh" {
		t.Fatalf("legacy preferred credential was kept: %+v", legacy.GitBindings)
	}
	if err := s.Save(Agent{Name: "bad", AcpBackend: "cursor", GitBindings: &models.GitBindings{Items: []models.GitCredentialBinding{{CredentialID: " "}}}}); !errors.Is(err, ErrInvalidAgent) || s.Exists("bad") {
		t.Fatalf("invalid selection saved: %v", err)
	}
}

func TestSharedWorkspaceDoesNotGrantGitAuthorization(t *testing.T) {
	shared := SharedAgentConfig{Env: map[string]string{"FEATURE": "keep"}}
	own := serviceGitSelection("gh")
	if got := ExtendOverlay(shared, Agent{Name: "own", GitBindings: own}); got.GitBindings != own {
		t.Fatal("workspace defaults replaced the Agent selection")
	}
	if got := ExtendOverlay(shared, Agent{Name: "none"}); got.GitBindings != nil && len(got.GitBindings.Items) != 0 {
		t.Fatal("workspace defaults granted Git authorization")
	}
	if withoutGitAuthorizationBindings(nil) != nil || withoutGitAuthorizationEnv(nil) != nil {
		t.Fatal("nil input changed")
	}
}

func TestGitBindingsInteractiveReuseRejectsUnappliedOrInvalidSelection(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	s.gitCredential = serviceGitValue
	selection := serviceGitSelection("gh")
	if err := s.skills.Save(Agent{Name: "agentA", AcpBackend: "cursor", ModelBinding: sandboxTestModelBinding(), GitBindings: selection}); err != nil {
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
	agent, _ := s.skills.Get("agentA")
	modelFingerprint, gitFingerprint, err := s.currentBindingFingerprints(agent)
	if err != nil {
		t.Fatal(err)
	}
	s.live[row.ID] = &liveSandbox{modelBindingFingerprint: modelFingerprint, gitBindingsFingerprint: gitFingerprint}
	if got, reused, err := s.OpenAgentSandbox(context.Background(), opts); err != nil || !reused || got.ID != row.ID {
		t.Fatalf("identical applied binding not reused: %+v %v %v", got, reused, err)
	}
	s.gitCredential = func(string) (runtime.GitCredentialValue, bool) {
		return runtime.GitCredentialValue{EnvKey: "GITHUB_TOKEN", Secret: "rotated", Host: "https://github.com"}, true
	}
	if _, _, err := s.OpenAgentSandbox(context.Background(), opts); err == nil {
		t.Fatal("rotated secret reused without sync")
	}
	s.gitCredential = func(string) (runtime.GitCredentialValue, bool) { return runtime.GitCredentialValue{}, false }
	if _, _, err := s.OpenAgentSandbox(context.Background(), opts); err == nil {
		t.Fatal("revoked authorization fell back to existing container")
	}
	s.gitCredential = serviceGitValue
	if err := s.skills.Save(Agent{Name: "other", AcpBackend: "cursor", ModelBinding: sandboxTestModelBinding(), GitBindings: selection}); err != nil {
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
			s.gitCredential = serviceGitValue
			previous := config.GetConfig()
			config.StoreConfig(&config.Config{Server: config.ServerConfig{MCPAdvertise: "http://control.example"}})
			defer config.StoreConfig(previous)
			store := sandbox.NewBundleStore()
			s.mgr = sandbox.NewManager(ds.fg.Client(), sandbox.ManagerOptions{InjectStore: store, InjectAdvertise: "http://control.example", WorkspaceDir: "/root/workspace"})
			agent := Agent{Name: "agentA", AcpBackend: "cursor", ModelBinding: sandboxTestModelBinding(), GitBindings: serviceGitSelection("gh", "gl")}
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
			if err := json.Unmarshal([]byte(files["git/manifest.json"]), &catalog); err != nil || len(catalog) != 2 || catalog[0].ID != "gh" || catalog[0].Preferred || catalog[1].ID != "gl" || catalog[1].Preferred || strings.TrimSpace(files["git/0.secret"]) != "selected-gh-token" || strings.TrimSpace(files["git/1.secret"]) != "selected-gl-token" {
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
			s.gitCredential = serviceGitValue
			agent := Agent{Name: "agentA", AcpBackend: "cursor", ModelBinding: sandboxTestModelBinding(), GitBindings: serviceGitSelection("gone")}
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
