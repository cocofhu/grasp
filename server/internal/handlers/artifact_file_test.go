package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func TestArtifactFileMCPDeliveryAndHTTPRange(t *testing.T) {
	h := newHarness(t)
	arts := services.NewArtifactService(h.db)
	delivery := services.NewArtifactDeliveryService(h.db, arts, nil)
	body := []byte("%PDF-1.7\noriginal report bytes\n")
	delivery.OpenSource = func(_ context.Context, runID, nodeID, path string) (io.ReadCloser, string, error) {
		if runID != "solo-files" || nodeID != "solo" || path != "output/report.pdf" {
			t.Errorf("unexpected source: %s %s %s", runID, nodeID, path)
		}
		return io.NopCloser(bytes.NewReader(body)), "session-files", nil
	}
	delivery.CurrentTurn = func(string, string) (services.ArtifactTurn, error) {
		return services.ArtifactTurn{SessionID: "session-files", TurnID: "user-1", SandboxID: 4}, nil
	}
	h.host.SetArtifactFiles(delivery)
	token := h.host.RegisterRun("solo-files")
	h.host.SetActiveNode("solo-files", "solo", nil)
	call := func(tool string, args map[string]any, credential string) (models.ArtifactAttachment, bool, int) {
		t.Helper()
		request, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": tool, "arguments": args}})
		r := httptest.NewRequest("POST", "/mcp/runs/solo-files", bytes.NewReader(request))
		r.Header.Set("Authorization", "Bearer "+credential)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.r.ServeHTTP(w, r)
		var response struct {
			Result struct {
				IsError bool `json:"isError"`
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
		}
		if w.Code != 200 {
			return models.ArtifactAttachment{}, true, w.Code
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response.Result.IsError {
			return models.ArtifactAttachment{}, true, w.Code
		}
		if len(response.Result.Content) != 1 {
			t.Fatalf("response=%s", w.Body.String())
		}
		var attachment models.ArtifactAttachment
		if err := json.Unmarshal([]byte(response.Result.Content[0].Text), &attachment); err != nil {
			t.Fatal(err)
		}
		return attachment, false, w.Code
	}
	args := map[string]any{"path": "output/report.pdf", "name": "报告.pdf", "caption": "报告"}
	file, isErr, status := call("send_file", args, token)
	if status != 200 || isErr || file.Kind != "pdf" || file.Revision != 1 {
		t.Fatalf("send=%+v error=%v status=%d", file, isErr, status)
	}
	if _, _, status = call("send_file", args, "wrong"); status != http.StatusUnauthorized {
		t.Fatalf("wrong credential status=%d", status)
	}
	duplicate, isErr, _ := call("send_file", args, token)
	if isErr || duplicate != file {
		t.Fatalf("duplicate=%+v error=%v", duplicate, isErr)
	}
	var raw string
	if err := h.db.Table("artifacts").Select("content").Where("id = ?", file.ArtifactID).Row().Scan(&raw); err != nil || !filestore.IsRef(raw) {
		t.Fatalf("stored=%q err=%v", raw, err)
	}
	request := httptest.NewRequest("GET", "/api/artifacts/"+file.ArtifactID+"/raw?revision=1", nil)
	request.Header = h.wsHeader()
	request.Header.Set("Range", "bytes=0-7")
	w := httptest.NewRecorder()
	h.r.ServeHTTP(w, request)
	if w.Code != http.StatusPartialContent || !bytes.Equal(w.Body.Bytes(), body[:8]) || !strings.HasPrefix(w.Header().Get("Content-Range"), "bytes 0-7/") {
		t.Fatalf("range status=%d body=%q headers=%v", w.Code, w.Body.Bytes(), w.Header())
	}
	if w.Header().Get("ETag") == "" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers=%v", w.Header())
	}
	w = h.do("HEAD", "/api/artifacts/"+file.ArtifactID+"/raw?revision=1", nil)
	if w.Code != 200 || w.Body.Len() != 0 || w.Header().Get("Content-Length") == "" {
		t.Fatalf("HEAD=%d %s", w.Code, w.Body.String())
	}
	first := append([]byte(nil), body...)
	body = []byte("%PDF-1.7\nupdated report bytes\n")
	updated, isErr, _ := call("send_file", args, token)
	if isErr || updated.ArtifactID != file.ArtifactID || updated.Revision != 2 {
		t.Fatalf("updated=%+v error=%v", updated, isErr)
	}
	w = h.do("GET", "/api/artifacts/"+file.ArtifactID+"/download?revision=1", nil)
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), first) {
		t.Fatalf("historical download=%d %q", w.Code, w.Body.Bytes())
	}
	if !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("disposition=%s", w.Header().Get("Content-Disposition"))
	}
	if _, isErr, _ = call("send_artifact", map[string]any{"artifact_id": "missing"}, token); !isErr {
		t.Fatal("sent unknown artifact")
	}
	var reply models.SoloMessage
	if err := h.db.First(&reply, "id = ?", "reply:user-1").Error; err != nil || len(reply.Files) != 2 {
		t.Fatalf("reply=%+v err=%v", reply, err)
	}
}
