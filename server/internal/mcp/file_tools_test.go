package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

type testFileTools struct {
	err       error
	called    string
	run, node string
}

func (f *testFileTools) result(tool, run, node string) (models.ArtifactAttachment, error) {
	f.called, f.run, f.node = tool, run, node
	return models.ArtifactAttachment{ArtifactID: "file-1", Revision: 3, Name: "report.pdf", Kind: "pdf", SizeBytes: 20}, f.err
}
func (f *testFileTools) Import(_ context.Context, run, node, _, _ string) (models.ArtifactAttachment, error) {
	return f.result("import", run, node)
}
func (f *testFileTools) SendFile(_ context.Context, run, node, _, _, _ string) (models.ArtifactAttachment, error) {
	return f.result("send", run, node)
}
func (f *testFileTools) SendExisting(_ context.Context, run, node, _ string, _ int, _ string) (models.ArtifactAttachment, error) {
	return f.result("existing", run, node)
}

type fileMetadataStore struct {
	memStore
	row models.Artifact
}

func (s *fileMetadataStore) GetByID(id string) (models.Artifact, bool) { return s.row, id == s.row.ID }
func (s *fileMetadataStore) GetRecord(run, name string) (models.Artifact, bool) {
	return s.row, run == s.row.RunID && name == s.row.Name
}

func TestFileToolsScopeDispatchAndAvailability(t *testing.T) {
	store := &fileMetadataStore{row: models.Artifact{ID: "file-1", RunID: "solo-files", Name: "report.pdf", Kind: "pdf", Revision: 3}}
	h := NewHost(store)
	token := h.RegisterRun("solo-files")
	h.SetActiveNode("solo-files", "solo", nil)
	f := &testFileTools{}
	if _, failed := h.runFileTool("solo-files", "bad", "send_file", nil); !failed {
		t.Fatal("unauthorized call accepted")
	}
	if _, failed := h.runFileTool("solo-files", token, "send_file", nil); !failed {
		t.Fatal("missing service accepted")
	}
	h.SetArtifactFiles(f)
	for name, want := range map[string]string{"send_file": "send", "upload_file_artifact": "import", "send_artifact": "existing"} {
		out, failed := h.runTool("solo-files", token, name, map[string]any{"path": "output/report.pdf", "artifact_id": "file-1", "revision": 3})
		if failed || f.called != want || f.run != "solo-files" || f.node != "solo" || !strings.Contains(out, `"revision":3`) {
			t.Fatalf("%s out=%s f=%+v failed=%v", name, out, f, failed)
		}
	}
	for _, args := range []map[string]any{{"artifact_id": "file-1", "revision": -1}, {"artifact_id": "file-1", "revision": 0}, {"artifact_id": "other"}} {
		if _, failed := h.runFileTool("solo-files", token, "send_artifact", args); !failed {
			t.Fatal("invalid artifact accepted")
		}
	}
	store.row.RunID = "another-run"
	if _, failed := h.runFileTool("solo-files", token, "send_artifact", map[string]any{"artifact_id": "file-1"}); !failed {
		t.Fatal("cross-run artifact accepted")
	}
	f.err = errors.New("source unavailable")
	if _, failed := h.runFileTool("solo-files", token, "send_file", nil); !failed {
		t.Fatal("source error ignored")
	}
	if _, failed := h.runFileTool("solo-files", token, "unknown", nil); !failed {
		t.Fatal("unknown tool accepted")
	}
	plain := NewHost(&memStore{})
	plainToken := plain.RegisterRun("plain")
	plain.SetArtifactFiles(f)
	if _, failed := plain.runFileTool("plain", plainToken, "send_artifact", nil); !failed {
		t.Fatal("missing metadata accepted")
	}
	if len(fileTools()) != 3 {
		t.Fatal("missing file tool schemas")
	}
	store.row.RunID = "solo-files"
	body, err := h.ReadArtifact("solo-files", token, "report.pdf")
	if err != nil || !strings.Contains(body, `"artifactId":"file-1"`) {
		t.Fatalf("binary read=%s err=%v", body, err)
	}
}

func TestFileBodiesAreOmittedFromMCPTraceAndAudit(t *testing.T) {
	h := NewHost(&memStore{})
	token := h.RegisterRun("r")
	h.SetActiveNode("r", "writer", nil)
	call(t, h, "r", token, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"write_artifact","arguments":{"name":"report.md","content":"PRIVATE-FILE-BODY"}}}`)
	call(t, h, "r", token, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"read_artifact","arguments":{"name":"report.md"}}}`)
	for _, trace := range h.TakeMcpCalls("r", "writer") {
		if strings.Contains(trace.Args+trace.Result, "PRIVATE-FILE-BODY") {
			t.Fatalf("trace duplicated body: %+v", trace)
		}
	}
	args := redactToolArgs("upload_image_artifact", map[string]any{"content": "BASE64-IMAGE", "name": "shot.png"})
	if args["content"] == "BASE64-IMAGE" || args["name"] != "shot.png" {
		t.Fatalf("image trace=%v", args)
	}
}
