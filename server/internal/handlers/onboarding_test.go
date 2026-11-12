package handlers_test

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/crypto"
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func setSecretsKey(t *testing.T) {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	t.Setenv(crypto.SecretsKeyEnv, base64.StdEncoding.EncodeToString(b))
}

func TestBootstrapOnboardingAPI(t *testing.T) {
	setSecretsKey(t)
	hn := newHarness(t)
	seedOnboardingModel(t, hn)

	w := hn.do("POST", "/api/workspace/bootstrap-onboarding", map[string]any{
		"modelConfigurationId": "",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("no key: %d %s", w.Code, w.Body.String())
	}

	w = hn.do("POST", "/api/workspace/bootstrap-onboarding", map[string]any{
		"modelConfigurationId": "model-onboarding",
		"agents":               []map[string]any{{"templateId": "implement"}},
	})
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "clarify") {
		t.Fatalf("team without clarify: %d %s", w.Code, w.Body.String())
	}

	w = hn.seedWorkspace(map[string]any{"name": "BootProj"})
	if w.Code != http.StatusOK {
		t.Fatalf("configure workspace: %d %s", w.Code, w.Body.String())
	}
	other := jsonField(w.Body.String(), "id")
	_ = other
	w = hn.do("POST", "/api/workspace/bootstrap-onboarding", map[string]any{
		"modelConfigurationId": "model-onboarding",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("non-default bootstrap: %d %s", w.Code, w.Body.String())
	}
	var otherRes services.OnboardingBootstrapResult
	if err := json.Unmarshal(w.Body.Bytes(), &otherRes); err != nil {
		t.Fatal(err)
	}
	if len(otherRes.AgentIDs) != len(services.OnboardingAgentNames) {
		t.Fatalf("non-default agents: %+v", otherRes)
	}
	for _, id := range otherRes.AgentIDs {
		if id == "" {
			t.Fatalf("non-default must derive names, got %q", id)
		}
	}

	w = hn.do("POST", "/api/workspace/bootstrap-onboarding", map[string]any{
		"modelConfigurationId": "model-onboarding",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("bootstrap: %d %s", w.Code, w.Body.String())
	}
	var res services.OnboardingBootstrapResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.AgentIDs) != len(services.OnboardingAgentNames) || res.WorkflowID == "" || !res.Published {
		t.Fatalf("bad result: %+v", res)
	}

	w = hn.do("POST", "/api/workspace/bootstrap-onboarding", map[string]any{
		"modelConfigurationId": "model-onboarding",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("second: %d %s", w.Code, w.Body.String())
	}
	var res2 services.OnboardingBootstrapResult
	_ = json.Unmarshal(w.Body.Bytes(), &res2)
	if res2.WorkflowID != res.WorkflowID {
		t.Fatalf("workflow id changed")
	}

	w = hn.do("GET", "/api/workflows", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list wf: %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), services.OnboardingWorkflowName) {
		t.Fatalf("workflow missing in list: %s", w.Body.String())
	}
}

func TestCreateWorkflowFromBaselineAPI(t *testing.T) {
	setSecretsKey(t)
	hn := newHarness(t)
	seedOnboardingModel(t, hn)

	if w := hn.do("POST", "/api/workspace/bootstrap-onboarding", map[string]any{
		"modelConfigurationId": "model-onboarding",
	}); w.Code != http.StatusOK {
		t.Fatalf("bootstrap: %d %s", w.Code, w.Body.String())
	}

	app, err := hn.h.Onboarding.Projects.Create(services.ProjectInput{Name: "app", RepoURL: "https://github.com/acme/app.git"}, "")
	if err != nil {
		t.Fatal(err)
	}
	api, err := hn.h.Onboarding.Projects.Create(services.ProjectInput{Name: "api", RepoURL: "https://github.com/acme/api.git"}, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, repos := range [][]map[string]any{{{"projectId": ""}}, {{"projectId": "missing"}}} {
		if w := hn.do("POST", "/api/workflows/from-baseline", map[string]any{
			"name":  "Bad repositories",
			"repos": repos,
		}); w.Code != http.StatusBadRequest {
			t.Fatalf("repos %v: %d %s", repos, w.Code, w.Body.String())
		}
	}

	if w := hn.do("POST", "/api/workflows/from-baseline", map[string]any{
		"name":  "　 ",
		"repos": []map[string]any{{"projectId": app.ID}},
	}); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), services.ErrEmptyWorkflowName.Error()) {
		t.Fatalf("empty name: %d %s", w.Code, w.Body.String())
	}

	w := hn.do("POST", "/api/workflows/from-baseline", map[string]any{
		"name": "  Requirements pipeline  ",
		"repos": []map[string]any{
			{"projectId": app.ID, "branch": "main"},
			{"projectId": api.ID},
			{"projectId": app.ID},
		},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create baseline: %d %s", w.Code, w.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created["name"] != "Requirements pipeline" || created["status"] != "published" || created["needsRepo"] != true {
		t.Fatalf("unexpected workflow: %#v", created)
	}
	if created["id"] == "" {
		t.Fatal("missing new workflow id")
	}
	if created["showOnHome"] != true {
		t.Fatalf("from-baseline showOnHome=%v want true (plan g1.1 / g1.2)", created["showOnHome"])
	}
	got := hn.do("GET", "/api/workflows/"+created["id"].(string), nil)
	if got.Code != http.StatusOK {
		t.Fatalf("GET created: %d %s", got.Code, got.Body.String())
	}
	var fetched struct {
		ShowOnHome bool                     `json:"showOnHome"`
		Status     string                   `json:"status"`
		Defaults   *models.WorkflowDefaults `json:"defaults"`
	}
	if err := json.Unmarshal(got.Body.Bytes(), &fetched); err != nil {
		t.Fatal(err)
	}
	if !fetched.ShowOnHome || fetched.Status != "published" {
		t.Fatalf("persisted from-baseline: %#v", fetched)
	}
	want := []models.RepoChoice{{ProjectID: app.ID, Branch: "main"}, {ProjectID: api.ID}}
	if fetched.Defaults == nil || !reflect.DeepEqual(fetched.Defaults.Repos, want) {
		t.Fatalf("bound repos = %+v, want %+v", fetched.Defaults, want)
	}
	ignored := hn.do("POST", "/api/workflows/from-baseline", map[string]any{
		"name":       "Client cannot hide",
		"showOnHome": false,
		"repos":      []map[string]any{{"projectId": api.ID}},
	})
	if ignored.Code != http.StatusCreated {
		t.Fatalf("create with showOnHome false in body: %d %s", ignored.Code, ignored.Body.String())
	}
	var ignoredBody map[string]any
	if err := json.Unmarshal(ignored.Body.Bytes(), &ignoredBody); err != nil {
		t.Fatal(err)
	}
	if ignoredBody["showOnHome"] != true {
		t.Fatalf("client showOnHome=false must not stick: %#v", ignoredBody)
	}

	w = hn.do("POST", "/api/workflows/from-baseline", map[string]any{
		"name":  "Requirements pipeline",
		"repos": []map[string]any{{"projectId": app.ID}},
	})
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), services.ErrWorkflowNameExists.Error()) {
		t.Fatalf("duplicate workflow name not rejected: %d %s", w.Code, w.Body.String())
	}

	w = hn.do("GET", "/api/workflows", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), services.OnboardingWorkflowName) {
		t.Fatalf("default workflow was overwritten: %d %s", w.Code, w.Body.String())
	}
}

