package handlers_test

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/gateshare"
	"github.com/cocofhu/grasp/internal/models"
)

// 1x1 PNG
const tinyPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func TestPublicGateImageByOpaqueIndex(t *testing.T) {
	h := newHarness(t)
	runID, nodeID := "run-img-sync", "research-img"
	seedInboxReview(t, h, runID, nodeID, true)

	now := time.Now().Format(time.RFC3339)
	h.db.Where("run_id = ? AND node_id = ?", runID, nodeID).Delete(&models.ReactConversation{})
	h.db.Create(&models.ReactConversation{
		RunID: runID, NodeID: nodeID, Iteration: 1, Done: false,
		Messages: []models.ReactMessage{
			{Role: "agent", Text: "请复审", At: now},
			{
				Role: "human", Text: "这里增加一个产物", At: now,
				Images: []models.PromptImage{{
					Data: tinyPNGBase64, MimeType: "image/png", Name: "shot.png",
				}},
			},
			{
				Role: "human", Text: "", At: now,
				Images: []models.PromptImage{{
					Data: tinyPNGBase64, MimeType: "image/png", Name: "only.png",
				}},
			},
		},
	})

	w := h.do(http.MethodPost, "/api/runs/"+runID+"/reviews/"+nodeID+"/share-link", map[string]any{"permissionPreset": "full", "ttlTier": "24h"})
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	created := parseJSON(t, w)
	url, _ := created["url"].(string)
	token := strings.TrimPrefix(url[strings.Index(url, "#t="):], "#t=")
	if !gateshare.ValidTokenShape(token) {
		t.Fatalf("token: %s", token)
	}

	req := httptest.NewRequest(http.MethodGet, "/public/gate-approvals/preview", nil)
	req.Header.Set("X-Gate-Share-Token", token)
	pw := httptest.NewRecorder()
	h.r.ServeHTTP(pw, req)
	if pw.Code != http.StatusOK {
		t.Fatalf("preview: %d %s", pw.Code, pw.Body.String())
	}
	preview := parseJSON(t, pw)
	rawPrev := pw.Body.String()
	if strings.Contains(rawPrev, "blob:") || strings.Contains(rawPrev, "/api/blobs") || strings.Contains(rawPrev, tinyPNGBase64) {
		t.Fatalf("preview leaked image bytes/refs: %s", rawPrev)
	}
	turns, _ := preview["turns"].([]any)
	if len(turns) < 3 {
		t.Fatalf("turns=%+v", preview["turns"])
	}
	human, _ := turns[1].(map[string]any)
	imgs, _ := human["images"].([]any)
	if len(imgs) != 1 {
		t.Fatalf("human images: %+v", human)
	}
	im0, _ := imgs[0].(map[string]any)
	if im0["index"] != float64(0) || im0["name"] != "shot.png" {
		t.Fatalf("first image: %+v", im0)
	}
	imgOnly, _ := turns[2].(map[string]any)
	onlyImgs, _ := imgOnly["images"].([]any)
	if len(onlyImgs) != 1 {
		t.Fatalf("image-only turn dropped: %+v", imgOnly)
	}
	im1, _ := onlyImgs[0].(map[string]any)
	if im1["index"] != float64(1) {
		t.Fatalf("second image index: %+v", im1)
	}

	// Valid token + index streams bytes.
	imgReq := httptest.NewRequest(http.MethodGet, "/public/gate-approvals/images/0?token="+token, nil)
	imgW := httptest.NewRecorder()
	h.r.ServeHTTP(imgW, imgReq)
	if imgW.Code != http.StatusOK {
		t.Fatalf("image: %d %s", imgW.Code, imgW.Body.String())
	}
	if !strings.HasPrefix(imgW.Header().Get("Content-Type"), "image/") {
		t.Fatalf("content-type: %s", imgW.Header().Get("Content-Type"))
	}
	want, _ := base64.StdEncoding.DecodeString(tinyPNGBase64)
	if imgW.Body.Len() != len(want) {
		t.Fatalf("bytes=%d want=%d", imgW.Body.Len(), len(want))
	}

	// Header auth also works.
	hdrReq := httptest.NewRequest(http.MethodGet, "/public/gate-approvals/images/1", nil)
	hdrReq.Header.Set("X-Gate-Share-Token", token)
	hdrW := httptest.NewRecorder()
	h.r.ServeHTTP(hdrW, hdrReq)
	if hdrW.Code != http.StatusOK {
		t.Fatalf("header image: %d %s", hdrW.Code, hdrW.Body.String())
	}

	// Invalid token rejected.
	bad := httptest.NewRequest(http.MethodGet, "/public/gate-approvals/images/0?token=not-a-valid-token", nil)
	badW := httptest.NewRecorder()
	h.r.ServeHTTP(badW, bad)
	if badW.Code == http.StatusOK && strings.HasPrefix(badW.Header().Get("Content-Type"), "image/") {
		t.Fatalf("invalid token must not stream image: %s", badW.Body.String())
	}

	// Out-of-range index.
	miss := httptest.NewRequest(http.MethodGet, "/public/gate-approvals/images/99", nil)
	miss.Header.Set("X-Gate-Share-Token", token)
	missW := httptest.NewRecorder()
	h.r.ServeHTTP(missW, miss)
	if missW.Code != http.StatusNotFound {
		t.Fatalf("missing index: %d %s", missW.Code, missW.Body.String())
	}
}
