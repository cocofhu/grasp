package sandbox

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/config"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/ssh"
)

// containerPrefix labels approving-managed sandboxes so they can be listed and
// reconciled apart from any other tenants sharing the gateway.
const containerPrefix = "grasp-sb-"

// managedByLabel/managedByValue tag every sandbox approving creates on the
// gateway, so List/reconcile only ever touch our own sandboxes.
const (
	managedByLabel = "grasp.managed"
	managedByValue = "1"
	cfNameLabel    = "grasp.name" // client correlation id (pre-allocated)
)

// NewContainerName returns a fresh, unique correlation id used as a placeholder
// DB name (and a gateway label) before the gateway assigns the real sandbox id.
func NewContainerName() string {
	return containerPrefix + uuid.NewString()[:8]
}

// sharedSSHKey lazily mints one process-wide ed25519 keypair used for all
// sandbox data-plane SSH. Every sandbox gets the same public key injected
// (SSH_KEY env), so any Manager instance can reach any approving sandbox
// regardless of which one created it.
var (
	sshKeyOnce sync.Once
	sshSigner  ssh.Signer
	sshAuthKey string
	sshKeyErr  error
)

func sharedSSHKey() (ssh.Signer, string, error) {
	sshKeyOnce.Do(func() { sshSigner, sshAuthKey, sshKeyErr = generateSSHKey() })
	return sshSigner, sshAuthKey, sshKeyErr
}

// Manager creates and destroys sandboxes through the sandbox-gateway control
// plane and reaches them over SSH for data-plane operations (exec/files/
// terminal). It no longer talks to Docker directly — the gateway owns the
// container/pod lifecycle and image.
type Manager struct {
	gw           *GatewayClient
	Image        string // optional image override; empty = gateway default
	WorkspaceDir string // default WORKSPACE_DIR inside the sandbox
	SSHUser      string // ssh login user (default root)
	SSHPassword  string // optional ssh password fallback (key auth is primary)

	// installHelpers, when set, seeds the artifact-upload CLI into each new
	// sandbox over SSH (the universal image ships no such CLI). Off by default
	// so tests that create sandboxes never block on real SSH.
	installHelpers bool

	// bundles serves short-lived ConfigHome .tgz for gateway config.bundleUrl.
	bundles *BundleStore
	// injectAdvertiseFallback is the boot-time MCP/inject base URL; live
	// config.ResolveMCPAdvertise is preferred at Create time.
	injectAdvertiseFallback string

	// createTimeout bounds how long Create waits for the gateway to report a
	// running sandbox with a resolved session endpoint.
	createTimeout time.Duration

	// blobs resolves blob:{id} attachments when opening ACP clients.
	blobs blob.Store

	// hostKeys holds per-endpoint TOFU SSH host keys for data-plane dials.
	hostKeys *hostKeyCache

	// runtime is the sandbox runtime bundle every new sandbox fetches at
	// start (GRASP_RUNTIME_URL) and running sandboxes get via EnsureRuntime.
	// Nil only in tests that do not exercise the bundle.
	runtime *RuntimeBundle
}

// ManagerOptions configures a gateway-backed Manager.
type ManagerOptions struct {
	Image        string
	WorkspaceDir string
	SSHUser      string
	SSHPassword  string
	// InstallHelpers seeds sandbox-side helper CLIs (artifact-upload) over SSH
	// on Create. Production wiring enables it; tests leave it off.
	InstallHelpers bool
	// InjectStore holds ConfigHome .tgz blobs referenced by config.bundleUrl.
	InjectStore *BundleStore
	// InjectAdvertise is the sandbox-reachable base URL for /sandbox-inject/*
	// (usually server.mcp_advertise). Empty → ResolveMCPAdvertise at Create.
	InjectAdvertise string
	// CreateTimeout bounds how long Create waits for the gateway to report a
	// running sandbox with a resolved session endpoint. Must be >= the gateway's
	// own FinalizeTimeout, otherwise approving gives up before the gateway
	// finishes provisioning (large cold-start image pull + PVC attach + boot).
	// 0 → default (see NewManager).
	CreateTimeout time.Duration
	// Blobs resolves blob:{id} attachments for ACP chat turns.
	Blobs blob.Store
	// Runtime is the sandbox runtime bundle; the image cannot start without it.
	Runtime *RuntimeBundle
}

