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

func TestProjectAgentsBundleRoundTripBindsTargetProject(t *testing.T) {
	src := NewAgentService(t.TempDir())
	saveBundleAgent(t, src, Agent{AcpBackend: AcpBackendCursor, Name: "alpha", ProjectID: "p-src", Files: []AgentFile{{Path: "AGENTS.md", Content: "# alpha"}}})
	saveBundleAgent(t, src, Agent{AcpBackend: AcpBackendCursor, Name: "beta", ProjectID: "p-src", Env: map[string]string{"FOO": "bar"}})
	saveBundleAgent(t, src, Agent{AcpBackend: AcpBackendCursor, Name: "outsider", ProjectID: "p-other"})

	raw, err := src.ExportProjectAgentsZIP("p-src")
	if err != nil {
		t.Fatal(err)
	}
	names := zipEntryNames(t, raw)
	if !names[projectAgentsManifestName] || !names["agents/alpha/agent.json"] || !names["agents/beta/agent.json"] {
		t.Fatalf("bundle entries = %v", names)
	}
	for n := range names {
		if bytes.Contains([]byte(n), []byte("outsider")) {
			t.Fatalf("foreign project agent exported: %s", n)
		}
	}

	dst := NewAgentService(t.TempDir())
	res, err := dst.ImportProjectAgentsZIP(raw, "p-dst", ImportProjectAgentsRename)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Created) != 2 || len(res.Renamed) != 0 {
		t.Fatalf("result = %+v", res)
	}
	for _, name := range []string{"alpha", "beta"} {
		got, ok := dst.Get(name)
		if !ok || got.ProjectID != "p-dst" {
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

	res, err = dst.ImportProjectAgentsZIP(raw, "p-dst", ImportProjectAgentsRename)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Renamed) != 2 || res.Renamed["alpha"] == "" || res.Renamed["alpha"] == "alpha" {
		t.Fatalf("rename result = %+v", res)
	}
	if got, ok := dst.Get(res.Renamed["alpha"]); !ok || got.ProjectID != "p-dst" {
		t.Fatalf("renamed copy = %+v", got)
	}
}

func TestProjectAgentsBundleOverwrite(t *testing.T) {
	src := NewAgentService(t.TempDir())
	saveBundleAgent(t, src, Agent{AcpBackend: AcpBackendCursor, Name: "alpha", ProjectID: "p1", Env: map[string]string{"V": "new"}})
	raw, err := src.ExportProjectAgentsZIP("p1")
	if err != nil {
		t.Fatal(err)
	}

	dst := NewAgentService(t.TempDir())
	saveBundleAgent(t, dst, Agent{AcpBackend: AcpBackendCursor, Name: "alpha", ProjectID: "p1", Env: map[string]string{"V": "old"}})
	res, err := dst.ImportProjectAgentsZIP(raw, "p1", ImportProjectAgentsOverwrite)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Overwritten) != 1 || res.Overwritten[0] != "alpha" {
		t.Fatalf("result = %+v", res)
	}
	if got, _ := dst.Get("alpha"); got.Env["V"] != "new" {
		t.Fatalf("not overwritten: %v", got.Env)
	}

	foreign := NewAgentService(t.TempDir())
	saveBundleAgent(t, foreign, Agent{AcpBackend: AcpBackendCursor, Name: "alpha", ProjectID: "elsewhere", Env: map[string]string{"V": "keep"}})
	if _, err := foreign.ImportProjectAgentsZIP(raw, "p1", ImportProjectAgentsOverwrite); !errors.Is(err, ErrProjectBundleForeignAgent) {
		t.Fatalf("want foreign agent error, got %v", err)
	}
	if got, _ := foreign.Get("alpha"); got.ProjectID != "elsewhere" || got.Env["V"] != "keep" {
		t.Fatalf("foreign agent modified: %+v", got)
	}
}

func TestProjectAgentsBundleRejectsInvalidInput(t *testing.T) {
	s := NewAgentService(t.TempDir())
	saveBundleAgent(t, s, Agent{AcpBackend: AcpBackendCursor, Name: "solo", ProjectID: "p1"})
	single, err := s.ExportZIP("solo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ImportProjectAgentsZIP(single, "p1", ImportProjectAgentsRename); !errors.Is(err, ErrProjectBundleSingleAgent) {
		t.Fatalf("single agent zip: %v", err)
	}
	if _, err := s.ImportProjectAgentsZIP([]byte("not a zip"), "p1", ImportProjectAgentsRename); !errors.Is(err, ErrProjectBundleInvalidZip) {
		t.Fatalf("invalid zip: %v", err)
	}
	bundle, err := s.ExportProjectAgentsZIP("p1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ImportProjectAgentsZIP(bundle, "", ImportProjectAgentsRename); !errors.Is(err, ErrAgentProjectRequired) {
		t.Fatalf("empty project: %v", err)
	}

	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	w, _ := zw.Create(projectAgentsManifestName)
	_, _ = io.WriteString(w, `{"kind":"something-else","schemaVersion":1,"agentNames":[]}`)
	_ = zw.Close()
	if _, err := s.ImportProjectAgentsZIP(buf.Bytes(), "p1", ImportProjectAgentsRename); !errors.Is(err, ErrProjectBundleInvalidKind) {
		t.Fatalf("bad kind: %v", err)
	}
}
