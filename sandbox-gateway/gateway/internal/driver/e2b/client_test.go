package e2b

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

const testProviderSecret = "synthetic-secret-never-in-errors"

func newControlTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := NewClient(ClientOptions{APIURL: server.URL, SandboxURL: server.URL,
		APIKey: testProviderSecret, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	return client, server
}

func writeControlSandbox(w http.ResponseWriter, status int, id string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"sandboxID": id, "templateID": "template-123", "envdVersion": "0.6.4",
		"envdAccessToken": testProviderSecret + "-envd", "trafficAccessToken": testProviderSecret + "-traffic",
		"domain": "region.e2b.app", "state": "running", "cpuCount": 2, "memoryMB": 3072, "diskSizeMB": 24576,
		"startedAt": "2026-11-05T01:02:03Z", "endAt": "2026-11-05T02:02:03Z",
		"metadata": map[string]string{"grasp.gateway.owner": "owner-a", "grasp.gateway.id": "gateway-a"},
	})
}

func assertSafeControlError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), testProviderSecret) {
		t.Fatal("provider credentials leaked in error")
	}
}

func TestClientOptionsValidateOriginsAndKeepCredentialsOutOfURLs(t *testing.T) {
	for _, opts := range []ClientOptions{
		{}, {APIKey: "bad\nkey"}, {APIKey: "key", APIURL: "https://user:password@api.example"},
		{APIKey: "key", APIURL: "https://api.example/path"}, {APIKey: "key", APIURL: "ftp://api.example"},
		{APIKey: "key", APIURL: "https://api.example?secret=value"}, {APIKey: "key", SandboxURL: "https://proxy.example/#fragment"},
		{APIKey: "key", SandboxURL: "https://proxy.example:65536"}, {APIKey: "key", Domain: "bad..example"},
		{APIKey: "key", Domain: "-bad.example"}, {APIKey: "key", Domain: "a." + strings.Repeat("b", 64)},
	} {
		if _, err := NewClient(opts); err == nil {
			t.Fatalf("accepted invalid client options: APIURL=%q SandboxURL=%q Domain=%q", opts.APIURL, opts.SandboxURL, opts.Domain)
		}
	}
	client, err := NewClient(ClientOptions{APIKey: testProviderSecret})
	if err != nil || client.apiOrigin.String() != "https://api.e2b.app" {
		t.Fatalf("unexpected default API origin: %v", err)
	}
	if client.http.Timeout != 60*time.Second {
		t.Fatal("default HTTP transport has no bounded request timeout")
	}
}

func TestCreatePrivateSandboxOfficialWireContract(t *testing.T) {
	client, _ := newControlTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/sandboxes" || r.Header.Get("X-API-Key") != testProviderSecret {
			t.Error("wrong create route or API authentication")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["templateID"] != "team/full:default" || body["timeout"] != float64(600) || body["secure"] != true ||
			body["autoPause"] != false || body["autoResume"].(map[string]any)["enabled"] != false ||
			body["network"].(map[string]any)["allowPublicTraffic"] != false || body["allow_internet_access"] != true {
			t.Error("create did not explicitly select private traffic, secured envd, and kill-on-timeout")
		}
		if body["envVars"].(map[string]any)["ROOT_PASSWORD"] != testProviderSecret ||
			body["metadata"].(map[string]any)["grasp.gateway.id"] != "gateway-a" {
			t.Error("environment or ownership metadata was not preserved")
		}
		writeControlSandbox(w, http.StatusCreated, "sandbox-a")
	})
	sb, err := client.Create(t.Context(), CreateOptions{TemplateID: "team/full:default", TimeoutSeconds: 600,
		EnvVars: map[string]string{"ROOT_PASSWORD": testProviderSecret}, Metadata: map[string]string{"grasp.gateway.id": "gateway-a"}})
	if err != nil {
		t.Fatal(err)
	}
	if sb.ID != "sandbox-a" || sb.EnvdAccessToken == "" || sb.TrafficAccessToken == "" || sb.MemoryMB != 3072 ||
		sb.CPUCount != 2 || sb.DiskSizeMB != 24576 || sb.StartedAt.IsZero() || sb.EndAt.IsZero() {
		t.Fatal("provider response identity, profile, credentials or dates missing")
	}
	encoded, _ := json.Marshal(sb)
	if strings.Contains(string(encoded), testProviderSecret) || strings.Contains(fmt.Sprintf("%+v %#v", sb, sb), testProviderSecret) {
		t.Fatal("sandbox formatting exposes private credentials")
	}
}

func TestCreateMissingPrivateCredentialsReturnsPartialIdentity(t *testing.T) {
	for _, missing := range []string{"envdAccessToken", "trafficAccessToken", "envdVersion"} {
		t.Run(missing, func(t *testing.T) {
			client, _ := newControlTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				body := map[string]string{"sandboxID": "sandbox-a", "templateID": "base", "envdVersion": "0.6.4",
					"envdAccessToken": "envd-token", "trafficAccessToken": "traffic-token"}
				delete(body, missing)
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(body)
			})
			sb, err := client.Create(t.Context(), CreateOptions{TemplateID: "base", TimeoutSeconds: 300})
			assertSafeControlError(t, err)
			if sb == nil || sb.ID != "sandbox-a" {
				t.Fatal("partial creation identity is unavailable for verified cleanup")
			}
		})
	}
}