// Spec describes one sandbox to create. The sandbox is a generic agent runner:
// it takes a repo clone list (GIT_REPOS in Env) plus environment variables and
// leaves integration-specific behavior (git credentials, push, MR) to the image.
type Spec struct {
	// Name is an optional caller-allocated correlation id (see NewContainerName)
	// recorded as a gateway label; the authoritative handle is the gateway id.
	Name string
	// Image overrides Manager.Image for this create (per-acpBackend images).
	Image string
	// Env vars injected into the sandbox. The repo clone list rides here as
	// GIT_REPOS (comma-separated name|url|branch). See EncodeRepos / RepoSpec.
	Env    map[string]string
	Mounts []string // extra docker "-v host:container[:ro]" specs (docker driver)
	// Labels are extra gateway labels for discovery/reconciliation.
	Labels map[string]string
	// ConfigHome, when set, is a host dir of rules/skills/mcp.json published as
	// a .tgz and injected via gateway config.bundleUrl before sandbox start.
	ConfigHome string
	// ConfigRoot is the in-sandbox path for ConfigHome (default /root/.cursor).
	ConfigRoot string
	// WorkspaceDir overrides WORKSPACE_DIR for this sandbox.
	WorkspaceDir string
	// Resources are optional per-sandbox CPU/memory/disk limits for the gateway.
	Resources *GWResources
	// SSHPrivateKey / SSHKnownHosts are optional literals injected as files
	// under /tmp/grasp-ssh-inject before git clone (not via ordinary env).
	// Empty fields are omitted (do not create/clear the corresponding file).
	SSHPrivateKey string
	SSHKnownHosts string
}

// SSHInjectStagingDir is the in-sandbox destination for the SSH file inject
// bundle. startup.sh applies these files to ~/.ssh before configure_git_credentials.
const SSHInjectStagingDir = "/tmp/grasp-ssh-inject"

// RepoSpec is one repository cloned into the sandbox at <workspace>/<Name>/.
type RepoSpec struct {
	Name   string
	URL    string
	Branch string
}

// RepoNameFromURL derives a workspace subdir name from a clone URL: the last
// path segment without a trailing ".git". Empty when it can't be derived.
func RepoNameFromURL(raw string) string {
	raw = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(raw), "/"))
	if raw == "" {
		return ""
	}
	seg := raw
	if i := strings.LastIndexAny(seg, "/:"); i >= 0 {
		seg = seg[i+1:]
	}
	return strings.TrimSpace(strings.TrimSuffix(seg, ".git"))
}

// ReposFromURL builds a single-entry repo list from one clone URL.
func ReposFromURL(url string) []RepoSpec {
	url = strings.TrimSpace(url)
	if url == "" {
		return nil
	}
	name := RepoNameFromURL(url)
	if name == "" {
		name = "repo"
	}
	return []RepoSpec{{Name: name, URL: url}}
}

// EncodeRepos serializes repos into the GIT_REPOS env var consumed by the
// sandbox startup script: comma-separated "name|url|branch" entries.
func EncodeRepos(repos []RepoSpec) string {
	parts := make([]string, 0, len(repos))
	for _, r := range repos {
		name := strings.TrimSpace(r.Name)
		url := strings.TrimSpace(r.URL)
		if name == "" || url == "" {
			continue
		}
		entry := name + "|" + url
		if b := strings.TrimSpace(r.Branch); b != "" {
			entry += "|" + b
		}
		parts = append(parts, entry)
	}
	return strings.Join(parts, ",")
}

