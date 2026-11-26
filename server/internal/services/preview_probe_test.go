package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbePreviewHTTPJudgesFirstHopOnly(t *testing.T) {
	login := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("identity provider"))
	}))
	t.Cleanup(login.Close)
	injector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(injectUpstreamHeader, "5173")
		http.Redirect(w, r, login.URL+"/authorize", http.StatusFound)
	}))
	t.Cleanup(injector.Close)

	if !probePreviewHTTP(context.Background(), injector.URL, 5173) {
		t.Fatal("app redirecting / to an external login must stay healthy")
	}
	if probePreviewHTTP(context.Background(), injector.URL, 3000) {
		t.Fatal("first-hop marker for another port must not count as healthy")
	}
}
