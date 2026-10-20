package services

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"testing"
)

func saveBundleAgent(t *testing.T, s *AgentService, a Agent) {
	t.Helper()
	if err := s.Save(a); err != nil {
		t.Fatal(err)
	}
}

func zipEntryNames(t *testing.T, raw []byte) map[string]bool {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, f := range zr.File {
		out[f.Name] = true
	}
	return out
}

func TestWorkspaceAgentsBundleRoundTrip(t *testing.T) {
	src := NewAgentService(t.TempDir())
	saveBundleAgent(t, src, Agent{AcpBackend: AcpBackendCursor, Name: "alpha", Files: []AgentFile{{Path: "AGENTS.md", Content: "# alpha"}}})
	saveBundleAgent(t, src, Agent{AcpBackend: AcpBackendCursor, Name: "beta", Env: map[string]string{"FOO": "bar"}})

	raw, err := src.ExportWorkspaceAgentsZIP()
	if err != nil {
		t.Fatal(err)
	}
	names := zipEntryNames(t, raw)
	if !names[workspaceAgentsManifestName] || !names["agents/alpha/agent.json"] || !names["agents/beta/agent.json"] {
		t.Fatalf("bundle entries = %v", names)
	}

	dst := NewAgentService(t.TempDir())
	res, err := dst.ImportWorkspaceAgentsZIP(raw, ImportWorkspaceAgentsRename)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Created) != 2 || len(res.Renamed) != 0 {
		t.Fatalf("result = %+v", res)
	}
	for _, name := range []string{"alpha", "beta"} {
		got, ok := dst.Get(name)
		if !ok {
			t.Fatalf("%s: %+v", name, got)
		}
	}
	beta, _ := dst.Get("beta")
	if beta.Env["FOO"] != "bar" {
		t.Fatalf("env lost: %v", beta.Env)
	}
	alpha, _ := dst.Get("alpha")
	if !agentHasFilePath(alpha, "AGENTS.md") {
		t.Fatalf("workspace lost: %v", filePaths(alpha))
	}

	res, err = dst.ImportWorkspaceAgentsZIP(raw, ImportWorkspaceAgentsRename)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Renamed) != 2 || res.Renamed["alpha"] == "" || res.Renamed["alpha"] == "alpha" {
		t.Fatalf("rename result = %+v", res)
	}
	if got, ok := dst.Get(res.Renamed["alpha"]); !ok {
		t.Fatalf("renamed copy = %+v", got)
	}
}

func TestWorkspaceAgentsBundleOverwrite(t *testing.T) {
	src := NewAgentService(t.TempDir())
	saveBundleAgent(t, src, Agent{AcpBackend: AcpBackendCursor, Name: "alpha", Env: map[string]string{"V": "new"}})
	raw, err := src.ExportWorkspaceAgentsZIP()
	if err != nil {
		t.Fatal(err)
	}

	dst := NewAgentService(t.TempDir())
	saveBundleAgent(t, dst, Agent{AcpBackend: AcpBackendCursor, Name: "alpha", Env: map[string]string{"V": "old"}})
	res, err := dst.ImportWorkspaceAgentsZIP(raw, ImportWorkspaceAgentsOverwrite)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Overwritten) != 1 || res.Overwritten[0] != "alpha" {
		t.Fatalf("result = %+v", res)
	}
	if got, _ := dst.Get("alpha"); got.Env["V"] != "new" {
		t.Fatalf("not overwritten: %v", got.Env)
	}
}

func TestWorkspaceAgentsBundleRejectsInvalidInput(t *testing.T) {
	s := NewAgentService(t.TempDir())
	saveBundleAgent(t, s, Agent{AcpBackend: AcpBackendCursor, Name: "solo"})
	single, err := s.ExportZIP("solo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ImportWorkspaceAgentsZIP(single, ImportWorkspaceAgentsRename); !errors.Is(err, ErrWorkspaceBundleSingleAgent) {
		t.Fatalf("single agent zip: %v", err)
	}
	if _, err := s.ImportWorkspaceAgentsZIP([]byte("not a zip"), ImportWorkspaceAgentsRename); !errors.Is(err, ErrWorkspaceBundleInvalidZip) {
		t.Fatalf("invalid zip: %v", err)
	}
	bundle, err := s.ExportWorkspaceAgentsZIP()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ImportWorkspaceAgentsZIP(bundle, ImportWorkspaceAgentsRename); err != nil {
		t.Fatalf("self import: %v", err)
	}

	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	w, _ := zw.Create(workspaceAgentsManifestName)
	_, _ = io.WriteString(w, `{"kind":"something-else","schemaVersion":1,"agentNames":[]}`)
	_ = zw.Close()
	if _, err := s.ImportWorkspaceAgentsZIP(buf.Bytes(), ImportWorkspaceAgentsRename); !errors.Is(err, ErrWorkspaceBundleInvalidKind) {
		t.Fatalf("bad kind: %v", err)
	}
}
