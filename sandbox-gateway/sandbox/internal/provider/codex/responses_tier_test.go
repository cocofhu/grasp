package codex

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"backend/internal/envutil"
)

func TestResponsesTierProxy(t *testing.T) {
	for _, tc := range []struct {
		name, path, tier, model, incomingTier, wantTier string
	}{
		{"standard responses", "/responses", "default", "primary", "priority", "default"},
		{"standard compaction", "/responses/compact", "default", "aux", "", "default"},
		{"fast supported", "/responses", "priority", "primary", "", "priority"},
		{"fast auxiliary model", "/responses", "priority", "aux", "auto", "auto"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]string
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Errorf("decode forwarded request: %v", err)
				}
				if r.URL.Path != "/v1"+tc.path || r.Header.Get("Authorization") != "Bearer fixture" ||
					body["model"] != tc.model || body["input"] != "keep this prompt" || body["service_tier"] != tc.wantTier {
					t.Errorf("unexpected request on %s: %v", r.URL.Path, body)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: fixture\n\n")
			}))
			defer upstream.Close()
			base, stop, err := startResponsesTierProxy(upstream.URL+"/v1/", "primary", tc.tier)
			if err != nil {
				t.Fatal(err)
			}
			defer stop()
			body := map[string]string{"model": tc.model, "input": "keep this prompt"}
			if tc.incomingTier != "" {
				body["service_tier"] = tc.incomingTier
			}
			payload, _ := json.Marshal(body)
			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, base+tc.path, strings.NewReader(string(payload)))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer fixture")
			req.Header.Set("Content-Type", "application/json")
			resp, err := upstream.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			reply, err := io.ReadAll(resp.Body)
			if err != nil || resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" || string(reply) != "data: fixture\n\n" {
				t.Fatalf("unexpected upstream response: %s %q, %v", resp.Status, reply, err)
			}
		})
	}
}

func TestResponsesProxyKeepsLoopbackOutOfTheChildProxy(t *testing.T) {
	env := withLoopbackNoProxy([]string{"NO_PROXY=.example.test", "no_proxy=local.test", "HTTP_PROXY=http://proxy.test"})
	if envutil.Get(env, "NO_PROXY") != "127.0.0.1,.example.test" ||
		envutil.Get(env, "no_proxy") != "127.0.0.1,local.test" ||
		envutil.Get(env, "HTTP_PROXY") != "http://proxy.test" {
		t.Fatalf("unexpected child proxy environment: %v", env)
	}
	if got := envutil.Get(withLoopbackNoProxy(nil), "NO_PROXY"); got != "127.0.0.1" {
		t.Fatalf("expected a loopback exclusion, got %q", got)
	}
}

func TestResponsesTierUsesAllProxy(t *testing.T) {
	for _, key := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "NO_PROXY", "no_proxy", "ALL_PROXY", "all_proxy"} {
		t.Setenv(key, "")
	}
	type proxiedRequest struct {
		method, host, tier string
	}
	seen := make(chan proxiedRequest, 1)
	egress := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if r.Method == http.MethodPost {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
		}
		seen <- proxiedRequest{r.Method, r.Host, body["service_tier"]}
		if r.Method == http.MethodConnect {
			w.WriteHeader(http.StatusBadGateway)
		}
	}))
	defer egress.Close()
	t.Setenv("ALL_PROXY", egress.URL)
	client := egress.Client()
	for _, tc := range []struct {
		scheme, tier, method, host string
		status                     int
	}{
		{"http", "default", http.MethodPost, "api.openai.invalid", http.StatusOK},
		{"http", "priority", http.MethodPost, "api.openai.invalid", http.StatusOK},
		{"https", "default", http.MethodConnect, "api.openai.invalid:443", http.StatusBadGateway},
	} {
		base, stop, err := startResponsesTierProxy(tc.scheme+"://api.openai.invalid/v1", "primary", tc.tier)
		if err != nil {
			t.Fatal(err)
		}
		defer stop()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, base+"/responses", strings.NewReader(`{"model":"primary"}`))
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != tc.status {
			t.Fatalf("got HTTP %d, want %d", resp.StatusCode, tc.status)
		}
		select {
		case request := <-seen:
			if request.method != tc.method || request.host != tc.host ||
				(tc.method == http.MethodPost && request.tier != tc.tier) {
				t.Fatalf("proxy received %+v", request)
			}
		default:
			t.Fatal("request bypassed ALL_PROXY")
		}
	}
}
