package sandbox

import "testing"

func TestApplyPasswords(t *testing.T) {
	env := map[string]string{}
	ApplyPasswords(env, "  secret  ")
	// Unified auth: every exposed service (incl. the acp-bridge) requires the
	// same token; the platform's ACP client logs in with it before dialing /ws.
	if env["ROOT_PASSWORD"] != "secret" || env[BridgePasswordEnv] != "secret" {
		t.Fatalf("env=%v", env)
	}
	if len(env) != 2 {
		t.Fatalf("only ROOT_PASSWORD and ACP_BRIDGE_PASSWORD may be set: %v", env)
	}
	ApplyPasswords(env, "")
	if env[BridgePasswordEnv] != "secret" {
		t.Fatal("empty password should not clear")
	}
	ApplyPasswords(nil, "x") // no panic
}
