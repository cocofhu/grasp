package codexoauth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func newServer(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return NewClient(srv.URL + "/")
}

func TestNewClientDefaultsAndProxy(t *testing.T) {
	c := NewClient("  ")
	if c.BaseURL() != DefaultBaseURL {
		t.Fatalf("base %q", c.BaseURL())
	}
	if c.VerificationURL() != "https://auth.openai.com/codex/device" {
		t.Fatalf("verification %q", c.VerificationURL())
	}
	if c.deviceRedirectURI() != "https://auth.openai.com/deviceauth/callback" {
		t.Fatalf("redirect %q", c.deviceRedirectURI())
	}
	tr, ok := c.http.Transport.(*http.Transport)
	if !ok || tr.Proxy == nil {
		t.Fatal("transport must follow proxy environment")
	}
	t.Setenv("HTTPS_PROXY", "http://proxy.example:3128")
	req, _ := http.NewRequest(http.MethodPost, "https://auth.openai.com/oauth/token", nil)
	u, err := tr.Proxy(req)
	if err != nil || u == nil || u.Host != "proxy.example:3128" {
		t.Fatalf("proxy %v %v", u, err)
	}
}

func TestRefreshSendsCodexParameters(t *testing.T) {
	var form url.Values
	c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/token" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("content type %q", ct)
		}
		body, _ := io.ReadAll(r.Body)
		form, _ = url.ParseQuery(string(body))
		_, _ = w.Write([]byte(`{"id_token":"id2","access_token":"at2","refresh_token":"rt2"}`))
	})
	tok, err := c.Refresh(context.Background(), "rt1")
	if err != nil {
		t.Fatal(err)
	}
	if tok != (Tokens{IDToken: "id2", AccessToken: "at2", RefreshToken: "rt2"}) {
		t.Fatalf("tokens %+v", tok)
	}
	want := map[string]string{"grant_type": "refresh_token", "client_id": ClientID, "scope": "openid profile email", "refresh_token": "rt1"}
	for k, v := range want {
		if form.Get(k) != v {
			t.Errorf("%s=%q want %q", k, form.Get(k), v)
		}
	}
}

func TestRefreshErrorClassification(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		body     string
		terminal bool
		code     string
	}{
		{"reused nested", 400, `{"error":{"code":"refresh_token_reused","message":"already used"}}`, true, "refresh_token_reused"},
		{"expired nested", 400, `{"error":{"code":"refresh_token_expired"}}`, true, "refresh_token_expired"},
		{"invalidated", 400, `{"error":{"type":"refresh_token_invalidated"}}`, true, "refresh_token_invalidated"},
		{"invalid grant flat", 400, `{"error":"invalid_grant","error_description":"bad"}`, true, "invalid_grant"},
		{"unauthorized", 401, `{"error":"unauthorized"}`, true, "unauthorized"},
		{"top level code", 400, `{"code":"refresh_token_reused","message":"m"}`, true, "refresh_token_reused"},
		{"server error", 502, `<html>bad gateway</html>`, false, "http_502"},
		{"rate limited", 429, `{"error":{"message":"slow down"}}`, false, "http_429"},
		{"region", 403, `{"error":{"code":"unsupported_country_region_territory"}}`, false, "unsupported_country_region_territory"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			_, err := c.Refresh(context.Background(), "rt")
			if err == nil {
				t.Fatal("want error")
			}
			if IsTerminal(err) != tc.terminal {
				t.Fatalf("terminal=%v want %v (%v)", IsTerminal(err), tc.terminal, err)
			}
			var e *Error
			if !errors.As(err, &e) || e.Code != tc.code || e.Status != tc.status {
				t.Fatalf("error %#v", err)
			}
			if strings.Contains(err.Error(), "rt") && strings.Contains(err.Error(), "refresh_token=") {
				t.Fatalf("error leaks request: %v", err)
			}
		})
	}
}

func TestRefreshRejectsBlankTokenAndBadResponses(t *testing.T) {
	c := NewClient("http://127.0.0.1:1")
	if _, err := c.Refresh(context.Background(), " "); !IsTerminal(err) {
		t.Fatalf("blank refresh token must be terminal: %v", err)
	}
	for _, body := range []string{`not json`, `{"id_token":"x"}`} {
		c := newServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) })
		if _, err := c.Refresh(context.Background(), "rt"); err == nil || IsTerminal(err) {
			t.Fatalf("body %q: want transient error, got %v", body, err)
		}
	}
	// Network failure is transient.
	if _, err := c.Refresh(context.Background(), "rt"); err == nil || IsTerminal(err) {
		t.Fatalf("network error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Refresh(ctx, "rt"); err == nil {
		t.Fatal("cancelled context must fail")
	}
}

func TestErrorString(t *testing.T) {
	e := &Error{Status: 400, Code: "invalid_grant", Message: "token expired"}
	if got := e.Error(); got != "openai auth: status 400: invalid_grant: token expired" {
		t.Fatalf("got %q", got)
	}
	if got := (&Error{Code: "x", Message: "x"}).Error(); got != "openai auth: x" {
		t.Fatalf("got %q", got)
	}
	long := strings.Repeat("a", 500)
	if got := truncate(long, 10); got != "aaaaaaaaaa…" {
		t.Fatalf("truncate %q", got)
	}
}

