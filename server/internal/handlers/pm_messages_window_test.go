package handlers_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func TestListPmMessagesTailWindowAndBefore(t *testing.T) {
	hn, pid, _ := setupPmEnabledHarness(t)
	w := hn.do(http.MethodPost, "/api/projects/"+pid+"/pm/threads", map[string]any{"title": "window"})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var thr map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &thr)
	tid := thr["id"].(string)

	base := time.Now().Add(-40 * time.Minute)
	for i := 0; i < 25; i++ {
		msg := models.ChatMessage{
			ID:        fmt.Sprintf("wmsg-%02d", i),
			ThreadID:  tid,
			Role:      "user",
			Content:   fmt.Sprintf("c%d", i),
			Status:    "ok",
			CreatedAt: base.Add(time.Duration(i) * time.Second),
		}
		if err := hn.db.Create(&msg).Error; err != nil {
			t.Fatal(err)
		}
	}

	// No params → default tail window of 20.
	w = hn.do(http.MethodGet, "/api/projects/"+pid+"/pm/threads/"+tid+"/messages", nil)
	if w.Code != 200 {
		t.Fatalf("default: %d %s", w.Code, w.Body.String())
	}
	var def struct {
		Items   []map[string]any `json:"items"`
		HasMore bool             `json:"hasMore"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &def)
	if len(def.Items) != 20 || !def.HasMore {
		t.Fatalf("default items=%d hasMore=%v", len(def.Items), def.HasMore)
	}

	// limit=20 → tail window + hasMore.
	w = hn.do(http.MethodGet, "/api/projects/"+pid+"/pm/threads/"+tid+"/messages?limit=20", nil)
	if w.Code != 200 {
		t.Fatalf("tail: %d %s", w.Code, w.Body.String())
	}
	var page struct {
		Items   []map[string]any `json:"items"`
		HasMore bool             `json:"hasMore"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &page)
	if !page.HasMore || len(page.Items) != 20 {
		t.Fatalf("tail: len=%d hasMore=%v", len(page.Items), page.HasMore)
	}
	firstID, _ := page.Items[0]["id"].(string)
	lastID, _ := page.Items[19]["id"].(string)
	if firstID != "wmsg-05" || lastID != "wmsg-24" {
		t.Fatalf("tail range %s..%s", firstID, lastID)
	}

	// before=first → older 5, hasMore=false.
	w = hn.do(http.MethodGet, "/api/projects/"+pid+"/pm/threads/"+tid+"/messages?limit=20&before="+firstID, nil)
	if w.Code != 200 {
		t.Fatalf("before: %d %s", w.Code, w.Body.String())
	}
	_ = json.Unmarshal(w.Body.Bytes(), &page)
	if page.HasMore || len(page.Items) != 5 {
		t.Fatalf("before: len=%d hasMore=%v", len(page.Items), page.HasMore)
	}
	if page.Items[0]["id"] != "wmsg-00" || page.Items[4]["id"] != "wmsg-04" {
		t.Fatalf("before range %v..%v", page.Items[0]["id"], page.Items[4]["id"])
	}

	// Invalid limit.
	w = hn.do(http.MethodGet, "/api/projects/"+pid+"/pm/threads/"+tid+"/messages?limit=abc", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad limit want 400 got %d", w.Code)
	}

	// Unknown before.
	w = hn.do(http.MethodGet, "/api/projects/"+pid+"/pm/threads/"+tid+"/messages?limit=20&before=missing", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing before want 404 got %d", w.Code)
	}
}
