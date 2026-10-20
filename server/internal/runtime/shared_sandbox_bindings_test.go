package runtime

import (
	"context"
	"testing"
)

func bindingCredentialResolver(_ string, id string) (string, string, bool) {
	switch id {
	case "ai-a":
		return "GRASP_CURSOR_API_KEY", "cursor-key-a", true
	case "ai-b":
		return "GRASP_CURSOR_API_KEY", "cursor-key-b", true
	case "gh-a":
		return "GITHUB_TOKEN", "gh-token-a", true
	case "gh-b":
		return "GITHUB_TOKEN", "gh-token-b", true
	case "svc-a":
		return "CUSTOM_SERVICE_KEY", "svc-secret-a", true
	}
	return "", "", false
}

func newBindingFlow(t *testing.T, agents map[string]string) (*sharedFlow, *acpProvider) {
	t.Helper()
	f := newSharedFlow(t, agents, idleChat)
	f.opts.SelectedCredentialForProject = bindingCredentialResolver
	f.opts.ProjectCredentialsForProject = func(string) map[string]string {
		return map[string]string{"GRASP_CURSOR_API_KEY": "project-default-key"}
	}
	return f, f.provider(t, BackendCursor)
}

var bindingAgents = map[string]string{
	"dev-a":  `{"acpBackend":"cursor","credentialBindings":{"GRASP_CURSOR_API_KEY":"ai-a","GITHUB_TOKEN":"gh-a","CUSTOM_SERVICE_KEY":"svc-a"}}`,
	"dev-b":  `{"acpBackend":"cursor","credentialBindings":{"GRASP_CURSOR_API_KEY":"ai-b","GITHUB_TOKEN":"gh-b"}}`,
	"dev-a2": `{"acpBackend":"cursor","credentialBindings":{"GRASP_CURSOR_API_KEY":"ai-b","GITHUB_TOKEN":"gh-a"}}`,
}

// Keys a per-Agent binding injects ride the node overlay, not the shared
// container env: the AI key and a custom key outside the secret list (the
// latter needs markChangedKeys to run after ApplyCredentialBindings).
func TestSharedSandboxBindingKeysRideNodeOverlay(t *testing.T) {
	f, p := newBindingFlow(t, bindingAgents)
	a, err := p.specLayered(f.req("node-a", "dev-a"), true, false)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"GRASP_CURSOR_API_KEY": "cursor-key-a", "CUSTOM_SERVICE_KEY": "svc-secret-a"} {
		if v, ok := a.spec.Env[key]; ok {
			t.Fatalf("binding-injected %s leaked into the shared container env: %q", key, v)
		}
		if a.overlay[key] != want {
			t.Fatalf("overlay %s=%q, want %q", key, a.overlay[key], want)
		}
	}
}

// A bound key that stays in the container env (GITHUB_TOKEN, used for the
// clone at create time) is part of the compatibility fingerprint through its
// credential id: different tokens never share one container, while nodes that
// only differ in overlay-scoped bindings still do.
func TestSharedSandboxContainerScopedBindingSplitsFingerprint(t *testing.T) {
	f, p := newBindingFlow(t, bindingAgents)
	spec := func(node, profile string) layeredSpec {
		ls, err := p.specLayered(f.req(node, profile), true, false)
		if err != nil {
			t.Fatal(err)
		}
		return ls
	}
	a, b, a2 := spec("node-a", "dev-a"), spec("node-b", "dev-b"), spec("node-c", "dev-a2")
	if a.spec.Env["GITHUB_TOKEN"] != "gh-token-a" || b.spec.Env["GITHUB_TOKEN"] != "gh-token-b" {
		t.Fatalf("container GITHUB_TOKEN a=%q b=%q", a.spec.Env["GITHUB_TOKEN"], b.spec.Env["GITHUB_TOKEN"])
	}
	if a.fingerprint == b.fingerprint {
		t.Fatal("nodes bound to different GITHUB_TOKEN credentials must not share a fingerprint")
	}
	// dev-a2 shares dev-a's git binding; it differs only in the AI key (overlay)
	// and in CUSTOM_SERVICE_KEY, which dev-a binds as overlay env.
	if a.fingerprint != a2.fingerprint {
		t.Fatal("overlay-only binding differences must not split the shared container")
	}
}

// End to end: node-b, bound to another git token, gets its own container
// instead of reusing the one created with node-a's token.
func TestSharedSandboxDifferentGitBindingGetsOwnContainer(t *testing.T) {
	f, p := newBindingFlow(t, bindingAgents)
	sbA, acpA, homeA, err := p.openSandbox(context.Background(), f.req("node-a", "dev-a"))
	if err != nil {
		t.Fatal(err)
	}
	p.retireRunSandbox(sbA, acpA, homeA)
	sbB, acpB, homeB, err := p.openSandbox(context.Background(), f.req("node-b", "dev-b"))
	if err != nil {
		t.Fatal(err)
	}
	defer p.retireRunSandbox(sbB, acpB, homeB)
	if sbA == sbB || f.mgr.createCount() != 2 {
		t.Fatalf("node-b reused node-a's container (created=%d)", f.mgr.createCount())
	}
}
