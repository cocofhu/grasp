package handlers_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestPrivatePreviewInjectorRejectsWrongAppBeforeBody(t *testing.T) {
	hn := newHarness(t)
	cases := []struct {
		name   string
		values []string
		want   int
	}{
		{"matching", []string{"5174"}, http.StatusOK},
		{"wrong-app", []string{"5173"}, http.StatusBadGateway},
		{"missing", nil, http.StatusBadGateway},
		{"non-numeric", []string{"not-a-port"}, http.StatusBadGateway},
		{"zero", []string{"0"}, http.StatusBadGateway},
		{"too-large", []string{"65536"}, http.StatusBadGateway},
		{"leading-zero", []string{"05174"}, http.StatusBadGateway},
		{"plus", []string{"+5174"}, http.StatusBadGateway},
		{"comma", []string{"5174,5173"}, http.StatusBadGateway},
		{"duplicate", []string{"5174", "5174"}, http.StatusBadGateway},
		{"multiple", []string{"5174", "5173"}, http.StatusBadGateway},
	}
	headers := make(map[string][]string, len(cases))
	for _, tc := range cases {
		headers["/app/"+tc.name] = tc.values
	}
	const upstreamBody = "UPSTREAM_APP_BODY"
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, value := range headers[r.URL.Path] {
			w.Header().Add("X-Grasp-Upstream", value)
		}
		w.Header().Set("X-Grasp-Internal", "reserved-value")
		_, _ = io.WriteString(w, upstreamBody)
	}))
	t.Cleanup(up.Close)
	public := seedPrivatePreview(t, hn, up.URL)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := &closeNotifyRecorder{ResponseRecorder: httptest.NewRecorder(), notify: make(chan bool)}
			hn.r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, public+"app/"+tc.name, nil))
			if w.Code != tc.want || w.Header().Get("X-Grasp-Internal") != "" {
				t.Fatalf("response status/header boundary: %d, want %d", w.Code, tc.want)
			}
			if tc.want == http.StatusOK {
				if w.Body.String() != upstreamBody || w.Header().Get("X-Grasp-Upstream") != "5174" {
					t.Fatal("matching app response did not survive")
				}
			} else if w.Body.String() != "preview upstream unavailable" || w.Header().Get("X-Grasp-Upstream") != "" {
				t.Fatal("wrong app body or identity leaked through rejected response")
			}
		})
	}
}

func TestPrivatePreviewInjectorAllowsOnlyFixedRuntimeRoutesWithoutMarker(t *testing.T) {
	hn := newHarness(t)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.URL.RequestURI())
	}))
	t.Cleanup(up.Close)
	public := seedPrivatePreview(t, hn, up.URL)
	for _, tc := range []struct {
		path string
		want int
	}{
		{"/__grasp/preview-pick.js", http.StatusOK},
		{"/__grasp/page-control.js?version=synthetic", http.StatusOK},
		{"/__grasp/live-overlay.js", http.StatusOK},
		{"/__grasp/embed-origin?ticket=synthetic&node=app", http.StatusOK},
		{"/__grasp/preview-pick.js/extra", http.StatusBadGateway},
		{"/__grasp/embed-origin/", http.StatusBadGateway},
		{"/__grasp/unknown", http.StatusBadGateway},
		{"/api?path=/__grasp/embed-origin", http.StatusBadGateway},
		{"/", http.StatusBadGateway},
	} {
		t.Run(tc.path, func(t *testing.T) {
			w := &closeNotifyRecorder{ResponseRecorder: httptest.NewRecorder(), notify: make(chan bool)}
			hn.r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, public+strings.TrimPrefix(tc.path, "/"), nil))
			if w.Code != tc.want {
				t.Fatalf("fixed route status %d, want %d", w.Code, tc.want)
			}
			if tc.want == http.StatusOK && w.Body.String() != tc.path {
				t.Fatal("runtime path/query did not survive")
			}
			if tc.want != http.StatusOK && w.Body.String() != "preview upstream unavailable" {
				t.Fatal("a lookalike runtime path bypassed the app identity check")
			}
		})
	}
}

func TestPrivatePreviewInjectorChecksWebSocketIdentityBeforeUpgrade(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values []string
		allow  bool
		path   string
	}{
		{"matching", []string{"5174"}, true, "/app-ws"},
		{"wrong-app", []string{"5173"}, false, "/app-ws"},
		{"missing", nil, false, "/app-ws"},
		{"duplicate", []string{"5174", "5174"}, false, "/app-ws"},
		{"runtime-path-must-not-bypass-upgrade-identity", nil, false, "/__grasp/preview-pick.js"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hn := newHarness(t)
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				upgrader := websocket.Upgrader{}
				conn, err := upgrader.Upgrade(w, r, http.Header{"X-Grasp-Upstream": tc.values})
				if err != nil {
					return
				}
				defer func() { _ = conn.Close() }()
				_ = conn.WriteMessage(websocket.TextMessage, []byte("UPSTREAM_WS_FRAME"))
			}))
			t.Cleanup(up.Close)
			public := seedPrivatePreview(t, hn, up.URL)
			server := httptest.NewServer(hn.r)
			t.Cleanup(server.Close)
			origin, _ := url.Parse(public)
			conn, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+tc.path, http.Header{
				"Host": {origin.Host}, "Origin": {strings.TrimRight(public, "/")},
			})
			if tc.allow {
				if err != nil {
					if resp != nil {
						_ = resp.Body.Close()
					}
					t.Fatal(err)
				}
				defer func() { _ = conn.Close() }()
				_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
				_, frame, err := conn.ReadMessage()
				if err != nil || string(frame) != "UPSTREAM_WS_FRAME" {
					t.Fatal("matching WebSocket was blocked")
				}
				return
			}
			if conn != nil {
				_ = conn.Close()
			}
			if err == nil || resp == nil || resp.StatusCode != http.StatusBadGateway {
				t.Fatal("mismatched WebSocket app was allowed to upgrade")
			}
			body, readErr := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if readErr != nil || string(body) != "preview upstream unavailable" || resp.Header.Get("X-Grasp-Upstream") != "" {
				t.Fatal("rejected WebSocket exposed upstream data")
			}
		})
	}
}
