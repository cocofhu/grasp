package runtime

import "testing"

func TestProviderImageFollowsGatewayUnlessOverridden(t *testing.T) {
	for _, backend := range []AcpBackend{BackendCursor, BackendCodex, BackendClaudeCode} {
		if got := resolveProviderImage(Options{}, backend); got != "" {
			t.Fatalf("unexpected default image %q", got)
		}
	}
	opts := Options{SandboxImages: map[string]string{"codex": "explicit-template"}}
	if got := resolveProviderImage(opts, BackendCodex); got != "explicit-template" {
		t.Fatalf("backend override lost: %q", got)
	}
	opts.SandboxImage = "global-template"
	if got := resolveProviderImage(opts, BackendCodex); got != "global-template" {
		t.Fatalf("global override lost: %q", got)
	}
}