func TestGetAndExplicitLifecycleRoutes(t *testing.T) {
	var operations []string
	client, _ := newControlTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		operations = append(operations, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/sandboxes/sandbox-a":
			if r.Method == http.MethodGet {
				writeControlSandbox(w, http.StatusOK, "sandbox-a")
				return
			}
		case "/sandboxes/sandbox-a/timeout":
			var b map[string]int
			_ = json.NewDecoder(r.Body).Decode(&b)
			if b["timeout"] != 120 {
				t.Error("wrong timeout")
			}
		case "/sandboxes/sandbox-a/pause":
			var b map[string]bool
			_ = json.NewDecoder(r.Body).Decode(&b)
			if !b["memory"] || !b["requireHealthyGuest"] {
				t.Error("pause allows losing RAM or snapshotting an unhealthy guest")
			}
		case "/sandboxes/sandbox-a/connect":
			var b map[string]any
			_ = json.NewDecoder(r.Body).Decode(&b)
			if b["timeout"] != float64(120) || b["memory"] != true {
				t.Error("connect does not request full memory recovery and lease")
			}
			writeControlSandbox(w, http.StatusCreated, "sandbox-a")
			return
		default:
			t.Error("unexpected lifecycle route")
		}
		w.WriteHeader(http.StatusNoContent)
	})
	if _, err := client.Get(t.Context(), "sandbox-a"); err != nil {
		t.Fatal(err)
	}
	if len(operations) != 1 {
		t.Fatal("Get implicitly resumed a sandbox")
	}
	for _, err := range []error{client.SetTimeout(t.Context(), "sandbox-a", 120), client.Pause(t.Context(), "sandbox-a")} {
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := client.Connect(t.Context(), "sandbox-a", 120); err != nil {
		t.Fatal(err)
	}
	if err := client.Delete(t.Context(), "sandbox-a"); err != nil {
		t.Fatal(err)
	}
	want := []string{"GET /sandboxes/sandbox-a", "POST /sandboxes/sandbox-a/timeout", "POST /sandboxes/sandbox-a/pause", "POST /sandboxes/sandbox-a/connect", "DELETE /sandboxes/sandbox-a"}
	if !reflect.DeepEqual(operations, want) {
		t.Fatalf("wrong lifecycle operations: %v", operations)
	}
}

func TestListPaginatesAndRejectsForeignMetadataEvenIfProviderIgnoresQuery(t *testing.T) {
	owner := map[string]string{"grasp.gateway.owner": "owner&with=characters", "grasp.gateway.driver": "e2b"}
	pages := 0
	client, _ := newControlTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		pages++
		if r.Method != http.MethodGet || r.URL.Path != "/v2/sandboxes" || r.URL.Query().Get("state") != "running,paused" || r.URL.Query().Get("limit") != "100" {
			t.Error("invalid list path or state filter")
		}
		metadata, err := url.ParseQuery(r.URL.Query().Get("metadata"))
		if err != nil || metadata.Get("grasp.gateway.owner") != owner["grasp.gateway.owner"] || metadata.Get("grasp.gateway.driver") != "e2b" {
			t.Error("ownership metadata was not nested URL encoded")
		}
		if pages == 1 {
			w.Header().Set("X-Next-Token", "test+test/test=")
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"sandboxID": "owned-a", "metadata": owner},
				{"sandboxID": "foreign", "metadata": map[string]string{"grasp.gateway.owner": "other", "grasp.gateway.driver": "e2b"}},
				{"sandboxID": "missing-owner"},
			})
		} else {
			if r.URL.Query().Get("nextToken") != "test+test/test=" {
				t.Error("pagination cursor changed")
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{{"sandboxID": "owned-a", "metadata": owner}, {"sandboxID": "owned-b", "metadata": owner, "state": "paused"}})
		}
	})
	result, err := client.List(t.Context(), owner)
	if err != nil || pages != 2 || len(result) != 2 || result[0].ID != "owned-a" || result[1].ID != "owned-b" {
		t.Fatalf("wrong owned list or pagination: %v, pages=%d, count=%d", err, pages, len(result))
	}
}

func TestControlErrorsAreTypedSafeAndDeleteIsIdempotent(t *testing.T) {
	for _, status := range []int{401, 403, 404, 409, 429, 500, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			client, _ := newControlTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(testProviderSecret))
			})
			_, err := client.Get(t.Context(), "sandbox-a")
			assertSafeControlError(t, err)
			var typed *APIError
			if !errors.As(err, &typed) || typed.HTTPStatus != status || errors.Is(err, ErrNotFound) != (status == 404) {
				t.Fatal("status or absence is not available as a safe typed error")
			}
			if status == 404 && client.Delete(t.Context(), "sandbox-a") != nil {
				t.Fatal("deleting an absent sandbox is not idempotent")
			}
		})
	}
}

