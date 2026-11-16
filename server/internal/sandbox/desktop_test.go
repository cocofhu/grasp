package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func desktopBridge(t *testing.T, handler http.HandlerFunc) (string, int) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/login" {
			http.SetCookie(w, &http.Cookie{Name: "agentchat_session", Value: "c", Path: "/"})
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return splitTestHost(t, srv.URL)
}

func TestDesktopStatusAndControl(t *testing.T) {
	var got map[string]string
	host, port := desktopBridge(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/desktop/status" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"mode":"desktop","shellReady":true,"agentActive":true,"holder":"agent","apps":["blender"]}`))
		case r.URL.Path == "/api/desktop/control" && r.Method == http.MethodPut:
			_ = json.NewDecoder(r.Body).Decode(&got)
			_, _ = w.Write([]byte(`{"mode":"desktop","holder":"user","expiresAt":"x","apps":[]}`))
		default:
			http.NotFound(w, r)
		}
	})
	st, err := GetDesktopStatus(context.Background(), host, port, "pw")
	if err != nil || st.Mode != "desktop" || !st.AgentActive || len(st.Apps) != 1 {
		t.Fatalf("status %+v %v", st, err)
	}
	st, err = SetDesktopControl(context.Background(), host, port, "pw", "user", "alice:tab")
	if err != nil || st.Holder != "user" {
		t.Fatalf("control %+v %v", st, err)
	}
	if got["holder"] != "user" || got["client"] != "alice:tab" {
		t.Fatalf("body %+v", got)
	}
}

func TestDesktopStatusOldBundleIsBrowser(t *testing.T) {
	host, port := desktopBridge(t, http.NotFound)
	st, err := GetDesktopStatus(context.Background(), host, port, "pw")
	if err != nil || st.Mode != "browser" || st.Holder != "agent" || st.Apps == nil {
		t.Fatalf("status %+v %v", st, err)
	}
	_, err = SetDesktopControl(context.Background(), host, port, "pw", "user", "c")
	var he *ChatHTTPError
	if !errors.As(err, &he) || he.StatusCode != http.StatusNotFound {
		t.Fatalf("control err %v", err)
	}
}

func TestDesktopErrors(t *testing.T) {
	host, port := desktopBridge(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/desktop/control" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{not json`))
	})
	if _, err := GetDesktopStatus(context.Background(), host, port, "pw"); err == nil {
		t.Fatal("bad json accepted")
	}
	_, err := SetDesktopControl(context.Background(), host, port, "pw", "bad", "c")
	var he *ChatHTTPError
	if !errors.As(err, &he) || he.StatusCode != http.StatusBadRequest {
		t.Fatalf("control err %v", err)
	}
	if _, err := GetDesktopStatus(context.Background(), "127.0.0.1", 1, "pw"); err == nil {
		t.Fatal("unreachable accepted")
	}
	if _, err := SetDesktopControl(context.Background(), "127.0.0.1", 1, "pw", "user", "c"); err == nil {
		t.Fatal("unreachable accepted")
	}
}