// DecodeRepos parses the GIT_REPOS wire format (comma-separated
// "name|url|branch" entries) produced by EncodeRepos. Entries missing a URL,
// with an unsafe name, or with fewer than two '|' fields are skipped. A blank
// name is derived from the URL when possible.
func DecodeRepos(s string) []RepoSpec {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]RepoSpec, 0, len(parts))
	seen := map[string]bool{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		fields := strings.SplitN(part, "|", 3)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimSpace(fields[0])
		url := strings.TrimSpace(fields[1])
		branch := ""
		if len(fields) >= 3 {
			branch = strings.TrimSpace(fields[2])
		}
		if name == "" {
			name = RepoNameFromURL(url)
		}
		if name == "" || url == "" || seen[name] {
			continue
		}
		// Same escape guard as runtime.safeRepoName / startup.sh.
		if name == "." || name == ".." || strings.ContainsAny(name, "/\\") {
			continue
		}
		seen[name] = true
		out = append(out, RepoSpec{Name: name, URL: url, Branch: branch})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Sandbox is a running sandbox handle. Host/Port point at the ACP session
// endpoint (8765) the gateway published; SSHHost/SSHPort at the sshd endpoint
// (22) used for the data plane.
type Sandbox struct {
	ID             string // gateway sandbox id (authoritative handle)
	Name           string // == ID (kept for API/DB compatibility)
	Host           string // session endpoint host
	Port           int    // session endpoint port (ACP bridge, 8765)
	CodeServerPort int    // published code-server (ide) port (0 if none)
	SSHHost        string // ssh endpoint host
	SSHPort        int    // ssh endpoint port
	WorkspaceDir   string // resolved WORKSPACE_DIR inside the sandbox
	ConfigRoot     string // in-sandbox agent config root (e.g. /root/.cursor)
	// Password is the sandbox token the acp-bridge (8765) requires
	// (ACP_BRIDGE_PASSWORD). ACP() attaches it so the platform's client logs in
	// before dialing /ws.
	Password string
	// Blobs resolves Ref-backed prompt images before they are sent to the bridge.
	Blobs blob.Store
	mgr   *Manager
	// hostKeys is used when mgr is nil (detached test handles) so TOFU state
	// survives across dials on the same Sandbox.
	hostKeys *hostKeyCache
}

// NewManager builds a gateway-backed manager.
func NewManager(gw *GatewayClient, opts ManagerOptions) *Manager {
	ws := opts.WorkspaceDir
	if ws == "" {
		ws = "/root/workspace"
	}
	// Default sized for cold-start: large image pull + PVC attach + git clone +
	// boot before the gateway's session-port probe succeeds. Must stay >= the
	// gateway's FinalizeTimeout.
	createTimeout := opts.CreateTimeout
	if createTimeout <= 0 {
		createTimeout = 20 * time.Minute
	}
	return &Manager{
		gw:                      gw,
		Image:                   strings.TrimSpace(opts.Image),
		WorkspaceDir:            ws,
		SSHUser:                 opts.SSHUser,
		SSHPassword:             opts.SSHPassword,
		installHelpers:          opts.InstallHelpers,
		bundles:                 opts.InjectStore,
		injectAdvertiseFallback: strings.TrimSpace(opts.InjectAdvertise),
		createTimeout:           createTimeout,
		blobs:                   opts.Blobs,
		hostKeys:                newHostKeyCache(),
		runtime:                 opts.Runtime,
	}
}

// Runtime returns the sandbox runtime bundle (nil when not configured).
func (m *Manager) Runtime() *RuntimeBundle { return m.runtime }

// Gateway exposes the underlying REST client (for host/port resolution etc.).
func (m *Manager) Gateway() *GatewayClient { return m.gw }

func (m *Manager) creds(host string, port int) sshCreds {
	signer, _, _ := sharedSSHKey()
	user := m.SSHUser
	if user == "" {
		user = "root"
	}
	keys := m.hostKeys
	if keys == nil {
		keys = newHostKeyCache()
		m.hostKeys = keys
	}
	return sshCreds{host: host, port: port, signer: signer, password: m.SSHPassword, user: user, hostKeys: keys}
}

// sandboxFromGW maps a gateway record's endpoints onto a Sandbox handle.
func (m *Manager) sandboxFromGW(sb *GWSandbox, workspaceDir string) *Sandbox {
	sh, sp := hostPortOf(sb.Endpoint("session"))
	_, ide := hostPortOf(sb.Endpoint("ide"))
	sshHost, sshPort := hostPortOf(sb.Endpoint("ssh"))
	if sshHost == "" {
		sshHost = sh
	}
	if workspaceDir == "" {
		workspaceDir = m.WorkspaceDir
	}
	return &Sandbox{
		ID: sb.ID, Name: sb.ID, Host: sh, Port: sp, CodeServerPort: ide,
		SSHHost: sshHost, SSHPort: sshPort, WorkspaceDir: workspaceDir, mgr: m, Blobs: m.blobs,
	}
}

// Create provisions a sandbox via the gateway, waits until it is running with a
// resolved session endpoint, and returns the handle. The caller should
// WaitForACPReady before connecting the ACP client.
func (m *Manager) Create(ctx context.Context, spec Spec) (*Sandbox, error) {
	if m.gw == nil {
		return nil, fmt.Errorf("sandbox manager has no gateway client configured")
	}
	workspaceDir := m.WorkspaceDir
	if spec.WorkspaceDir != "" {
		workspaceDir = spec.WorkspaceDir
	}
	configRoot := spec.ConfigRoot
	if configRoot == "" {
		configRoot = "/root/.cursor"
	}

	env := map[string]string{}
	for k, v := range spec.Env {
		env[k] = v
	}
	bridgePassword := strings.TrimSpace(env[BridgePasswordEnv])
	if bridgePassword == "" {
		return nil, fmt.Errorf("sandbox spec missing %s", BridgePasswordEnv)
	}
	// Inner Docker (DinD): default SKIP_INNER_DOCKER=0 so startup.sh starts
	// dockerd. Callers may set SKIP_INNER_DOCKER=1 to skip (faster boot).
	if _, ok := env["SKIP_INNER_DOCKER"]; !ok {
		env["SKIP_INNER_DOCKER"] = "0"
	}
	// Inject the shared public key so we can reach the sandbox over SSH.
	if _, authKey, err := sharedSSHKey(); err == nil && authKey != "" {
		if existing := strings.TrimSpace(env["SSH_KEY"]); existing == "" {
			env["SSH_KEY"] = authKey
		} else {
			env["SSH_KEY"] = existing + "\n" + authKey
		}
	} else if err != nil {
		return nil, fmt.Errorf("prepare sandbox ssh key: %w", err)
	}

	labels := map[string]string{managedByLabel: managedByValue}
	if spec.Name != "" {
		labels[cfNameLabel] = spec.Name
	}
	for k, v := range spec.Labels {
		labels[k] = v
	}

	image := strings.TrimSpace(spec.Image)
	if image == "" {
		image = m.Image
	}
	req := GWCreateRequest{
		Image:        image,
		Env:          env,
		Labels:       labels,
		WorkspaceDir: workspaceDir,
		Mounts:       spec.Mounts,
		Resources:    withDefaultMemory(spec.Resources),
	}
	var runtimeData []byte
	var runtimeMan RuntimeManifest
	if m.runtime != nil {
		data, man, err := m.runtime.Current()
		if err != nil {
			return nil, err
		}
		runtimeData, runtimeMan = data, man
	}
	// Pre-start inject: pack SSH staging (+ ConfigHome) into SANDBOX_INJECT and
	// the runtime bundle into GRASP_RUNTIME_URL. SSH staging must land before
	// git clone; startup.sh runs inject first. All share one auth token.
	var bundleIDs []string
	if inj := m.buildMultiInject(spec.ConfigHome, configRoot, spec.SSHPrivateKey, spec.SSHKnownHosts, runtimeData); inj.ok() {
		if len(inj.parts) > 0 {
			env["SANDBOX_INJECT"] = strings.Join(inj.parts, ",")
			log.Info().Str("sandbox_inject", env["SANDBOX_INJECT"]).
				Bool("ssh", strings.TrimSpace(spec.SSHPrivateKey) != "" || strings.TrimSpace(spec.SSHKnownHosts) != "").
				Msg("sandbox config+ssh inject via SANDBOX_INJECT")
		}
		if inj.runtimeURL != "" {
			env["GRASP_RUNTIME_URL"] = inj.runtimeURL
		}
		env["SANDBOX_INJECT_HEADERS"] = inj.headers
		req.Env = env
		bundleIDs = inj.ids
	} else if runtimeData != nil {
		return nil, fmt.Errorf("sandbox runtime bundle needs the inject store and a sandbox-reachable server URL (server.mcp_advertise)")
	} else if cfg := m.buildInjectConfig(spec.ConfigHome, configRoot); cfg != nil {
		// Fallback single ConfigHome inject (no SSH).
		req.Config = cfg
		if id := bundleIDFromURL(cfg.BundleURL); id != "" {
			bundleIDs = []string{id}
		}
		log.Info().Str("bundleUrl", cfg.BundleURL).Str("configRoot", configRoot).
			Msg("sandbox config inject via bundleUrl")
	}

	repoCount := 0
	if r := strings.TrimSpace(env["GIT_REPOS"]); r != "" {
		repoCount = strings.Count(r, ",") + 1
	}
	created, err := m.gw.Create(ctx, req)
	if err != nil {
		m.deleteInjectBundles(bundleIDs)
		return nil, fmt.Errorf("gateway create: %w", err)
	}
	log.Info().Str("id", created.ID).Str("cf_name", spec.Name).
		Int("repos", repoCount).Int("env_vars", len(env)).Msg("sandbox create (gateway)")

	running, err := m.gw.WaitRunning(ctx, created.ID, m.createTimeout)
	if err != nil {
		_ = m.gw.Destroy(context.Background(), created.ID)
		m.deleteInjectBundles(bundleIDs)
		return nil, fmt.Errorf("sandbox not ready: %w", err)
	}
	sb := m.sandboxFromGW(running, workspaceDir)
	sb.ConfigRoot = configRoot
	if runtimeData != nil {
		m.runtime.noteInstalled(sb.ID, runtimeMan.Version)
	}
	// Carry the acp-bridge secret so ACP() / WaitForACPReady can log in.
	sb.Password = bridgePassword
	// Do NOT Delete inject bundles here. WaitRunning can return before
	// startup.sh finishes SANDBOX_INJECT fetch; early Delete → 401 and no
	// mcp.json. Images without sshd also cannot SSH-heal. Bundles expire via
	// DefaultInjectBundleTTL instead.
	_ = bundleIDs
	// If bundleUrl inject was skipped or failed in-image, SSH-sync as heal
	// (no-op when the sandbox image has no sshd).
	if spec.ConfigHome != "" && !m.configHomePresent(ctx, sb, configRoot) {
		m.EnsureConfigHome(ctx, sb, spec.ConfigHome, configRoot)
	}
	m.EnsureHelpers(ctx, sb)
	return sb, nil
}

// deleteInjectBundles removes all pre-start inject bundle ids on Create failure.
func (m *Manager) deleteInjectBundles(ids []string) {
	if m == nil || m.bundles == nil {
		return
	}
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			continue
		}
		m.bundles.Delete(id)
	}
}

