//go:build e2ereal

// Package e2e holds end-to-end tests that drive a real Grasp stack (gateway +
// control plane + sandbox containers) through Docker Compose and a real model.
//
//	docker build -f server/Dockerfile -t grasp:e2e .
//	DEEPSEEK_API_KEY=... go test -tags e2ereal ./e2e -run TestRestartReattach -v -timeout 30m
package e2e

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	e2eProject   = "grasp-e2e"
	e2ePort      = "18080"
	e2eGateway   = "18899"
	sleepSeconds = 97
	// Demo account from start.sh: admin / demo1234.
	demoUsers    = `[{"username":"admin","password_hash":"$2a$10$EY.SdHq0p6drMz6U9JVrz.Kq0jNkg7TWmsVUFLtB1dL1yIelDkITi","is_admin":true}]`
	demoPassword = "demo1234"
)

type stack struct {
	t        *testing.T
	root     string
	api      string
	env      []string
	artifact string
	client   *http.Client
}

func TestRestartReattach(t *testing.T) {
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		t.Skip("DEEPSEEK_API_KEY not set")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	s := newStack(t)
	s.up()

	projectID := s.workspaceID()
	model := s.pickModel()
	t.Logf("using deepseek model %s", model)
	s.onboard(apiKey, model)
	wfID := s.createWorkflow(projectID)

	marker := "E2E-" + randHex(6)
	runA := s.startRun(wfID, marker)
	runB := s.startRun(wfID, marker+"-B")

	sbName := s.waitSandboxRunning(runA, 6*time.Minute)
	t.Logf("run A sandbox %s", sbName)
	// Anchored: the opencode command line carries the whole prompt, which
	// mentions the same command.
	sleepProc := fmt.Sprintf("^sleep %d$", sleepSeconds)
	s.waitProcess(sbName, sleepProc, 6*time.Minute)
	if st := s.runStatus(runB); st != "queued" {
		t.Fatalf("run B status = %q before restart, want queued", st)
	}

	// The control plane must exit promptly instead of draining the agent run.
	start := time.Now()
	s.compose("stop", "-t", "120", "grasp")
	if d := time.Since(start); d > 8*time.Second {
		t.Fatalf("control plane took %s to stop, want a fast exit", d)
	} else {
		t.Logf("control plane stopped in %s", d.Round(time.Millisecond))
	}
	if !s.processRunning(sbName, sleepProc) {
		t.Fatal("agent tool call died with the control plane")
	}
	s.compose("up", "-d", "--wait", "grasp")
	s.login()

	if st := s.runStatus(runA); st != "running" {
		t.Fatalf("run A status = %q after restart, want running", st)
	}
	if st := s.runStatus(runB); st != "queued" {
		t.Fatalf("run B status = %q after restart, want queued", st)
	}

	a := s.waitRunDone(runA, 10*time.Minute)
	if a.Status != "completed" {
		t.Fatalf("run A finished %q, want completed", a.Status)
	}
	if !a.hasTrace("work", "reattach", sbName) {
		t.Errorf("run A trace has no reattach of %s: %s", sbName, a.traceSummary())
	}
	if n := len(a.NodeExecutions["work"]); n != 1 {
		t.Errorf("work node ran %d times, want one continued visit", n)
	}
	if got := s.runSandboxes(runA); len(got) != 1 || got[0] != sbName {
		t.Errorf("run A sandboxes = %v, want only %s", got, sbName)
	}
	if !strings.Contains(string(a.NodeRuns["work"]), marker) {
		t.Errorf("work node output does not mention marker %s", marker)
	}

	// The run queued across the restart is admitted once A frees the slot.
	// Its own model round-trip proves nothing more about the restart.
	s.waitSandboxRunning(runB, 6*time.Minute)
	if st := s.runStatus(runB); st != "running" {
		t.Fatalf("run B status = %q after A finished, want running", st)
	}
	s.mustDo(http.MethodPost, "/api/runs/"+runB+"/cancel", nil, nil)
}

