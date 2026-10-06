package sandbox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestFetchEventLogPageCursorBranches(t *testing.T) {
	ctx := context.Background()
	// invalid cursor → empty page
	page, err := FetchEventLogPageWithPassword(ctx, "127.0.0.1", 9, "bad", 10, testBridgePassword)
	if err != nil || page == nil || len(page.Events) != 0 {
		t.Fatalf("bad cursor: %+v err=%v", page, err)
	}
	page, err = FetchEventLogPageWithPassword(ctx, "127.0.0.1", 9, "0", 10, testBridgePassword)
	if err != nil || page == nil {
		t.Fatalf("zero cursor: %+v err=%v", page, err)
	}

	mux := http.NewServeMux()
	handleTestLogin(mux)
	mux.HandleFunc("/api/events", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"events":  []json.RawMessage{json.RawMessage(`{"type":"x"}`)},
			"hasMore": true,
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	host, portStr, _ := splitHostPortLocal(srv.URL)
	port, _ := strconv.Atoi(portStr)
	page, err = FetchEventLogPageWithPassword(ctx, host, port, "20", 5, testBridgePassword)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || !page.HasMore || page.NextCursor == "" {
		t.Fatalf("page=%+v", page)
	}
}

func splitHostPortLocal(raw string) (host, port string, err error) {
	u := raw
	if len(u) > 7 && u[:7] == "http://" {
		u = u[7:]
	}
	for i := len(u) - 1; i >= 0; i-- {
		if u[i] == ':' {
			return u[:i], u[i+1:], nil
		}
	}
	return u, "", nil
}

func TestFetchEventLogPageDefaultLimit(t *testing.T) {
	// dial will fail quickly — just covers limit<=0 branch before dial
	_, err := FetchEventLogPageWithPassword(context.Background(), "127.0.0.1", 1, "", 0, testBridgePassword)
	if err == nil {
		t.Fatal("expected dial error")
	}
}
