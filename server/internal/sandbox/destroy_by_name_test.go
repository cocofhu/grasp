package sandbox

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// DestroyByName is idempotent: a sandbox the gateway no longer knows (404)
// counts as destroyed, any other failure is reported so callers can retry.
func TestDestroyByNameTreatsNotFoundAsDestroyed(t *testing.T) {
	status := http.StatusNotFound
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "x", status)
	}))
	defer srv.Close()
	m := NewManager(NewGatewayClient(srv.URL, ""), ManagerOptions{})

	if err := m.DestroyByName(context.Background(), "gone"); err != nil {
		t.Fatalf("404 must count as destroyed: %v", err)
	}
	status = http.StatusInternalServerError
	if err := m.DestroyByName(context.Background(), "stuck"); err == nil {
		t.Fatal("a gateway 500 must be reported")
	}
}
