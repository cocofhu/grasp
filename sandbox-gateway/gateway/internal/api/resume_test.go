package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStartRejectsMalformedOrOversizedPreparation(t *testing.T) {
	r, _, _ := testRouter(t, nil)
	for _, body := range []string{"{", `{"bootstrapScript":"` + strings.Repeat("x", 256<<10) + `"}`} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/sandboxes/resident/start", bytes.NewBufferString(body)))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("bad preparation returned %d: %s", w.Code, w.Body.String())
		}
	}
}