func TestControlRejectsRedirectsWithoutSendingCredentials(t *testing.T) {
	forwarded := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded = true }))
	defer target.Close()
	client, _ := newControlTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	})
	_, err := client.Get(t.Context(), "sandbox-a")
	assertSafeControlError(t, err)
	if forwarded {
		t.Fatal("API credentials followed a redirect")
	}
}

func TestControlRejectsTruncatedOversizedOrMismatchedJSON(t *testing.T) {
	for name, body := range map[string]string{
		"truncated": `{"sandboxID":"` + testProviderSecret,
		"oversized": strings.Repeat(" ", maxControlResponseBytes+1),
		"mismatch":  `{"sandboxID":"another-sandbox"}`,
	} {
		t.Run(name, func(t *testing.T) {
			client, _ := newControlTestClient(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) })
			_, err := client.Get(t.Context(), "sandbox-a")
			assertSafeControlError(t, err)
		})
	}
}

func TestListRepeatedCursorFailsInsteadOfLooping(t *testing.T) {
	requests := 0
	client, _ := newControlTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("X-Next-Token", "same-cursor")
		_, _ = w.Write([]byte("[]"))
	})
	_, err := client.List(t.Context(), map[string]string{"owner": "one"})
	if err == nil || requests != 2 {
		t.Fatalf("repeated cursor did not terminate: err=%v requests=%d", err, requests)
	}
}

type controlRoundTripFunc func(*http.Request) (*http.Response, error)

func (f controlRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTransportErrorAndCancellationPreserveOnlySafeCauses(t *testing.T) {
	client, err := NewClient(ClientOptions{APIKey: testProviderSecret, HTTPClient: &http.Client{
		Transport: controlRoundTripFunc(func(r *http.Request) (*http.Response, error) { return nil, errors.New(testProviderSecret) }),
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Get(t.Context(), "sandbox-a")
	assertSafeControlError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = client.Get(ctx, "sandbox-a")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation cause lost: %v", err)
	}
}

func TestSandboxEndpointsSupportPrivateCloudAndEmbed(t *testing.T) {
	cloud, err := NewClient(ClientOptions{APIKey: "key", Domain: "fallback.example"})
	if err != nil {
		t.Fatal(err)
	}
	sb := &Sandbox{ID: "sandbox-a", Domain: "region.e2b.app", TrafficAccessToken: testProviderSecret}
	u, headers, err := cloud.SandboxEndpoint(sb, 8081)
	if err != nil || u.String() != "https://8081-sandbox-a.region.e2b.app" || headers.Get("E2b-Sandbox-Port") != "8081" ||
		headers.Get("E2b-Sandbox-Id") != sb.ID || headers.Get("e2b-traffic-access-token") != testProviderSecret || headers.Get("X-API-Key") != "" {
		t.Fatal("cloud private routing or credential separation is wrong")
	}
	sb.Domain = ""
	u, _, err = cloud.SandboxEndpoint(sb, 22)
	if err != nil || u.Host != "22-sandbox-a.fallback.example" {
		t.Fatal("domain fallback is wrong")
	}
	sb.Domain = "untrusted/path?secret=value"
	if _, _, err = cloud.SandboxEndpoint(sb, 22); err == nil {
		t.Fatal("accepted a non-domain provider routing value")
	}
	embed, _ := newControlTestClient(t, func(w http.ResponseWriter, r *http.Request) {})
	u, _, err = embed.SandboxEndpoint(sb, 8081)
	if err != nil || u.String() != embed.sandboxOrigin.String() {
		t.Fatal("custom Embed origin was not used")
	}
	sb.TrafficAccessToken = ""
	if _, _, err = embed.SandboxEndpoint(sb, 8081); err == nil {
		t.Fatal("public sandbox traffic fallback was allowed")
	}
}

func TestInvalidControlInputsMakeNoProviderRequests(t *testing.T) {
	requests := 0
	client, _ := newControlTestClient(t, func(w http.ResponseWriter, r *http.Request) { requests++ })
	for _, id := range []string{"", "../escape", "with space", "sandbox?token=" + testProviderSecret} {
		_, err := client.Get(t.Context(), id)
		assertSafeControlError(t, err)
		assertSafeControlError(t, client.Delete(t.Context(), id))
		assertSafeControlError(t, client.SetTimeout(t.Context(), id, 10))
		assertSafeControlError(t, client.Pause(t.Context(), id))
		_, err = client.Connect(t.Context(), id, 10)
		assertSafeControlError(t, err)
	}
	_, err := client.Create(t.Context(), CreateOptions{TemplateID: "base", TimeoutSeconds: 0})
	assertSafeControlError(t, err)
	_, err = client.List(t.Context(), nil)
	assertSafeControlError(t, err)
	assertSafeControlError(t, client.SetTimeout(t.Context(), "sandbox-a", 0))
	_, err = client.Connect(t.Context(), "sandbox-a", 0)
	assertSafeControlError(t, err)
	if requests != 0 {
		t.Fatal("invalid input reached provider")
	}
}
