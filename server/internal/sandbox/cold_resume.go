package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/cocofhu/grasp/internal/config"
)

var ErrColdConfigurationUnsupported = errors.New("environment cannot replace legacy authorization before cold start")
var ErrColdConfigurationUnknown = errors.New("original cold configuration requires read-only confirmation")

type GWColdInjection struct {
	URL         string `json:"url"`
	Destination string `json:"destination"`
	SHA256      string `json:"sha256"`
	Bytes       int64  `json:"bytes"`
}
type GWColdStartConfig struct {
	ExpectedNativeIdentity string            `json:"expectedNativeIdentity"`
	RequestID              string            `json:"requestId"`
	InputSHA256            string            `json:"inputSha256"`
	PayloadSHA256          string            `json:"payloadSha256"`
	Env                    map[string]string `json:"env"`
	Injections             []GWColdInjection `json:"injections"`
}
type GWColdStartReceipt struct {
	RequestID      string `json:"requestId"`
	InputSHA256    string `json:"inputSha256"`
	PayloadSHA256  string `json:"payloadSha256"`
	NativeIdentity string `json:"nativeIdentity"`
	WorkspaceDir   string `json:"workspaceDir"`
	State          string `json:"state"`
}
type GWColdStartTarget struct {
	NativeIdentity string `json:"nativeIdentity"`
	WorkspaceDir   string `json:"workspaceDir"`
}

func (g *GatewayClient) ColdStartReceipt(ctx context.Context, id, request string) (*GWColdStartReceipt, error) {
	var receipt GWColdStartReceipt
	err := g.do(ctx, http.MethodGet, "/api/v1/sandboxes/"+id+"/cold-starts/"+url.PathEscape(request), nil, &receipt)
	return &receipt, err
}
func (g *GatewayClient) ColdStartTarget(ctx context.Context, id string) (*GWColdStartTarget, error) {
	var target GWColdStartTarget
	err := g.do(ctx, http.MethodGet, "/api/v1/sandboxes/"+id+"/cold-starts", nil, &target)
	return &target, err
}
func (g *GatewayClient) StartConfigured(ctx context.Context, id string, opts GWStartOptions) (*GWColdStartReceipt, error) {
	var receipt GWColdStartReceipt
	err := g.do(ctx, http.MethodPost, "/api/v1/sandboxes/"+id+"/cold-starts", opts, &receipt)
	return &receipt, err
}
func coldDigest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

// CanResumeConfigured observes support without dispatching a native operation.
// Review services call it before persisting their first dispatch marker.
func (m *Manager) CanResumeConfigured(ctx context.Context, id string) (bool, error) {
	if m == nil || m.gw == nil || m.runtime == nil || m.bundles == nil || m.workspaceOperations == nil {
		return false, nil
	}
	info, err := m.gw.InfoFor(ctx, id)
	if err != nil {
		return false, err
	}
	if info.Driver != "kubernetes" || !info.Capabilities.ColdConfiguration {
		return false, nil
	}
	if err := m.prepareWorkspaceRuntimeEnv(map[string]string{}, id, ""); err != nil {
		return false, err
	}
	return true, nil
}