func TestDeviceFlow(t *testing.T) {
	polls := 0
	var exchange url.Values
	c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/accounts/deviceauth/usercode":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["client_id"] != ClientID {
				t.Errorf("client id %q", body["client_id"])
			}
			_, _ = w.Write([]byte(`{"device_auth_id":"dev-1","user_code":"ABCD-EFGH","interval":"3"}`))
		case "/api/accounts/deviceauth/token":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["device_auth_id"] != "dev-1" || body["user_code"] != "ABCD-EFGH" {
				t.Errorf("poll body %v", body)
			}
			polls++
			if polls == 1 {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			_, _ = w.Write([]byte(`{"authorization_code":"code-1","code_verifier":"ver-1","code_challenge":"ch"}`))
		case "/oauth/token":
			raw, _ := io.ReadAll(r.Body)
			exchange, _ = url.ParseQuery(string(raw))
			_, _ = w.Write([]byte(`{"id_token":"id","access_token":"at","refresh_token":"rt"}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	start, err := c.StartDevice(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if start.DeviceAuthID != "dev-1" || start.UserCode != "ABCD-EFGH" || start.Interval != 3*time.Second || start.ExpiresIn != defaultDeviceTTL {
		t.Fatalf("start %+v", start)
	}
	if !strings.HasSuffix(start.VerificationURL, "/codex/device") {
		t.Fatalf("verification %q", start.VerificationURL)
	}
	if _, ok, err := c.PollDevice(context.Background(), start); ok || err != nil {
		t.Fatalf("first poll must be pending: %v %v", ok, err)
	}
	tok, ok, err := c.PollDevice(context.Background(), start)
	if err != nil || !ok {
		t.Fatalf("second poll: %v %v", ok, err)
	}
	if tok.AccessToken != "at" || tok.RefreshToken != "rt" {
		t.Fatalf("tokens %+v", tok)
	}
	want := map[string]string{"grant_type": "authorization_code", "code": "code-1", "code_verifier": "ver-1", "client_id": ClientID}
	for k, v := range want {
		if exchange.Get(k) != v {
			t.Errorf("%s=%q want %q", k, exchange.Get(k), v)
		}
	}
	if !strings.HasSuffix(exchange.Get("redirect_uri"), "/deviceauth/callback") {
		t.Errorf("redirect_uri %q", exchange.Get("redirect_uri"))
	}
}

func TestDeviceStartVariants(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		check  func(*testing.T, DeviceStart, error)
	}{
		{"not enabled 404", 404, `{}`, func(t *testing.T, _ DeviceStart, err error) {
			if !errors.Is(err, ErrDeviceNotEnabled) {
				t.Fatalf("err %v", err)
			}
		}},
		{"not enabled text", 400, `{"error":{"message":"Device code login is not enabled for this workspace"}}`, func(t *testing.T, _ DeviceStart, err error) {
			if !errors.Is(err, ErrDeviceNotEnabled) {
				t.Fatalf("err %v", err)
			}
		}},
		{"server error", 500, `{}`, func(t *testing.T, _ DeviceStart, err error) {
			var e *Error
			if !errors.As(err, &e) || e.Status != 500 {
				t.Fatalf("err %v", err)
			}
		}},
		{"bad json", 200, `nope`, func(t *testing.T, _ DeviceStart, err error) {
			if err == nil {
				t.Fatal("want error")
			}
		}},
		{"missing code", 200, `{"device_auth_id":"d"}`, func(t *testing.T, _ DeviceStart, err error) {
			if err == nil {
				t.Fatal("want error")
			}
		}},
		{"alt code numeric interval", 200, `{"device_auth_id":"d","usercode":"XY","interval":7,"expires_in":600}`, func(t *testing.T, s DeviceStart, err error) {
			if err != nil || s.UserCode != "XY" || s.Interval != 7*time.Second || s.ExpiresIn != 10*time.Minute {
				t.Fatalf("start %+v %v", s, err)
			}
		}},
		{"tiny interval", 200, `{"device_auth_id":"d","user_code":"XY","interval":0.1}`, func(t *testing.T, s DeviceStart, err error) {
			if err != nil || s.Interval != defaultDeviceInterval {
				t.Fatalf("start %+v %v", s, err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			s, err := c.StartDevice(context.Background())
			tc.check(t, s, err)
		})
	}
	if _, err := NewClient("http://127.0.0.1:1").StartDevice(context.Background()); err == nil {
		t.Fatal("network error expected")
	}
}

func TestDevicePollFailures(t *testing.T) {
	d := DeviceStart{DeviceAuthID: "d", UserCode: "u"}
	cases := []struct {
		name string
		h    http.HandlerFunc
	}{
		{"poll server error", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }},
		{"poll missing code", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"authorization_code":"c"}`)) }},
		{"exchange rejected", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/oauth/token" {
				w.WriteHeader(400)
				_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
			_, _ = w.Write([]byte(`{"authorization_code":"c","code_verifier":"v"}`))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newServer(t, tc.h)
			if _, ok, err := c.PollDevice(context.Background(), d); ok || err == nil {
				t.Fatalf("want failure, got ok=%v err=%v", ok, err)
			}
		})
	}
	if _, _, err := NewClient("http://127.0.0.1:1").PollDevice(context.Background(), d); err == nil {
		t.Fatal("network error expected")
	}
}

func TestFlexSeconds(t *testing.T) {
	if got := flexSeconds(nil, time.Second); got != time.Second {
		t.Fatal(got)
	}
	if got := flexSeconds(json.RawMessage(`"x"`), time.Second); got != time.Second {
		t.Fatal(got)
	}
	if got := flexSeconds(json.RawMessage(`true`), time.Second); got != time.Second {
		t.Fatal(got)
	}
	if got := flexSeconds(json.RawMessage(`"2.5"`), time.Second); got != 2500*time.Millisecond {
		t.Fatal(got)
	}
}

func TestScrubURLError(t *testing.T) {
	inner := errors.New("dial failed")
	if got := scrubURLError(&url.Error{Op: "Post", URL: "https://x", Err: inner}); got != inner {
		t.Fatalf("got %v", got)
	}
	if got := scrubURLError(inner); got != inner {
		t.Fatalf("got %v", got)
	}
}
