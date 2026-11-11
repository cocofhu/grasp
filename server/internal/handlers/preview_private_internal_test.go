package handlers

import (
	"net/http"
	"testing"

	"github.com/cocofhu/grasp/internal/mcp"
)

func TestCheckPrivateInjectorUpstream(t *testing.T) {
	reg := mcp.PreviewPort{Port: 5174, InjectorPort: 18080}
	resp := func(status int, upstream ...string) *http.Response {
		return &http.Response{StatusCode: status, Header: http.Header{"X-Grasp-Upstream": upstream}}
	}
	cases := []struct {
		name string
		reg  mcp.PreviewPort
		path string
		resp *http.Response
		ok   bool
	}{
		{"matching app", reg, "/", resp(200, "5174"), true},
		{"fallback app", reg, "/", resp(200, "3000"), false},
		{"missing marker", reg, "/", resp(200), false},
		{"duplicated marker", reg, "/", resp(200, "5174", "5174"), false},
		{"non-canonical marker", reg, "/", resp(200, "05174"), false},
		{"runtime route", reg, "/__grasp/preview-pick.js", resp(200), true},
		{"runtime route upgrade", reg, "/__grasp/embed-origin", resp(http.StatusSwitchingProtocols), false},
		{"no injector", mcp.PreviewPort{Port: 5174}, "/", resp(200), true},
	}
	for _, tc := range cases {
		if err := checkPrivateInjectorUpstream(tc.reg, tc.path, tc.resp); (err == nil) != tc.ok {
			t.Errorf("%s: err=%v", tc.name, err)
		}
	}
}

func TestStripPrivateReservedHeaders(t *testing.T) {
	h := http.Header{}
	for _, k := range []string{"E2b-Traffic-Access-Token", "X-E2b-Sandbox-Id", "Proxy-Authorization", "Authorization", "X-Api-Key"} {
		h.Set(k, "v")
	}
	stripPrivateReservedHeaders(h)
	if len(h) != 2 || h.Get("Authorization") == "" || h.Get("X-Api-Key") == "" {
		t.Fatalf("headers: %v", h)
	}
}