func (m *Manager) configHomePresent(ctx context.Context, sb *Sandbox, configRoot string) bool {
	if m == nil || !m.installHelpers || sb == nil || configRoot == "" {
		// Unit tests without InstallHelpers skip the SSH probe.
		return m == nil || !m.installHelpers
	}
	cmd, err := newSafeCmd("test", "-f", configRoot+"/mcp.json")
	if err != nil {
		return false
	}
	_, err = sb.creds().run(ctx, 15*time.Second, cmd)
	return err == nil
}

// buildInjectConfig packs ConfigHome and returns gateway config.bundleUrl inject.
func (m *Manager) buildInjectConfig(hostDir, configRoot string) *GWConfigInject {
	hostDir = strings.TrimSpace(hostDir)
	if hostDir == "" || m.bundles == nil {
		return nil
	}
	base := strings.TrimRight(config.ResolveMCPAdvertise(m.injectAdvertiseFallback), "/")
	if base == "" {
		return nil
	}
	data, err := PackConfigHomeTarGz(hostDir)
	if err != nil {
		log.Warn().Err(err).Str("dir", hostDir).Msg("pack config-home inject bundle failed")
		return nil
	}
	id, token := m.bundles.Put(data, DefaultInjectBundleTTL)
	if id == "" || token == "" {
		return nil
	}
	return &GWConfigInject{
		ConfigRoot: configRoot,
		BundleURL:  base + "/sandbox-inject/" + id + ".tgz",
		Headers:    "Authorization: Bearer " + token,
	}
}

