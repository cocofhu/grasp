package sandbox

import "strings"

// BridgePasswordEnv is the only env var the acp-bridge reads its login secret
// from. Manager.Create refuses a spec without it.
const BridgePasswordEnv = "ACP_BRIDGE_PASSWORD"

// ApplyPasswords sets the password env vars the universal-sandbox image
// recognizes so direct host:port access (not via the platform proxy) requires
// the same secret everywhere:
//
//   - ROOT_PASSWORD         → root shell / SSH and code-server (port 8744) login
//   - ACP_BRIDGE_PASSWORD   → acp-bridge (port 8765) /ws + UI login
//
// Auth is unified: every exposed service requires the sandbox token. The
// platform's own ACP client (see ACPClient.WithPassword) and WaitForACPReady
// log in with this same token (POST /api/login → session cookie)
// before dialing /ws.
func ApplyPasswords(env map[string]string, password string) {
	if env == nil {
		return
	}
	password = strings.TrimSpace(password)
	if password == "" {
		return
	}
	env["ROOT_PASSWORD"] = password
	env[BridgePasswordEnv] = password
}