func newStack(t *testing.T) *stack {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	image := os.Getenv("GRASP_IMAGE")
	if image == "" {
		image = "grasp:e2e"
	}
	artifact := filepath.Join(root, "e2e-artifacts")
	_ = os.MkdirAll(artifact, 0o755)
	secret := make([]byte, 32)
	_, _ = rand.Read(secret)
	s := &stack{
		t: t, root: root, api: "http://127.0.0.1:" + e2ePort, artifact: artifact,
		env: append(os.Environ(),
			"COMPOSE_PROJECT_NAME="+e2eProject,
			"GRASP_IMAGE="+image,
			"GRASP_PORT="+e2ePort,
			"GRASP_GATEWAY_PORT="+e2eGateway,
			"GRASP_SECRETS_KEY="+base64.StdEncoding.EncodeToString(secret),
			"GRASP_DOCTOR_TOKEN="+randHex(32),
			"GRASP_AUTH_USERS="+demoUsers,
		),
	}
	jar, _ := cookiejar.New(nil)
	s.client = &http.Client{Jar: jar}
	t.Cleanup(s.down)
	return s
}

func (s *stack) compose(args ...string) string {
	s.t.Helper()
	full := append([]string{"compose", "-f", "compose.release.yaml", "-f", "server/e2e/compose.e2e.yaml"}, args...)
	cmd := exec.Command("docker", full...)
	cmd.Dir = s.root
	cmd.Env = s.env
	out, err := cmd.CombinedOutput()
	if err != nil {
		s.t.Fatalf("docker compose %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func (s *stack) up() {
	s.t.Helper()
	s.compose("up", "-d", "--wait")
	s.login()
}

func (s *stack) login() {
	s.t.Helper()
	s.mustDo(http.MethodPost, "/api/auth/login", map[string]any{"username": "admin", "password": demoPassword}, nil)
}

func (s *stack) down() {
	cmd := exec.Command("docker", "compose", "-f", "compose.release.yaml", "-f", "server/e2e/compose.e2e.yaml", "logs", "--no-color", "--timestamps")
	cmd.Dir, cmd.Env = s.root, s.env
	if out, err := cmd.CombinedOutput(); err == nil {
		_ = os.WriteFile(filepath.Join(s.artifact, "compose.log"), out, 0o644)
	}
	// The gateway knows every sandbox it created, including ones the control
	// plane already let go of.
	for _, id := range gatewaySandboxes() {
		c := "sbx-" + id
		if out, err := exec.Command("docker", "logs", "--timestamps", c).CombinedOutput(); err == nil {
			_ = os.WriteFile(filepath.Join(s.artifact, "sandbox-"+id+".log"), out, 0o644)
		}
		gatewayDelete(id)
		_ = exec.Command("docker", "rm", "-f", c).Run()
	}
	cmd = exec.Command("docker", "compose", "-f", "compose.release.yaml", "-f", "server/e2e/compose.e2e.yaml", "down", "--volumes", "--remove-orphans", "-t", "5")
	cmd.Dir, cmd.Env = s.root, s.env
	_ = cmd.Run()
	_ = os.RemoveAll(filepath.Join(s.root, ".localdata-e2e"))
}

const gatewayAPI = "http://127.0.0.1:" + e2eGateway + "/api/v1/sandboxes"

func gatewayRequest(method, url string) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer grasp-local-demo")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(raw))
	return resp, nil
}

func gatewaySandboxes() []string {
	resp, err := gatewayRequest(http.MethodGet, gatewayAPI)
	if err != nil {
		return nil
	}
	var out struct {
		Sandboxes []struct {
			ID string `json:"id"`
		} `json:"sandboxes"`
	}
	if json.NewDecoder(resp.Body).Decode(&out) != nil {
		return nil
	}
	ids := make([]string, 0, len(out.Sandboxes))
	for _, sb := range out.Sandboxes {
		ids = append(ids, sb.ID)
	}
	return ids
}

func gatewayDelete(id string) {
	_, _ = gatewayRequest(http.MethodDelete, gatewayAPI+"/"+id)
}

func (s *stack) do(method, path string, body, out any) (int, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		rd = bytes.NewReader(b)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, s.api+path, rd)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("%s %s: %d %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil {
		return resp.StatusCode, json.Unmarshal(raw, out)
	}
	return resp.StatusCode, nil
}