// injectPlan is the pre-start inject for one sandbox: SANDBOX_INJECT parts
// (SSH staging first, then ConfigHome), the runtime bundle URL and the shared
// Authorization header.
type injectPlan struct {
	parts      []string
	runtimeURL string
	headers    string
	ids        []string
}

func (p injectPlan) ok() bool { return len(p.parts) > 0 || p.runtimeURL != "" }

// buildMultiInject registers the optional SSH staging, ConfigHome and runtime
// bundles under one bearer token. The zero plan means nothing to inject (or no
// inject store / reachable base URL).
func (m *Manager) buildMultiInject(configHome, configRoot, sshKey, sshHosts string, runtime []byte) injectPlan {
	if m.bundles == nil {
		return injectPlan{}
	}
	base := strings.TrimRight(config.ResolveMCPAdvertise(m.injectAdvertiseFallback), "/")
	if base == "" {
		return injectPlan{}
	}
	token := randomHex(24)
	if token == "" {
		return injectPlan{}
	}
	var plan injectPlan
	put := func(data []byte) string {
		id := m.bundles.PutWithToken(data, DefaultInjectBundleTTL, token)
		if id == "" {
			return ""
		}
		plan.ids = append(plan.ids, id)
		return base + "/sandbox-inject/" + id + ".tgz"
	}
	if sshData, err := PackSSHInjectTarGz(sshKey, sshHosts); err != nil {
		log.Warn().Err(err).Msg("pack ssh inject bundle failed")
	} else if sshData != nil {
		u := put(sshData)
		if u == "" {
			return injectPlan{}
		}
		plan.parts = append(plan.parts, u+"|"+SSHInjectStagingDir)
	}
	if strings.TrimSpace(configHome) != "" {
		data, err := PackConfigHomeTarGz(configHome)
		if err != nil {
			log.Warn().Err(err).Str("dir", configHome).Msg("pack config-home inject bundle failed")
		} else if data != nil {
			dest := configRoot
			if dest == "" {
				dest = "/root/.cursor"
			}
			u := put(data)
			if u == "" {
				return injectPlan{}
			}
			plan.parts = append(plan.parts, u+"|"+dest)
		}
	}
	if runtime != nil {
		if plan.runtimeURL = put(runtime); plan.runtimeURL == "" {
			return injectPlan{}
		}
	}
	if !plan.ok() {
		return injectPlan{}
	}
	plan.headers = "Authorization: Bearer " + token
	return plan
}

