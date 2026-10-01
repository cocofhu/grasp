package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"backend/internal/service"

	"github.com/gin-gonic/gin"
)

func TestRuntimeBusyLoopbackOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/runtime/busy", RuntimeBusy(service.NewChatManager()))

	for _, tc := range []struct {
		remote string
		code   int
	}{
		{"127.0.0.1:5000", http.StatusOK},
		{"[::1]:5000", http.StatusOK},
		{"10.0.0.5:5000", http.StatusForbidden},
		{"garbage", http.StatusForbidden},
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/runtime/busy", nil)
		req.RemoteAddr = tc.remote
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != tc.code {
			t.Fatalf("%s: code=%d want %d", tc.remote, w.Code, tc.code)
		}
		if tc.code != http.StatusOK {
			continue
		}
		var body struct {
			Busy   bool   `json:"busy"`
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Busy {
			t.Fatalf("%s: body=%s err=%v", tc.remote, w.Body.String(), err)
		}
	}
}