func TestBootstrapPutsGitCredentialOnTheRepository(t *testing.T) {
	setSecretsKey(t)
	hn := newHarness(t)
	seedOnboardingModel(t, hn)
	credential, err := hn.h.Onboarding.Credentials.Create(services.CredentialInput{Purpose: "git", Type: "git", Provider: "github", Name: "GitHub", EnvKey: "GITHUB_TOKEN", Value: "test-github-key"})
	if err != nil {
		t.Fatal(err)
	}
	w := hn.do("POST", "/api/workspace/bootstrap-onboarding", map[string]any{
		"modelConfigurationId": "model-onboarding",
		"repoUrl":              "https://github.com/acme/app.git",
		"repoCredentialId":     credential.ID,
		"commitName":           "Ada",
		"commitEmail":          "ada@example.com",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("bootstrap: %d %s", w.Code, w.Body)
	}
	var result services.OnboardingBootstrapResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	projects, err := hn.h.Onboarding.Projects.List()
	if err != nil || len(projects) != 1 || projects[0].CredentialID != credential.ID || projects[0].CommitEmail != "ada@example.com" {
		t.Fatalf("repository = %+v, %v", projects, err)
	}
	for _, name := range append(result.AgentIDs, result.SoloAgentID) {
		agent, _ := hn.h.Agents.Get(name)
		if agent.Env["GITHUB_TOKEN"] != "" || len(agent.CredentialBindings) != 0 {
			t.Fatalf("Agent %s carries Git authorization", name)
		}
	}
	if w := hn.do("POST", "/api/workspace/bootstrap-onboarding", map[string]any{
		"modelConfigurationId": "model-onboarding",
		"repoUrl":              "https://github.com/acme/app.git",
		"repoCredentialId":     "missing",
	}); w.Code == http.StatusOK {
		t.Fatal("missing repository credential accepted")
	}
}

func seedOnboardingModel(t *testing.T, hn *harness) {
	t.Helper()
	credential, err := hn.h.Onboarding.Credentials.Create(services.CredentialInput{Purpose: "tool_auth", ToolID: "cursor", Name: "Cursor onboarding", Value: "test-key"})
	if err != nil {
		t.Fatal(err)
	}
	row := models.ModelConfiguration(modelrouting.Configuration{ID: "model-onboarding", Name: "Cursor model", Backend: "cursor", State: "active", Revision: 1, Binding: modelrouting.Binding{Kind: "platform", CredentialID: credential.ID, ModelID: "composer-2.5"}})
	if err := hn.db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	hn.h.Agents.SetModelBindingResolver(func(backend string, binding *modelrouting.Binding) error {
		_, err := services.NewModelConnectionService(hn.db).Resolve(backend, *binding)
		return err
	})
}
