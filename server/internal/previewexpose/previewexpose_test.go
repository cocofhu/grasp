package previewexpose

import (
	"context"
	"strings"
	"testing"
)

func TestParseBaseAndMatchHost(t *testing.T) {
	base, err := ParseBase("https://Preview.Example.COM:443/")
	if err != nil {
		t.Fatalf("ParseBase: %v", err)
	}
	if base.Origin() != "https://preview.example.com" || base.Port() != "" {
		t.Fatalf("unexpected normalized base: origin=%q port=%q", base.Origin(), base.Port())
	}

	withPort, err := ParseBase("http://preview.example.com:8080")
	if err != nil {
		t.Fatalf("ParseBase with port: %v", err)
	}
	if withPort.Origin() != "http://preview.example.com:8080" || withPort.Port() != "8080" {
		t.Fatalf("unexpected port base: %q", withPort.Origin())
	}

	for _, bad := range []string{
		"",
		"ftp://preview.example.com",
		"http://127.0.0.1:8080",
		"https://10.0.0.5",
		"http://[::1]:8080",
		"https://user:pass@preview.example.com",
		"https://preview.example.com/path",
		"https://preview.example.com?q=1",
		"https://preview.example.com#frag",
		"https://preview.example.com.",
		"https://bad_label.example.com",
		"https://preview.example.com:99999",
	} {
		if _, err := ParseBase(bad); err == nil {
			t.Errorf("ParseBase(%q) unexpectedly succeeded", bad)
		}
	}

	sub := NewSubdomain(base)
	key, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	if got := sub.URL(Target{}, Binding{Strategy: StrategySubdomain, Key: key}); got != "https://"+key+".preview.example.com/" {
		t.Fatalf("URL = %q", got)
	}
	if got := sub.URL(Target{}, Binding{Strategy: StrategyNone, Key: key}); got != "" {
		t.Fatalf("mismatched strategy must return empty URL, got %q", got)
	}
	if got := sub.URL(Target{}, Binding{Strategy: StrategySubdomain, Key: "not-a-key"}); got != "" {
		t.Fatalf("invalid key must return empty URL, got %q", got)
	}

	cases := []struct {
		host    string
		wantKey string
		claimed bool
	}{
		{key + ".preview.example.com", key, true},
		{key + ".preview.example.com:443", key, true},
		{strings.ToUpper(key) + ".PREVIEW.EXAMPLE.COM", key, true},
		{"preview.example.com", "", true},
		{"unknown.preview.example.com", "", true},
		{"a." + key + ".preview.example.com", "", true},
		{key + ".preview.example.com:8443", "", true},
		{"grasp.example.com", "", false},
		{"evilpreview.example.com", "", false},
	}
	for _, tc := range cases {
		gotKey, claimed := sub.MatchHost(tc.host)
		if gotKey != tc.wantKey || claimed != tc.claimed {
			t.Errorf("MatchHost(%q) = (%q, %v), want (%q, %v)", tc.host, gotKey, claimed, tc.wantKey, tc.claimed)
		}
	}

	for host, want := range map[string]bool{
		key + ".retired.example.com":       true,
		key + ".retired.example.com:8080":  true,
		strings.ToUpper(key) + ".OLD.TEST": true,
		key:                                false,
		"grasp.example.com":                false,
		"a." + key + ".example.com":        false,
		key[:31] + ".example.com":          false,
	} {
		if got := TicketHost(host); got != want {
			t.Errorf("TicketHost(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestValidatePlatformOrigin(t *testing.T) {
	base, _ := ParseBase("https://grasp-preview.other-site.com")
	if warn, err := base.ValidatePlatformOrigin("https://grasp.example.com", true); err != nil || warn != "" {
		t.Fatalf("valid prod cross-site failed: warn=%q err=%v", warn, err)
	}

	sameSite, _ := ParseBase("https://preview.example.com")
	if _, err := sameSite.ValidatePlatformOrigin("https://grasp.example.com", true); err == nil {
		t.Fatal("expected production same-site error")
	}
	if warn, err := sameSite.ValidatePlatformOrigin("https://grasp.example.com", false); err != nil || warn == "" {
		t.Fatalf("expected non-prod same-site warning, got warn=%q err=%v", warn, err)
	}

	httpBase, _ := ParseBase("http://grasp-preview.other-site.com")
	if _, err := httpBase.ValidatePlatformOrigin("https://grasp.example.com", true); err == nil {
		t.Fatal("expected production non-https error")
	}

	localhostBase, _ := ParseBase("https://preview.localhost")
	if _, err := localhostBase.ValidatePlatformOrigin("https://grasp.example.com", true); err == nil {
		t.Fatal("expected production localhost error")
	}

	nested, _ := ParseBase("https://sub.grasp.example.com")
	if _, err := nested.ValidatePlatformOrigin("https://grasp.example.com", false); err == nil {
		t.Fatal("expected nested subdomain of platform to fail")
	}
	if _, err := base.ValidatePlatformOrigin("https://grasp-preview.other-site.com", false); err == nil {
		t.Fatal("expected identical host to fail")
	}

	// A Unicode platform host and a Punycode preview base on the same
	// registrable site must still be recognized as the same site.
	idnBase, err := ParseBase("https://preview.xn--fsqu00a.com")
	if err != nil {
		t.Fatalf("ParseBase punycode: %v", err)
	}
	if _, err := idnBase.ValidatePlatformOrigin("https://grasp.例子.com", true); err == nil {
		t.Fatal("expected production same-site error for Unicode/Punycode spellings of one site")
	}
	if warn, err := idnBase.ValidatePlatformOrigin("https://grasp.例子.com", false); err != nil || warn == "" {
		t.Fatalf("expected non-prod same-site warning for IDN site, got warn=%q err=%v", warn, err)
	}
	idnNested, _ := ParseBase("https://preview.xn--fsqu00a.com")
	if _, err := idnNested.ValidatePlatformOrigin("https://例子.com", false); err == nil {
		t.Fatal("expected preview base nested under Unicode platform host to fail")
	}
	// Pure-ASCII hosts the IDNA profile rejects (container names) still work.
	if _, err := base.ValidatePlatformOrigin("http://grasp_server:8080", false); err != nil {
		t.Fatalf("underscore platform host rejected: %v", err)
	}
}

func TestRetiredTicketHostSparesPlatformHost(t *testing.T) {
	platformKey, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	otherKey, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := NewPolicy(PolicyOptions{
		GlobalStrategy:  StrategyNone,
		PublicAdvertise: "https://" + platformKey + ".example.com",
		Connections:     []ConnSpec{{ConnID: "", Transport: "direct"}},
	})
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}
	for host, want := range map[string]bool{
		platformKey + ".example.com":                   false,
		platformKey + ".example.com:443":               false,
		strings.ToUpper(platformKey) + ".EXAMPLE.COM.": false,
		otherKey + ".example.com":                      true,
		platformKey + ".retired.example.net":           true,
		"grasp.example.com":                            false,
	} {
		if got := p.RetiredTicketHost(host); got != want {
			t.Errorf("RetiredTicketHost(%q) = %v, want %v", host, got, want)
		}
	}
	var nilPolicy *Policy
	if !nilPolicy.RetiredTicketHost(otherKey + ".example.com") {
		t.Error("nil policy should still treat ticket hosts as retired")
	}
}

func TestPolicyEffectiveRulesAndValidation(t *testing.T) {
	t.Run("effective rules matrix", func(t *testing.T) {
		p, warnings, err := NewPolicy(PolicyOptions{
			GlobalStrategy:   StrategyAuto,
			GlobalBaseURL:    "https://shared.other-site.com",
			PrivateEndpoints: false,
			PublicAdvertise:  "https://grasp.example.com",
			Production:       true,
			Connections: []ConnSpec{
				{ConnID: "", Transport: "direct"},                                               // auto -> direct
				{ConnID: "gw-auto", Transport: "gateway"},                                       // auto + global base -> subdomain (shared)
				{ConnID: "gw-own", Transport: "gateway", BaseURL: "https://e2b.third-site.com"}, // auto + own base -> subdomain (own)
				{ConnID: "gw-none", Transport: "gateway", Strategy: StrategyNone},               // explicit none
				{ConnID: "dir-sub", Transport: "direct", Strategy: StrategySubdomain},           // direct explicitly using subdomain (shared)
				{ConnID: "dir-none", Transport: "direct", Strategy: StrategyNone},               // direct explicitly none
			},
		})
		if err != nil || len(warnings) != 0 {
			t.Fatalf("NewPolicy: warn=%v err=%v", warnings, err)
		}
		if p.For("", true).Name() != StrategyDirect {
			t.Fatalf("legacy = %s", p.For("", true).Name())
		}
		if p.For("gw-auto", true).Name() != StrategySubdomain {
			t.Fatalf("gw-auto = %s", p.For("gw-auto", true).Name())
		}
		if p.For("gw-auto", true) != p.For("dir-sub", true) {
			t.Fatal("connections sharing the same base_url must share one SubdomainExposer instance")
		}
		if p.For("gw-own", true) == p.For("gw-auto", true) {
			t.Fatal("connections with distinct base_urls must use distinct SubdomainExposer instances")
		}
		if p.For("gw-none", true).Name() != StrategyNone || p.For("dir-none", true).Name() != StrategyNone {
			t.Fatal("explicit none failed")
		}
		if p.For("unknown", false).Name() != StrategyNone || p.For("unknown", true).Name() != StrategyNone {
			t.Fatal("unknown connection must fail closed to none")
		}
		if len(p.Inbounds()) != 2 {
			t.Fatalf("expected 2 deduplicated inbounds, got %d", len(p.Inbounds()))
		}
	})

	t.Run("private_endpoints affects auto only and explicit conn auto overrides global none", func(t *testing.T) {
		p, _, err := NewPolicy(PolicyOptions{
			GlobalStrategy:   StrategyNone,
			PrivateEndpoints: true,
			PublicAdvertise:  "http://grasp.example.com",
			Connections: []ConnSpec{
				{ConnID: "", Transport: "direct"},                                       // inherits global none
				{ConnID: "dir-explicit", Transport: "direct", Strategy: StrategyDirect}, // explicit direct overrides private_endpoints
				{ConnID: "dir-auto", Transport: "direct", Strategy: StrategyAuto},       // explicit auto + private_endpoints + no base -> none
			},
		})
		if err != nil {
			t.Fatalf("NewPolicy: %v", err)
		}
		if p.For("", true).Name() != StrategyNone {
			t.Fatalf("legacy = %s", p.For("", true).Name())
		}
		if p.For("dir-explicit", true).Name() != StrategyDirect {
			t.Fatalf("dir-explicit = %s", p.For("dir-explicit", true).Name())
		}
		if p.For("dir-auto", true).Name() != StrategyNone {
			t.Fatalf("dir-auto = %s", p.For("dir-auto", true).Name())
		}
	})

	t.Run("explicit conn auto inherits the global strategy", func(t *testing.T) {
		p, _, err := NewPolicy(PolicyOptions{
			GlobalStrategy:  StrategyNone,
			PublicAdvertise: "http://grasp.example.com",
			Connections: []ConnSpec{
				{ConnID: "dir-auto", Transport: "direct", Strategy: StrategyAuto},
			},
		})
		if err != nil {
			t.Fatalf("NewPolicy: %v", err)
		}
		if p.For("dir-auto", true).Name() != StrategyNone {
			t.Fatalf("dir-auto = %s, want none from global", p.For("dir-auto", true).Name())
		}
	})

	t.Run("validation rejects invalid configs", func(t *testing.T) {
		badCases := []struct {
			name string
			opts PolicyOptions
		}{
			{
				name: "global direct forbidden",
				opts: PolicyOptions{GlobalStrategy: StrategyDirect, PublicAdvertise: "http://grasp.example.com", Connections: []ConnSpec{{ConnID: "", Transport: "direct"}}},
			},
			{
				name: "gateway transport with direct",
				opts: PolicyOptions{PublicAdvertise: "http://grasp.example.com", Connections: []ConnSpec{{ConnID: "e2b", Transport: "gateway", Strategy: StrategyDirect}}},
			},
			{
				name: "subdomain without base_url",
				opts: PolicyOptions{PublicAdvertise: "http://grasp.example.com", Connections: []ConnSpec{{ConnID: "e2b", Transport: "gateway", Strategy: StrategySubdomain}}},
			},
			{
				name: "overlapping distinct base_urls",
				opts: PolicyOptions{
					PublicAdvertise: "https://grasp.example.com",
					Production:      true,
					Connections: []ConnSpec{
						{ConnID: "a", Transport: "gateway", BaseURL: "https://preview.other-site.com"},
						{ConnID: "b", Transport: "gateway", BaseURL: "https://e2b.preview.other-site.com"},
					},
				},
			},
			{
				name: "unknown strategy",
				opts: PolicyOptions{GlobalStrategy: "tunnel", PublicAdvertise: "http://grasp.example.com"},
			},
		}
		for _, tc := range badCases {
			t.Run(tc.name, func(t *testing.T) {
				if _, _, err := NewPolicy(tc.opts); err == nil {
					t.Fatalf("expected error for %s", tc.name)
				}
			})
		}
	})
}

func TestSubdomainBindAndDirectExposer(t *testing.T) {
	base, _ := ParseBase("http://preview.other.test")
	sub := NewSubdomain(base)
	b, err := sub.Bind(context.Background(), Target{})
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if b.Strategy != StrategySubdomain || !ValidKey(b.Key) {
		t.Fatalf("unexpected binding: %+v", b)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := sub.Bind(ctx, Target{}); err == nil {
		t.Fatal("expected canceled context error")
	}

	if got := Direct().URL(Target{Upstream: "http://10.0.0.5:18080/"}, Binding{}); got != "http://10.0.0.5:18080/" {
		t.Fatalf("Direct().URL = %q", got)
	}
	if got := Direct().URL(Target{}, Binding{}); got != "" {
		t.Fatalf("Direct().URL empty upstream = %q", got)
	}
	if got := None().URL(Target{Upstream: "http://10.0.0.5:18080"}, Binding{Strategy: StrategyDirect}); got != "" {
		t.Fatalf("None().URL = %q", got)
	}
}