func bundleIDFromURL(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return ""
	}
	i := strings.LastIndex(u, "/")
	if i < 0 {
		return ""
	}
	return strings.TrimSuffix(u[i+1:], ".tgz")
}

// Attach rebuilds a Sandbox handle for an existing gateway sandbox by id.
func (m *Manager) Attach(ctx context.Context, id string) (*Sandbox, error) {
	if m.gw == nil {
		return nil, fmt.Errorf("no gateway client")
	}
	sb, err := m.gw.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if sb.Endpoint("session") == "" {
		return nil, fmt.Errorf("sandbox %s has no session endpoint (status=%s)", id, sb.Status)
	}
	out := m.sandboxFromGW(sb, m.WorkspaceDir)
	m.ensureRuntimeAsync(out)
	return out, nil
}

// DestroyByName removes a sandbox by gateway id (best effort).
func (m *Manager) DestroyByName(ctx context.Context, id string) error {
	if m.gw == nil || strings.TrimSpace(id) == "" {
		return nil
	}
	m.runtime.forget(id)
	return m.gw.Destroy(ctx, id)
}

// Logs fetches PID1 combined stdout/stderr via the gateway control plane.
// id is the gateway sandbox id (also stored as Sandbox.Name). Empty content
// with a nil error means a successful read with no output yet.
func (m *Manager) Logs(ctx context.Context, id string, tail int) (string, error) {
	if m.gw == nil {
		return "", fmt.Errorf("sandbox manager has no gateway client configured")
	}
	if strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("sandbox id required")
	}
	if tail <= 0 {
		tail = 5000
	}
	return m.gw.Logs(ctx, id, tail)
}

// normalizeGWStatus maps gateway/driver status to the docker-like vocabulary the
// rest of the server expects ("running"/"exited"/"not_found"/…).
func normalizeGWStatus(s string) string {
	switch s {
	case "running":
		return "running"
	case "stopped":
		return "exited"
	case "":
		return "not_found"
	default:
		return s
	}
}

// Status returns "running", "exited", "creating", "not_found", … for a sandbox.
func (m *Manager) Status(ctx context.Context, id string) string {
	if m.gw == nil || strings.TrimSpace(id) == "" {
		return "not_found"
	}
	st, err := m.gw.LiveStatus(ctx, id)
	if err != nil {
		return "not_found"
	}
	return normalizeGWStatus(st)
}

// managedLabelFilter is the gateway ?label= filter for approving sandboxes.
func managedLabelFilter() string { return managedByLabel + ":" + managedByValue }

// CorrelationNameKey is the label that binds a gateway sandbox to the
// pre-allocated local placeholder name (see NewContainerName / Spec.Name).
func CorrelationNameKey() string { return cfNameLabel }

// List returns the gateway ids of all approving-managed sandboxes.
func (m *Manager) List(ctx context.Context) ([]string, error) {
	all, err := m.ListManaged(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(all))
	for i := range all {
		ids = append(ids, all[i].ID)
	}
	return ids, nil
}

// ListManaged returns approving-managed gateway sandboxes (id/status/labels).
func (m *Manager) ListManaged(ctx context.Context) ([]GWSandbox, error) {
	if m.gw == nil {
		return nil, nil
	}
	return m.gw.List(ctx, managedLabelFilter())
}

// ListStatuses returns an id→status map for all approving-managed sandboxes.
func (m *Manager) ListStatuses(ctx context.Context) (map[string]string, error) {
	all, err := m.ListManaged(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(all))
	for i := range all {
		out[all[i].ID] = normalizeGWStatus(all[i].Status)
	}
	return out, nil
}

// DestroyByCorrelationName destroys every managed gateway sandbox whose
// grasp.name label equals corr (best effort). Used when a local row still
// carries the pre-adopt placeholder name so Status/DestroyByName cannot address
// the real gateway id.
func (m *Manager) DestroyByCorrelationName(ctx context.Context, corr string) {
	corr = strings.TrimSpace(corr)
	if m.gw == nil || corr == "" {
		return
	}
	all, err := m.ListManaged(ctx)
	if err != nil {
		return
	}
	for i := range all {
		if all[i].Labels[cfNameLabel] == corr {
			_ = m.DestroyByName(ctx, all[i].ID)
		}
	}
}

