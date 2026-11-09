package sandbox

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"sync"
)

// DoctorBootstrap contains only disposable sandbox startup credentials. It
// carries no Agent, model, Git or run-scoped MCP authorization.
type DoctorBootstrap struct {
	Env map[string]string `json:"env"`
}

// Validate rejects incomplete bootstrap responses before the doctor contacts
// the provider. In particular, arbitrary session data cannot become an env
// override for the scratch sandbox.
func (b DoctorBootstrap) Validate() error {
	if len(b.Env) != 6 || b.Env["WORKSPACE_DIR"] != "/root/workspace" || b.Env["SKIP_INNER_DOCKER"] != "1" {
		return errors.New("incomplete diagnostic sandbox bootstrap")
	}
	u, err := url.Parse(b.Env["GRASP_RUNTIME_URL"])
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !strings.HasSuffix(u.Path, ".tgz") || !strings.Contains(u.Path, "/sandbox-inject/") {
		return errors.New("invalid diagnostic runtime URL")
	}
	for _, key := range []string{BridgePasswordEnv, "ROOT_PASSWORD"} {
		value, err := hex.DecodeString(b.Env[key])
		if err != nil || len(value) != 24 {
			return errors.New("invalid diagnostic sandbox credential")
		}
	}
	if b.Env[BridgePasswordEnv] == b.Env["ROOT_PASSWORD"] {
		return errors.New("diagnostic sandbox credentials must be independent")
	}
	header := b.Env["SANDBOX_INJECT_HEADERS"]
	token := strings.TrimPrefix(header, "Authorization: Bearer ")
	if header == token || token == "" || strings.ContainsAny(token, " \t\r\n") {
		return errors.New("invalid diagnostic runtime authorization")
	}
	return nil
}

// PrepareDoctorBootstrap publishes the Manager's actual runtime through the
// existing inject store. The owning diagnostic session must call cleanup on
// completion or expiry; cleanup revokes only this publication and is idempotent.
func (m *Manager) PrepareDoctorBootstrap(ctx context.Context) (DoctorBootstrap, func(), error) {
	if err := ctx.Err(); err != nil {
		return DoctorBootstrap{}, nil, err
	}
	if m == nil || m.Runtime() == nil {
		return DoctorBootstrap{}, nil, errors.New("diagnostic sandbox runtime is unavailable")
	}
	runtime, _, err := m.Runtime().Current()
	if err != nil {
		return DoctorBootstrap{}, nil, err
	}
	var passwords [48]byte
	if _, err := rand.Read(passwords[:]); err != nil {
		return DoctorBootstrap{}, nil, err
	}
	plan := m.buildMultiInject("", "", "", "", false, nil, runtime)
	if plan.runtimeURL == "" {
		return DoctorBootstrap{}, nil, errors.New("diagnostic runtime needs an inject store and sandbox-reachable advertise URL")
	}
	var once sync.Once
	cleanup := func() { once.Do(func() { m.deleteInjectBundles(plan.ids) }) }
	bootstrap := DoctorBootstrap{Env: map[string]string{
		"GRASP_RUNTIME_URL":      plan.runtimeURL,
		"SANDBOX_INJECT_HEADERS": plan.headers,
		BridgePasswordEnv:        hex.EncodeToString(passwords[:24]),
		"ROOT_PASSWORD":          hex.EncodeToString(passwords[24:]),
		"WORKSPACE_DIR":          "/root/workspace",
		"SKIP_INNER_DOCKER":      "1",
	}}
	if err := bootstrap.Validate(); err != nil {
		cleanup()
		return DoctorBootstrap{}, nil, err
	}
	if err := ctx.Err(); err != nil {
		cleanup()
		return DoctorBootstrap{}, nil, err
	}
	return bootstrap, cleanup, nil
}