func (s *stack) getJSON(path string, out any) error {
	_, err := s.do(http.MethodGet, path, nil, out)
	return err
}

func (s *stack) mustDo(method, path string, body, out any) {
	s.t.Helper()
	if _, err := s.do(method, path, body, out); err != nil {
		s.t.Fatal(err)
	}
}

func (s *stack) workspaceID() string {
	var p struct {
		ID string `json:"id"`
	}
	s.mustDo(http.MethodGet, "/api/workspace", nil, &p)
	if p.ID == "" {
		s.t.Fatal("workspace id is empty")
	}
	return p.ID
}

func (s *stack) pickModel() string {
	var res struct {
		Models []struct {
			ID string `json:"id"`
		} `json:"models"`
	}
	s.mustDo(http.MethodGet, "/api/opencode/providers/deepseek/models", nil, &res)
	// Ids the sandbox's opencode build knows; newer catalog ids can make
	// `opencode run` exit at once.
	for _, want := range []string{"deepseek-flash", "deepseek-chat"} {
		for _, m := range res.Models {
			if m.ID == want {
				return m.ID
			}
		}
	}
	if len(res.Models) > 0 {
		return res.Models[0].ID
	}
	return "deepseek-chat"
}

func (s *stack) onboard(apiKey, model string) {
	s.t.Helper()
	body := map[string]any{
		"acpBackend": "opencode", "apiKey": apiKey,
		"openCodeProvider": "deepseek", "openCodeModel": model,
		"vncPreview": false, "browserMcp": false,
		"agents": []map[string]any{
			{"templateId": "clarify", "name": "e2e-pm"},
			{"templateId": "implement", "name": "e2e-dev"},
		},
	}
	// The body carries the API key: report only the status on failure.
	if code, err := s.do(http.MethodPost, "/api/workspace/bootstrap-onboarding", body, nil); err != nil {
		s.t.Fatalf("bootstrap onboarding failed with status %d", code)
	}
	// The implement template parks for human review after its run; this test
	// needs the node to finish on its own.
	var agent map[string]any
	s.mustDo(http.MethodGet, "/api/agents/e2e-dev", nil, &agent)
	caps, _ := agent["capabilities"].(map[string]any)
	if caps == nil {
		caps = map[string]any{}
	}
	caps["review"] = false
	agent["capabilities"] = caps
	s.mustDo(http.MethodPut, "/api/agents/e2e-dev", agent, nil)
}

func (s *stack) createWorkflow(projectID string) string {
	prompt := fmt.Sprintf("Use your shell tool to run exactly this one command and wait for it to finish:\n\n"+
		"sleep %d && echo {{vars.marker}} > /tmp/e2e-marker.txt && cat /tmp/e2e-marker.txt\n\n"+
		"Do not run anything else. When it has printed the marker, call node_complete with status "+
		"\"success\" and a summary that contains the printed marker text.", sleepSeconds)
	wf := map[string]any{
		"projectId": projectID, "name": "e2e-restart-" + randHex(3),
		"variables": []map[string]any{{"name": "marker", "type": "string", "ask": true, "required": true, "editable": true}},
		"nodes": []map[string]any{
			{"id": "input", "type": "input", "label": "input"},
			{"id": "work", "type": "agent", "label": "work",
				"caps":   map[string]any{"interaction": "auto", "reads": []string{"*"}},
				"config": map[string]any{"agent_profile": "e2e-dev", "prompt": prompt}},
			{"id": "output", "type": "output", "label": "output"},
		},
		"edges": []map[string]any{
			{"id": "e1", "source": "input", "target": "work"},
			{"id": "e2", "source": "work", "target": "output"},
		},
	}
	var out struct {
		ID string `json:"id"`
	}
	s.mustDo(http.MethodPost, "/api/workflows", wf, &out)
	s.mustDo(http.MethodPost, "/api/workflows/"+out.ID+"/publish", nil, nil)
	return out.ID
}

