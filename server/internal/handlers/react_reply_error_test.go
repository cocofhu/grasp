package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cocofhu/grasp/internal/engine"
	"github.com/gin-gonic/gin"
)

func TestWriteReactReplyErrorSandboxBusy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	writeReactReplyError(c, &engine.SandboxBusyError{RunningOpID: "oid-1", Desynced: true})
	if w.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("json: %v", err)
	}
	if body["code"] != "sandbox_busy" || body["runningOpId"] != "oid-1" {
		t.Fatalf("body=%s", w.Body.String())
	}
	if body["desynced"] != true {
		t.Fatalf("desynced=%v", body["desynced"])
	}
}

func TestWriteReactReplyErrorOther(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	writeReactReplyError(c, errors.New("仍有待确认问题或收尾未完成，无法确认并流转"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
