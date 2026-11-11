package services

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
)

func boundSandboxFixture(t *testing.T, purpose string, git bool) (*SandboxService, *models.Sandbox, *ModelConnectionService, *ModelConfigurationService, CredentialView, modelrouting.Connection, modelrouting.Configuration) {
	t.Helper()
	connections, configurations, key, connection, configuration := newModelConfigurationFixture(t)
	acp, port := fakeACPServer(t)
	t.Cleanup(acp.Close)
	ds := &dockerState{acpPort: port}
	s := newSandboxService(t, configurations.db, ds)
	s.modelBinding = func(backend string, binding *modelrouting.Binding) (modelrouting.Resolved, error) {
		return connections.Resolve(backend, *binding)
	}
	agent := Agent{Name: "agentA", AcpBackend: "opencode", ModelBinding: &modelrouting.Binding{Kind: "configuration", ConfigurationID: configuration.ID}}
	if git {
		s.gitCredential = serviceGitValue
		agent.GitBindings = serviceGitSelection("gh", "gl")
	}
	if err := s.skills.Save(agent); err != nil {
		t.Fatal(err)
	}
	agent, _ = s.skills.Get(agent.Name)
	row := &models.Sandbox{Name: "bound-live-" + purpose, Profile: agent.Name, Purpose: purpose, ThreadID: "bound-thread", Status: "creating", Token: "test-password"}
	if err := s.db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
	if purpose == "test" {
		s.startContainer(row.ID, row.Name, agent.Name, "bound-run", row.Token, nil, agent, "")
	} else {
		s.startAgentContainer(row.ID, row.Name, agent.Name, row.ThreadID, "bound-run", row.Token, nil, agent, agentContainerExtra{Resident: purpose == SandboxPurposeSolo})
	}
	if err := s.db.First(row, row.ID).Error; err != nil || row.Status != "running" {
		t.Fatalf("start failed: %v status=%s error=%s", err, row.Status, row.Error)
	}
	t.Cleanup(func() { _ = s.Destroy(context.Background(), row.ID) })
	if len(row.ModelBindingFingerprint) != 64 || (git && row.GitBindingsFingerprint == "") {
		t.Fatal("applied binding fingerprints were not persisted")
	}
	encoded, _ := json.Marshal(row)
	if strings.Contains(string(encoded), row.ModelBindingFingerprint) || strings.Contains(string(encoded), "private-model-key") {
		t.Fatal("binding fingerprint or credential escaped through sandbox API")
	}
	return s, row, connections, configurations, key, connection, configuration
}

func TestLongLivedSandboxModelChangesBlockNextTurn(t *testing.T) {
	for _, purpose := range []string{"test", SandboxPurposeAgent, SandboxPurposeSolo} {
		for _, change := range []string{"cleared key", "revoked key", "rotated key", "disabled configuration", "deleted configuration", "disabled connection", "changed model", "removed binding"} {
			t.Run(purpose+"/"+change, func(t *testing.T) {
				s, row, connections, configurations, key, connection, configuration := boundSandboxFixture(t, purpose, false)
				credentials := NewCredentialService(s.db)
				var err error
				switch change {
				case "cleared key":
					err = credentials.Clear(key.ID)
				case "revoked key":
					err = credentials.Revoke(key.ID)
				case "rotated key":
					_, err = credentials.Update(key.ID, CredentialInput{Value: "new-model-key"})
				case "disabled configuration":
					configuration.State = "disabled"
					_, err = configurations.Update(configuration.ID, configuration)
				case "deleted configuration":
					err = configurations.Delete(configuration.ID)
				case "disabled connection":
					connection.State = "disabled"
					_, err = connections.Update(connection.ID, connection)
				case "changed model":
					connection.Models = append(connection.Models, modelrouting.Model{ID: "another-model", Source: "manual"})
					_, err = connections.Update(connection.ID, connection)
					if err == nil {
						configuration.Binding.ModelID = "another-model"
						_, err = configurations.Update(configuration.ID, configuration)
					}
				case "removed binding":
					err = s.skills.Save(Agent{Name: "agentA", AcpBackend: "opencode"})
				}
				if err != nil {
					t.Fatal(err)
				}
				if _, _, err := s.ChatWithTimeout(context.Background(), row.ID, "must not reach the old model", nil, 0, nil); err == nil {
					t.Fatal("a new turn used stale model authorization")
				}
				if purpose == SandboxPurposeAgent {
					if _, _, err := s.OpenAgentSandbox(context.Background(), AgentSandboxOpenOpts{Profile: row.Profile, ThreadID: row.ThreadID, Reuse: true}); err == nil {
						t.Fatal("thread reused stale model authorization")
					}
				}
			})
		}
	}
}