// ResumeConfigured is an explicit legacy review operation, never an ordinary
// Resume fallback. An accepted request is only observed, never dispatched again.
// Preparation and all provider/object I/O run outside callers' SQL transactions.
func (m *Manager) ResumeConfigured(ctx context.Context, id string, spec Spec, requestID string) (*Sandbox, error) {
	if m == nil || m.gw == nil || m.runtime == nil || m.bundles == nil || m.workspaceOperations == nil || !runtimeWorkspaceID.MatchString(requestID) || spec.WorkspaceDir != "" || !spec.GitBindingsConfigured || len(spec.Mounts) != 0 {
		return nil, ErrColdConfigurationUnsupported
	}
	info, err := m.gw.InfoFor(ctx, id)
	if err != nil {
		return nil, err
	}
	if info.Driver != "kubernetes" || !info.Capabilities.ColdConfiguration {
		return nil, ErrColdConfigurationUnsupported
	}
	old, lookupErr := m.gw.ColdStartReceipt(ctx, id, requestID)
	var target *GWColdStartTarget
	if lookupErr == nil {
		target = &GWColdStartTarget{NativeIdentity: old.NativeIdentity, WorkspaceDir: old.WorkspaceDir}
	} else {
		if !GatewayRecordMissing(lookupErr) {
			return nil, errors.Join(ErrColdConfigurationUnknown, lookupErr)
		}
		target, err = m.gw.ColdStartTarget(ctx, id)
		if err != nil {
			return nil, err
		}
	}
	if target == nil || target.NativeIdentity == "" || target.WorkspaceDir == "" {
		return nil, ErrColdConfigurationUnknown
	}
	env := make(map[string]string, len(spec.Env))
	for k, v := range spec.Env {
		if !gitCredentialEnv[k] {
			env[k] = v
		}
	}
	env["WORKSPACE_DIR"], env["GIT_REPOS"] = target.WorkspaceDir, ""
	if err := m.prepareWorkspaceRuntimeEnv(env, id, ""); err != nil {
		return nil, err
	}
	root := spec.ConfigRoot
	if root == "" {
		return nil, ErrColdConfigurationUnsupported
	}
	env["CONFIG_ROOT"] = root
	if env[BridgePasswordEnv] == "" || env["ROOT_PASSWORD"] == "" || env["GRASP_EXECUTION_LINEAGE"] == "" || env["GRASP_WORKSPACE_CONTROL_ROOT"] == "" {
		return nil, ErrColdConfigurationUnsupported
	}
	if _, ok := env["SKIP_INNER_DOCKER"]; !ok {
		env["SKIP_INNER_DOCKER"] = "0"
	}
	_, authKey, keyErr := sharedSSHKey()
	if keyErr != nil || authKey == "" {
		return nil, fmt.Errorf("prepare current sandbox SSH authorization")
	}
	env["SSH_KEY"] = authKey
	runtimeData, _, err := m.runtime.Current()
	if err != nil {
		return nil, err
	}
	logical := spec
	logical.Env = env
	fingerprint := SpecFingerprint(logical)
	if fingerprint == "" || spec.ConfigHome == "" {
		return nil, ErrColdConfigurationUnsupported
	}
	input, _ := json.Marshal(struct {
		Resource, Native, Workspace, Spec, Runtime string
		Env                                        map[string]string
	}{id, target.NativeIdentity, target.WorkspaceDir, fingerprint, coldDigest(runtimeData), env})
	inputSHA := coldDigest(input)
	if lookupErr == nil {
		if old.RequestID != requestID || old.InputSHA256 != inputSHA || old.NativeIdentity != target.NativeIdentity || old.State != "started" {
			return nil, ErrColdConfigurationUnknown
		}
		return m.finishConfiguredResume(ctx, id, spec, target)
	}
	// Freeze every authorized byte before publishing its short-lived URL.
	gitData, err := packGitInjectBundle("", "", true, spec.GitCredentials)
	if err != nil {
		return nil, err
	}
	configData, err := PackConfigHomeTarGz(spec.ConfigHome)
	if err != nil {
		return nil, err
	}
	if len(gitData) == 0 || len(configData) == 0 || len(gitData) > 64<<20 || len(configData) > 64<<20 {
		return nil, ErrColdConfigurationUnsupported
	}
	bootstrap, err := runtimeBootstrapScript(runtimeData)
	if err != nil {
		return nil, err
	}
	base := strings.TrimRight(config.ResolveMCPAdvertise(m.injectAdvertiseFallback), "/")
	if base == "" {
		return nil, ErrColdConfigurationUnsupported
	}
	token := randomHex(24)
	var ids []string
	put := func(data []byte) string {
		bundleID := m.bundles.PutWithToken(data, DefaultInjectBundleTTL, token)
		if bundleID == "" {
			return ""
		}
		ids = append(ids, bundleID)
		return base + "/sandbox-inject/" + bundleID + ".tgz"
	}
	gitURL, configURL, runtimeURL := put(gitData), put(configData), put(runtimeData)
	if gitURL == "" || configURL == "" || runtimeURL == "" {
		m.deleteInjectBundles(ids)
		return nil, ErrColdConfigurationUnsupported
	}
	headers := "Authorization: Bearer " + token
	env["GRASP_RUNTIME_URL"], env["SANDBOX_INJECT_HEADERS"], env["SANDBOX_INJECT"] = runtimeURL, headers, ""
	cold := &GWColdStartConfig{ExpectedNativeIdentity: target.NativeIdentity, RequestID: requestID, InputSHA256: inputSHA, Env: env, Injections: []GWColdInjection{{gitURL, SSHInjectStagingDir, coldDigest(gitData), int64(len(gitData))}, {configURL, root, coldDigest(configData), int64(len(configData))}}}
	opts := GWStartOptions{BootstrapScript: bootstrap, RuntimeURL: runtimeURL, RuntimeHeaders: headers, ColdConfig: cold}
	payload, _ := json.Marshal(opts)
	cold.PayloadSHA256 = coldDigest(payload)
	receipt, err := m.gw.StartConfigured(ctx, id, opts)
	// Once a request was sent, preserve URLs through their TTL even after an
	// ACK loss. Their consumer may still be fetching the original frozen input.
	if err != nil {
		return nil, errors.Join(ErrColdConfigurationUnknown, err)
	}
	if receipt.RequestID != requestID || receipt.InputSHA256 != inputSHA || receipt.PayloadSHA256 != cold.PayloadSHA256 || receipt.NativeIdentity != target.NativeIdentity || receipt.WorkspaceDir != target.WorkspaceDir || receipt.State != "started" {
		return nil, ErrColdConfigurationUnknown
	}
	return m.finishConfiguredResume(ctx, id, spec, target)
}

func (m *Manager) finishConfiguredResume(ctx context.Context, id string, spec Spec, target *GWColdStartTarget) (*Sandbox, error) {
	sb, err := m.WaitForRunning(ctx, id)
	if err != nil {
		return nil, err
	}
	sb.Password = spec.Env[BridgePasswordEnv]
	sb.WorkspaceDir = target.WorkspaceDir
	sb.ConfigRoot = spec.ConfigRoot
	return sb, nil
}