func (s *stack) startRun(wfID, marker string) string {
	var out struct {
		ID string `json:"id"`
	}
	s.mustDo(http.MethodPost, "/api/workflows/"+wfID+"/runs", map[string]any{"inputs": map[string]any{"marker": marker}}, &out)
	return out.ID
}

type runDetail struct {
	Status         string                       `json:"status"`
	NodeRuns       map[string]json.RawMessage   `json:"nodeRuns"`
	NodeExecutions map[string][]json.RawMessage `json:"nodeExecutions"`
	Trace          []struct {
		NodeID string `json:"nodeId"`
		Event  string `json:"event"`
		Detail string `json:"detail"`
	} `json:"trace"`
}

func (r runDetail) hasTrace(node, event, contains string) bool {
	for _, tr := range r.Trace {
		if tr.NodeID == node && tr.Event == event && strings.Contains(tr.Detail, contains) {
			return true
		}
	}
	return false
}

func (r runDetail) traceSummary() string {
	parts := make([]string, 0, len(r.Trace))
	for _, tr := range r.Trace {
		parts = append(parts, tr.NodeID+":"+tr.Event)
	}
	return strings.Join(parts, " ")
}

func (s *stack) run(id string) runDetail {
	s.t.Helper()
	var r runDetail
	s.mustDo(http.MethodGet, "/api/runs/"+id, nil, &r)
	return r
}

func (s *stack) runStatus(id string) string { return s.run(id).Status }

func (s *stack) waitRunDone(id string, timeout time.Duration) runDetail {
	s.t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		r := s.run(id)
		switch r.Status {
		case "completed", "failed", "cancelled":
			raw, _ := json.MarshalIndent(r, "", "  ")
			_ = os.WriteFile(filepath.Join(s.artifact, "run-"+id+".json"), raw, 0o644)
			return r
		}
		if time.Now().After(deadline) {
			s.t.Fatalf("run %s still %q after %s", id, r.Status, timeout)
		}
		time.Sleep(3 * time.Second)
	}
}

func (s *stack) runSandboxes(runID string) []string {
	s.t.Helper()
	var views []struct {
		Name    string `json:"name"`
		Purpose string `json:"purpose"`
		RunID   string `json:"runId"`
	}
	s.mustDo(http.MethodGet, "/api/sandboxes", nil, &views)
	var names []string
	for _, v := range views {
		if v.Purpose == "run" && v.RunID == runID {
			names = append(names, v.Name)
		}
	}
	return names
}

func (s *stack) waitSandboxRunning(runID string, timeout time.Duration) string {
	s.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var views []struct {
			Name    string `json:"name"`
			Purpose string `json:"purpose"`
			Status  string `json:"status"`
			RunID   string `json:"runId"`
		}
		if s.getJSON("/api/sandboxes", &views) == nil {
			for _, v := range views {
				if v.Purpose == "run" && v.RunID == runID && v.Status == "running" {
					return v.Name
				}
			}
		}
		if st := s.runStatus(runID); st == "failed" || st == "cancelled" {
			s.t.Fatalf("run %s %s before its sandbox started", runID, st)
		}
		time.Sleep(2 * time.Second)
	}
	s.t.Fatalf("run %s sandbox not running after %s", runID, timeout)
	return ""
}

// container resolves the Docker container of a gateway sandbox (the gateway
// prefixes its own name to the sandbox name).
func container(sandbox string) string {
	out, err := exec.Command("docker", "ps", "--filter", "name="+sandbox, "--format", "{{.Names}}").Output()
	if err != nil {
		return ""
	}
	for _, name := range strings.Fields(string(out)) {
		if strings.HasSuffix(name, sandbox) {
			return name
		}
	}
	return ""
}

func (s *stack) processRunning(sandbox, pattern string) bool {
	c := container(sandbox)
	return c != "" && exec.Command("docker", "exec", c, "pgrep", "-f", pattern).Run() == nil
}

func (s *stack) waitProcess(sandbox, pattern string, timeout time.Duration) {
	s.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if s.processRunning(sandbox, pattern) {
			return
		}
		time.Sleep(2 * time.Second)
	}
	s.t.Fatalf("%q never started in sandbox %s", pattern, sandbox)
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