// ExecPTY opens an interactive PTY shell on the sandbox over SSH. Caller must
// Close the returned terminal.
func (m *Manager) ExecPTY(ctx context.Context, id string, command []string) (*SSHTerminal, error) {
	sb, err := m.Attach(ctx, id)
	if err != nil {
		return nil, err
	}
	return sb.mgr.creds(sb.SSHHost, sb.SSHPort).openTerminal(ctx, command)
}

// ContainerIP returns the reachable host of the sandbox's session endpoint.
// (Under the docker driver this is the loopback bind address; app ports are
// resolved separately via HostForPort.)
func (m *Manager) ContainerIP(ctx context.Context, id string) (string, error) {
	sb, err := m.gw.Get(ctx, id)
	if err != nil {
		return "", err
	}
	host, _ := hostPortOf(sb.Endpoint("session"))
	if host == "" {
		return "", fmt.Errorf("no reachable host for sandbox %s", id)
	}
	return host, nil
}

// HostForPort resolves the reachable "host:port" address for an in-sandbox
// container port (e.g. an app's 3000/5173) via the gateway. Empty when absent.
func (m *Manager) HostForPort(ctx context.Context, id string, port int) (string, error) {
	if m.gw == nil {
		return "", fmt.Errorf("no gateway client")
	}
	sb, err := m.gw.Get(ctx, id)
	if err != nil {
		return "", err
	}
	if addr := sb.Endpoint(fmt.Sprintf("%d", port)); addr != "" {
		return addr, nil
	}
	// Fall back to the gateway host-resolution endpoint for ports not yet in
	// the cached endpoints map.
	var out struct {
		Address string `json:"address"`
	}
	if err := m.gw.do(ctx, "GET", fmt.Sprintf("/api/v1/sandboxes/%s/hosts/%d", id, port), nil, &out); err != nil {
		return "", err
	}
	return out.Address, nil
}

// PublishPort asks the gateway to expose an extra public port (k8s Service mapping).
func (m *Manager) PublishPort(ctx context.Context, id string, port int) (string, error) {
	if m.gw == nil {
		return "", fmt.Errorf("no gateway client")
	}
	if strings.TrimSpace(id) == "" || port <= 0 {
		return "", fmt.Errorf("invalid sandbox or port")
	}
	var out struct {
		Address string `json:"address"`
	}
	body := map[string]int{"port": port}
	if err := m.gw.do(ctx, "POST", fmt.Sprintf("/api/v1/sandboxes/%s/ports", id), body, &out); err != nil {
		return "", err
	}
	return out.Address, nil
}

// EndpointAddr returns the gateway-published reachable "host:port" for a named
// endpoint key (session / ide / ssh / …). Prefer this for IDE/ACP reverse
// proxies so dial targets track gateway endpoints rather than hard-coded
// 127.0.0.1:<persistedPort>.
func (m *Manager) EndpointAddr(ctx context.Context, id, key string) (string, error) {
	if m.gw == nil {
		return "", fmt.Errorf("no gateway client")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", fmt.Errorf("empty endpoint key")
	}
	sb, err := m.gw.Get(ctx, id)
	if err != nil {
		return "", err
	}
	addr := strings.TrimSpace(sb.Endpoint(key))
	if addr == "" {
		return "", fmt.Errorf("sandbox %s has no %q endpoint", id, key)
	}
	return addr, nil
}

// Exec runs a command on a named sandbox over SSH and returns combined output.
// Each argv fragment must pass validateShellArg (via newSafeCmd).
func (m *Manager) Exec(ctx context.Context, id string, timeout time.Duration, cmd ...string) (string, error) {
	sb, err := m.Attach(ctx, id)
	if err != nil {
		return "", err
	}
	return sb.Exec(ctx, timeout, cmd...)
}

// ExecScript runs interpreter with script on stdin (argv is only the
// interpreter and "-s"). Use this instead of embedding shell scripts in Exec
// argv so metacharacters never reach Session.Start/Run.
func (m *Manager) ExecScript(ctx context.Context, id string, timeout time.Duration, interpreter, script string) (string, error) {
	sb, err := m.Attach(ctx, id)
	if err != nil {
		return "", err
	}
	return sb.ExecScript(ctx, timeout, interpreter, script)
}

// --- Sandbox data-plane (over SSH) -------------------------------------------

func (s *Sandbox) workspaceDir() string {
	if s.WorkspaceDir != "" {
		return s.WorkspaceDir
	}
	if s.mgr != nil {
		return s.mgr.WorkspaceDir
	}
	return ""
}

