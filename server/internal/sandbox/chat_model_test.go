package sandbox

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestChatModelGetAndSet(t *testing.T) {
	selected := "aux"
	var posted string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/login" && r.Method == http.MethodPost:
			http.SetCookie(w, &http.Cookie{Name: "agentchat_session", Value: "test-cookie", Path: "/"})
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		case r.URL.Path == "/api/models" && r.Method == http.MethodGet:
			if r.URL.Query().Get("chat") != "default" {
				t.Errorf("chat query = %q", r.URL.Query().Get("chat"))
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"selected": selected})
		case r.URL.Path == "/api/model" && r.Method == http.MethodPost:
			var body struct {
				Model string `json:"model"`
			}
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			posted = body.Model
			selected = body.Model
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"selected":"` + body.Model + `"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	host, port := splitTestHost(t, srv.URL)

	got, err := GetChatModel(context.Background(), host, port, "secret")
	if err != nil || got != "aux" {
		t.Fatalf("get: %q %v", got, err)
	}
	if err := SetChatModel(context.Background(), host, port, "secret", "aux-2"); err != nil {
		t.Fatal(err)
	}
	if posted != "aux-2" {
		t.Fatalf("posted %q", posted)
	}
	got, err = GetChatModel(context.Background(), host, port, "secret")
	if err != nil || got != "aux-2" {
		t.Fatalf("get after set: %q %v", got, err)
	}
}

func TestSetChatModelRejects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/login" {
			http.SetCookie(w, &http.Cookie{Name: "agentchat_session", Value: "test-cookie", Path: "/"})
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"该模型不在当前配置中"}`))
	}))
	defer srv.Close()
	host, port := splitTestHost(t, srv.URL)
	err := SetChatModel(context.Background(), host, port, "secret", "nope")
	if err == nil || !strings.Contains(err.Error(), "不在当前配置中") {
		t.Fatalf("err = %v", err)
	}
}

func splitTestHost(t *testing.T, raw string) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(strings.TrimPrefix(raw, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}
	return host, port
}