func TestLiveBindingFingerprintsSurviveReconnectAndRestart(t *testing.T) {
	s, row, _, _, _, _, _ := boundSandboxFixture(t, SandboxPurposeAgent, true)
	ctx := context.Background()
	for _, restart := range []bool{false, true} {
		if restart {
			s.teardownLive(row.ID)
		} else {
			s.live[row.ID].acp.Close()
		}
		if _, _, err := s.ChatWithTimeout(ctx, row.ID, "continue", nil, 0, nil); err != nil {
			t.Fatalf("unchanged authorization failed after reconnect (restart=%v): %v", restart, err)
		}
		live := s.live[row.ID]
		if live.modelBindingFingerprint != row.ModelBindingFingerprint || live.gitBindingsFingerprint != row.GitBindingsFingerprint {
			t.Fatal("reconnect discarded applied authorization fingerprints")
		}
		if got, reused, err := s.OpenAgentSandbox(ctx, AgentSandboxOpenOpts{Profile: row.Profile, ThreadID: row.ThreadID, Reuse: true}); err != nil || !reused || got.ID != row.ID {
			t.Fatalf("unchanged thread was not reused: %v", err)
		}
	}
	s.gitCredential = func(string) (runtime.GitCredentialValue, bool) { return runtime.GitCredentialValue{}, false }
	if _, _, err := s.ChatWithTimeout(ctx, row.ID, "revoked Git must block", nil, 0, nil); err == nil {
		t.Fatal("chat ignored revoked Git authorization")
	}
}

func TestTestSandboxDoesNotReusePreviouslyBoundCredentials(t *testing.T) {
	for _, binding := range []string{"model", "git"} {
		t.Run(binding, func(t *testing.T) {
			db := newTestDB(t)
			ds := &dockerState{}
			s := newSandboxService(t, db, ds)
			row := models.Sandbox{Name: "previously-bound", Profile: "agentA", Purpose: "test", Status: "running"}
			if binding == "model" {
				row.ModelBindingFingerprint = "old-model"
			} else {
				row.GitBindingsFingerprint = "old-git"
			}
			if err := db.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			ds.setStatus(row.Name, "running")
			if got, err := s.Open(context.Background(), "agentA", nil); err == nil && got.ID == row.ID {
				t.Fatal("cleared binding reused a sandbox with previous authorization")
			}
		})
	}
}

func TestSandboxCreationRequiresSelectedModelConfiguration(t *testing.T) {
	for _, purpose := range []string{"test", SandboxPurposeAgent, SandboxPurposeSolo} {
		t.Run(purpose, func(t *testing.T) {
			db := newTestDB(t)
			ds := &dockerState{}
			s := newSandboxService(t, db, ds)
			if err := s.skills.Save(Agent{Name: "agentA", AcpBackend: "cursor"}); err != nil {
				t.Fatal(err)
			}
			var err error
			switch purpose {
			case "test":
				_, err = s.Open(context.Background(), "agentA", nil)
			case SandboxPurposeAgent:
				_, _, err = s.OpenAgentSandbox(context.Background(), AgentSandboxOpenOpts{Profile: "agentA", ThreadID: "missing-model"})
			case SandboxPurposeSolo:
				_, err = s.OpenSoloSandbox(context.Background(), SoloSandboxOpts{Profile: "agentA", SessionID: "missing-model"})
			}
			if err == nil || !strings.Contains(err.Error(), "模型配置") {
				t.Fatalf("an unconfigured Agent started: %v", err)
			}
			var count int64
			db.Model(&models.Sandbox{}).Count(&count)
			if count != 0 || len(ds.fg.LastCreateEnv()) != 0 {
				t.Fatal("missing model configuration reached sandbox creation")
			}
		})
	}
}