func (s *Sandbox) creds() sshCreds {
	if s.mgr != nil {
		return s.mgr.creds(s.SSHHost, s.SSHPort)
	}
	// Detached handle (e.g. a test-injected sandbox): fall back to the shared
	// key and the default login user. The signer is unused when execHook is set.
	signer, _, _ := sharedSSHKey()
	if s.hostKeys == nil {
		s.hostKeys = newHostKeyCache()
	}
	return sshCreds{host: s.SSHHost, port: s.SSHPort, signer: signer, user: "root", hostKeys: s.hostKeys}
}

func (s *Sandbox) resolvePath(path string) string {
	if strings.HasPrefix(path, "/") {
		return path
	}
	return strings.TrimRight(s.workspaceDir(), "/") + "/" + path
}

// ReadFile reads a file from inside the sandbox (relative paths resolve against
// WORKSPACE_DIR).
func (s *Sandbox) ReadFile(ctx context.Context, path string) ([]byte, error) {
	cmd, err := newSafeCmd("cat", "--", s.resolvePath(path))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	out, err := s.creds().run(ctx, 20*time.Second, cmd)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return out, nil
}

// WriteFile writes content to a path inside the sandbox, creating parent dirs.
func (s *Sandbox) WriteFile(ctx context.Context, path string, content []byte) error {
	resolved := s.resolvePath(path)
	dir := resolved
	if i := strings.LastIndex(resolved, "/"); i > 0 {
		dir = resolved[:i]
	}
	mkdirCmd, err := newSafeCmd("mkdir", "-p", dir)
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if out, err := s.creds().run(ctx, 20*time.Second, mkdirCmd); err != nil {
		return fmt.Errorf("write %s: mkdir: %w: %s", path, err, strings.TrimSpace(string(out)))
	}
	teeCmd, err := newSafeCmd("tee", resolved)
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	out, err := s.creds().runInput(ctx, 20*time.Second, teeCmd, strings.NewReader(string(content)))
	if err != nil {
		return fmt.Errorf("write %s: %w: %s", path, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// FileExists reports whether a path exists inside the sandbox.
func (s *Sandbox) FileExists(ctx context.Context, path string) bool {
	cmd, err := newSafeCmd("test", "-e", s.resolvePath(path))
	if err != nil {
		return false
	}
	_, err = s.creds().run(ctx, 10*time.Second, cmd)
	return err == nil
}

// Exec runs an arbitrary command inside the sandbox over SSH.
// Each argv fragment must pass validateShellArg; shell scripts belong in ExecScript.
func (s *Sandbox) Exec(ctx context.Context, timeout time.Duration, cmd ...string) (string, error) {
	sc, err := newSafeCmd(cmd...)
	if err != nil {
		return "", err
	}
	out, err := s.creds().run(ctx, timeout, sc)
	return strings.TrimSpace(string(out)), err
}

// ExecScript runs interpreter -s with script on stdin so script body never
// appears on the Session.Start/Run command line (CodeQL #11).
func (s *Sandbox) ExecScript(ctx context.Context, timeout time.Duration, interpreter, script string) (string, error) {
	sc, err := newSafeCmd(interpreter, "-s")
	if err != nil {
		return "", err
	}
	out, err := s.creds().runInput(ctx, timeout, sc, strings.NewReader(script))
	return strings.TrimSpace(string(out)), err
}

// Destroy removes the sandbox via the gateway (best effort).
func (s *Sandbox) Destroy(ctx context.Context) {
	if s == nil || s.mgr == nil {
		return
	}
	if err := s.mgr.DestroyByName(ctx, s.ID); err != nil {
		log.Warn().Err(err).Str("id", s.ID).Msg("sandbox destroy failed")
	}
}

// ACP returns a new ACP client wired to this sandbox's session endpoint,
// pre-authenticated with the sandbox token when the bridge requires it.
func (s *Sandbox) ACP() *ACPClient {
	c := NewACPClient(s.Host, s.Port).WithPassword(s.Password)
	if s.Blobs != nil {
		c = c.WithBlobs(s.Blobs)
	}
	return c
}

// shellArgPattern is the allowlist for remote shell argv fragments (CodeQL #11).
// Values outside this set never reach sess.Start/Run via safeCmd.render.
var shellArgPattern = regexp.MustCompile(`^[A-Za-z0-9_./:@%+=,-]+$`)

func validateShellArg(s string) error {
	if s == "" {
		return fmt.Errorf("empty shell argument")
	}
	if strings.ContainsRune(s, 0) {
		return fmt.Errorf("shell argument contains NUL")
	}
	if strings.Contains(s, "..") {
		return fmt.Errorf("shell argument must not contain '..'")
	}
	if !shellArgPattern.MatchString(s) {
		return fmt.Errorf("shell argument contains disallowed characters")
	}
	return nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
