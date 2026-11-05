package privatepreview

import (
	"strings"
	"testing"
)

func TestIsolatedPreviewAddresses(t *testing.T) {
	b, err := Parse("https://preview.example.net:8443/")
	if err != nil {
		t.Fatal(err)
	}
	if b.Origin() != "https://preview.example.net:8443" {
		t.Fatal("base origin changed")
	}
	ticket := strings.Repeat("a", 32)
	want := "https://" + ticket + ".preview.example.net:8443/"
	if got := b.URL(ticket); got != want || !b.OwnsURL(got) {
		t.Fatalf("URL = %q, want %q", got, want)
	}
	if got, ok := b.Ticket(ticket + ".preview.example.net:8443"); !ok || got != ticket {
		t.Fatalf("ticket = %q, %v", got, ok)
	}
	// Reserve all ports and malformed labels, even though they cannot redeem
	// the capability, so none can fall through to platform routes.
	for _, host := range []string{"preview.example.net", "bad.preview.example.net", ticket + ".preview.example.net:1234", "nested." + ticket + ".preview.example.net:8443"} {
		if !b.ContainsHost(host) {
			t.Errorf("host must be reserved: %s", host)
		}
		if _, ok := b.Ticket(host); ok {
			t.Errorf("host must not redeem a ticket: %s", host)
		}
	}
	for _, raw := range []string{strings.Replace(want, "https:", "http:", 1), want + "x", want + "?token=x", want + "#x", "https://preview.example.net/", "https://" + ticket + ".preview.example.net.attacker.test:8443/"} {
		if b.OwnsURL(raw) {
			t.Errorf("unexpected owned URL: %s", raw)
		}
	}
	first, err := b.NewURL()
	if err != nil || !b.OwnsURL(first) {
		t.Fatalf("new URL = %q, %v", first, err)
	}
	second, err := b.NewURL()
	if err != nil || second == first {
		t.Fatalf("second URL = %q, %v", second, err)
	}
	if (Base{}).ContainsHost("example.net") || b.URL("bad") != "" || b.ContainsHost("preview.example.net.attacker.test") {
		t.Fatal("invalid base or ticket accepted")
	}
	if !b.ContainsHost("unknown.preview.example.net.") {
		t.Fatal("DNS trailing dot escaped reserved preview routing")
	}
}

func TestPrivatePreviewPlatformSiteIsolation(t *testing.T) {
	for _, tc := range []struct {
		name, preview, platform string
		production, wantError   bool
	}{
		{name: "production different sites", preview: "https://preview.example.net", platform: "https://grasp.example.com", production: true},
		{name: "production sibling origins share cookies", preview: "https://preview.example.com", platform: "https://grasp.example.com", production: true, wantError: true},
		{name: "production public suffix-aware", preview: "https://preview.example.co.uk", platform: "https://grasp.example.co.uk", production: true, wantError: true},
		{name: "production provider private suffix", preview: "https://preview.tenant.github.io", platform: "https://grasp.other.github.io", production: true},
		{name: "production platform IP", preview: "https://preview.example.net", platform: "http://192.0.2.1", production: true},
		{name: "development localhost", preview: "http://preview.localhost:8080", platform: "http://grasp.localhost:8080"},
		{name: "production localhost forbidden", preview: "http://preview.localhost:8080", platform: "http://grasp.localhost:8080", production: true, wantError: true},
		{name: "origin overlap always forbidden", preview: "https://example.net", platform: "https://grasp.example.net", wantError: true},
		{name: "malformed platform", preview: "https://preview.example.net", platform: "not an origin", wantError: true},
		{name: "platform credentials", preview: "https://preview.example.net", platform: "https://user:password@grasp.example.com", wantError: true},
		{name: "nonregistrable platform", preview: "https://preview.example.net", platform: "https://com", production: true, wantError: true},
		{name: "nonregistrable preview", preview: "https://net", platform: "https://grasp.example.com", production: true, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base, err := Parse(tc.preview)
			if err != nil {
				t.Fatal(err)
			}
			if err := base.ValidatePlatformOrigin(tc.platform, tc.production); (err != nil) != tc.wantError {
				t.Fatalf("isolation error = %v, wantError %v", err, tc.wantError)
			}
		})
	}
}

func TestRejectInvalidPreviewBase(t *testing.T) {
	for _, raw := range []string{"", "ftp://preview.example.net", "https://u:p@preview.example.net", "http://127.0.0.1", "http://[::1]", "https://preview.example.net/path", "https://preview.example.net?q=1", "https://preview.example.net?", "https://preview.example.net#x", "https://bad_.example.net", "https://preview.example.net.", "http://host:bad", "http://host:0", "http://host:65536"} {
		t.Run(raw, func(t *testing.T) {
			if _, err := Parse(raw); err == nil {
				t.Fatal("invalid base accepted")
			}
		})
	}
	if _, err := Parse("http://preview.localhost:8080"); err != nil {
		t.Fatal(err)
	}
	if ValidTicket(strings.Repeat("A", 32)) || ValidTicket(strings.Repeat("x", 32)) || ValidTicket("short") {
		t.Fatal("invalid ticket accepted")
	}
}
