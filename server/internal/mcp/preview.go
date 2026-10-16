package mcp

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	PreviewKindPort = "port"
	PreviewKindURL  = "url"
)

// PreviewPort is a registered preview endpoint of an Agent node (set_preview).
type PreviewPort struct {
	RunID       string `json:"runId"`
	NodeID      string `json:"nodeId"`
	Kind        string `json:"kind,omitempty"`
	ItemKey     string `json:"-"`
	Port        int    `json:"port"`
	URL         string `json:"url,omitempty"`
	Label       string `json:"label,omitempty"`
	ProxyURL    string `json:"proxyUrl"`
	SandboxName string `json:"-"`
	// Host is the resolved upstream base the proxy dials (e.g.
	// "http://172.17.0.5:9090"), persisted so the proxy needn't re-resolve the
	// container IP through the sandbox manager on every request.
	Host string `json:"-"`
	// Mode is "direct" for an IP-direct preview; empty/vnc otherwise.
	Mode string `json:"mode,omitempty"`
	// DirectURL is the browser-facing http://IP:port/ when Mode=direct.
	DirectURL string `json:"directUrl,omitempty"`
	// Healthy is true when ProbeHTTPPort succeeded at registration time.
	// set_preview only succeeds when Healthy; HasHealthyPreviewPorts gates
	// production-phase completion / early park into review.
	Healthy bool `json:"healthy"`
	// KeepalivePID is the setsid-detached listener pid (0 when unknown).
	KeepalivePID int       `json:"keepalivePid,omitempty"`
	RegisteredAt time.Time `json:"registeredAt"`
}

// PreviewStore persists preview port registrations.
type PreviewStore interface {
	UpsertPreviewPort(rec PreviewPort) error
	ListPreviewPorts(runID, nodeID string) ([]PreviewPort, error)
	GetPreviewPort(runID, nodeID string, port int) (*PreviewPort, bool)
	UpdatePreviewHealth(runID, nodeID string, port int, healthy bool) error
}

// PreviewSandboxOps resolves run sandboxes and probes in-container HTTP ports.
type PreviewSandboxOps interface {
	SandboxForRunNode(runID, nodeID string) (name string, ok bool)
	ProbeHTTPPort(ctx context.Context, sandboxName string, port int) bool
	// KeepalivePort detaches the listener (setsid + pidfile + log) and returns
	// the keepalive pid (0 when unknown / no-op).
	KeepalivePort(ctx context.Context, sandboxName string, port int) (pid int, err error)
	// PreviewUpstream resolves the reachable base URL (http://host:port) for an
	// in-sandbox app port via the gateway, so the registration can persist an
	// upstream host for the proxy to dial.
	PreviewUpstream(ctx context.Context, sandboxName string, port int) (string, bool)
}

// previewDirectChecker is optionally implemented by PreviewSandboxOps.
type previewDirectChecker interface {
	DirectPreview(runID, nodeID string) bool
}

// previewListenInspector is optionally implemented to report which local
// addresses hold port inside the sandbox, so an unreachable port can say why.
type previewListenInspector interface {
	ListenAddrs(ctx context.Context, sandboxName string, port int) ([]string, error)
}

// Probe retries cover an app that is still binding when the agent calls set_preview.
const previewProbeAttempts = 5

var previewProbeInterval = 500 * time.Millisecond

func probePreviewPort(ctx context.Context, ops PreviewSandboxOps, sandboxName string, port int) bool {
	return retryProbe(ctx, func() bool { return ops.ProbeHTTPPort(ctx, sandboxName, port) })
}

func retryProbe(ctx context.Context, probe func() bool) bool {
	for i := 0; i < previewProbeAttempts; i++ {
		if probe() {
			return true
		}
		if i == previewProbeAttempts-1 {
			break
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(previewProbeInterval):
		}
	}
	return false
}

// previewUnreachableError tells the agent what to fix, using the in-sandbox
// listen addresses when ops can report them.
func previewUnreachableError(ctx context.Context, ops PreviewSandboxOps, sandboxName string, port int) error {
	insp, ok := ops.(previewListenInspector)
	if !ok {
		return fmt.Errorf("预览端口 %d 不可达：请确认服务已启动并监听 0.0.0.0:%d，然后重试 set_preview", port, port)
	}
	addrs, err := insp.ListenAddrs(ctx, sandboxName, port)
	if err != nil {
		return fmt.Errorf("预览端口 %d 不可达：请确认服务已启动并监听 0.0.0.0:%d，然后重试 set_preview", port, port)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("预览端口 %d 没有进程在监听：请先在沙箱里启动服务并监听 0.0.0.0:%d，然后重试 set_preview", port, port)
	}
	list := strings.Join(addrs, ", ")
	if allLoopback(addrs) {
		return fmt.Errorf("预览端口 %d 只监听在 %s，平台访问不到：请改为监听 0.0.0.0:%d，然后重试 set_preview", port, list, port)
	}
	return fmt.Errorf("预览端口 %d 已在 %s 监听，但平台请求没有响应（服务可能还在启动或卡住）：确认服务能正常返回后重试 set_preview", port, list)
}

func allLoopback(addrs []string) bool {
	for _, a := range addrs {
		host := a
		if h, _, err := net.SplitHostPort(a); err == nil {
			host = h
		}
		host = strings.Trim(host, "[]")
		if i := strings.IndexByte(host, '%'); i >= 0 {
			host = host[:i]
		}
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return false
		}
	}
	return true
}

// Results of `preview-inject.sh set-upstream` (see InjectSwitch).
const (
	InjectSwitched    = "ok"
	InjectReset       = "reset"
	InjectBusy        = "busy"
	InjectNoListen    = "nolisten"
	InjectDenied      = "denied"
	InjectUnsupported = "unsupported"
)

// InjectSwitch is the outcome of pointing the sandbox's preview injector at
// an app port. PreviewPort is the sandbox's public PREVIEW_PORT (0 = unknown).
type InjectSwitch struct {
	Result      string
	PreviewPort int
}

// previewInjectSwitcher is optionally implemented by PreviewSandboxOps for
// IP-direct sandboxes whose injector can serve an app on any port.
type previewInjectSwitcher interface {
	SwitchInjectUpstream(ctx context.Context, sandboxName string, port int) (InjectSwitch, error)
	// ProbeInjectUpstream reports whether PREVIEW_PORT answers from want.
	ProbeInjectUpstream(ctx context.Context, sandboxName string, previewPort, want int) bool
}

func probeInjectUpstream(ctx context.Context, sw previewInjectSwitcher, sandboxName string, previewPort, want int) bool {
	return retryProbe(ctx, func() bool { return sw.ProbeInjectUpstream(ctx, sandboxName, previewPort, want) })
}

// reservedPreviewPorts are the sandbox's own services (sshd, VNC, noVNC,
// code-server, backend, CDP, injector). Behind IP-direct preview they would be
// public without auth, and keepalive would restart them.
var reservedPreviewPorts = map[int]bool{22: true, 5900: true, 6080: true, 8744: true, 8765: true, 9222: true, 17980: true}

func reservedPreviewPortError(port int) error {
	return fmt.Errorf("端口 %d 是沙箱自带的服务（SSH、远程桌面、IDE、浏览器调试或沙箱后端），不能登记为预览；请登记你启动的前端页面端口", port)
}

// previewPortPublisher is optionally implemented to PATCH k8s Services for unpublished ports.
type previewPortPublisher interface {
	EnsurePublishedPort(ctx context.Context, sandboxName string, port int) (string, bool)
}

// previewLeaser is optionally implemented to keep a sandbox serving a
// registered preview alive until the run ends, so a later review/gate can
// still open it after the node's debug window.
type previewLeaser interface {
	LeasePreviewSandbox(sandboxName string)
}

// PreviewDesktop optionally puts a registered app port on the sandbox desktop
// (the VNC screen the Agent's browser also drives) after set_preview.
type PreviewDesktop interface {
	ShowPreviewOnDesktop(sandboxName string, port int)
}

func previewKey(runID, nodeID string) string { return runID + "|" + nodeID }

func previewItemKey(kind string, port int, externalURL string) string {
	if kind == PreviewKindURL || strings.TrimSpace(externalURL) != "" {
		return "u:" + strings.TrimSpace(externalURL)
	}
	return fmt.Sprintf("p:%d", port)
}

func previewPortKind(p PreviewPort) string {
	if k := strings.TrimSpace(p.Kind); k != "" {
		return k
	}
	if strings.TrimSpace(p.URL) != "" {
		return PreviewKindURL
	}
	return PreviewKindPort
}

// PreviewItemKeyFor returns the stable upsert key for a preview registration.
func PreviewItemKeyFor(p PreviewPort) string {
	if k := strings.TrimSpace(p.ItemKey); k != "" {
		return k
	}
	return previewItemKey(previewPortKind(p), p.Port, p.URL)
}

func previewPortItemKey(p PreviewPort) string {
	return PreviewItemKeyFor(p)
}

// ValidatePreviewURL checks an absolute http/https URL (authority may include a port).
func ValidatePreviewURL(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", fmt.Errorf("url is required")
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", fmt.Errorf("invalid url: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("url scheme must be http or https")
	}
	if !u.IsAbs() || strings.TrimSpace(u.Host) == "" {
		return "", fmt.Errorf("url must be an absolute http(s) address")
	}
	u.Scheme = scheme
	u.Host = strings.ToLower(u.Host)
	return u.String(), nil
}

// SetPreviewBaseURL sets the browser-facing base URL used to build proxy links.
func (h *Host) SetPreviewBaseURL(base string) {
	h.mu.Lock()
	h.previewBase = strings.TrimRight(strings.TrimSpace(base), "/")
	h.mu.Unlock()
}

// SetPreviewStore wires DB persistence for preview ports.
func (h *Host) SetPreviewStore(s PreviewStore) { h.previewStore = s }

// SetPreviewSandboxOps wires sandbox lookup and health probes.
func (h *Host) SetPreviewSandboxOps(ops PreviewSandboxOps) { h.previewOps = ops }

func (h *Host) previewProxyURL(runID, nodeID string, port int) string {
	return fmt.Sprintf("/preview/%s/%s/%d/", runID, nodeID, port)
}

// HasPreviewPorts reports whether at least one healthy preview port is registered.
// Unreachable registrations do not count (set_preview fails closed on probe).
func (h *Host) HasPreviewPorts(runID, nodeID string) bool {
	return h.HasHealthyPreviewPorts(runID, nodeID)
}

// HasHealthyPreviewPorts reports whether at least one registered port is Healthy.
func (h *Host) HasHealthyPreviewPorts(runID, nodeID string) bool {
	for _, p := range h.ListPreviewPorts(runID, nodeID) {
		if p.Healthy {
			return true
		}
	}
	return false
}

// ListPreviewKeepalivePIDs returns setsid-detached preview pids for whitelist
// during Cancel/Abort session cleanup (sandbox Destroy still reclaims all).
func (h *Host) ListPreviewKeepalivePIDs(runID, nodeID string) []int {
	ports := h.ListPreviewPorts(runID, nodeID)
	out := make([]int, 0, len(ports))
	seen := map[int]bool{}
	for _, p := range ports {
		if p.KeepalivePID > 0 && !seen[p.KeepalivePID] {
			seen[p.KeepalivePID] = true
			out = append(out, p.KeepalivePID)
		}
	}
	return out
}

// IsPreviewKeepalivePID reports whether pid is a registered preview keepalive
// pid for this run (any node). Used to skip killing preview processes when
// ending an Agent/ACP session without destroying the sandbox.
func (h *Host) IsPreviewKeepalivePID(runID string, pid int) bool {
	if h == nil || pid <= 0 || runID == "" {
		return false
	}
	h.mu.RLock()
	mem := h.previewMem
	h.mu.RUnlock()
	for key, ports := range mem {
		if !strings.HasPrefix(key, runID+"|") {
			continue
		}
		for _, p := range ports {
			if p.KeepalivePID == pid {
				return true
			}
		}
	}
	// Also check DB-backed ports for this run (best-effort via store list is
	// per-node; callers with nodeID should use ListPreviewKeepalivePIDs).
	_ = mem
	return false
}

// SignalPreviewReady marks that a healthy set_preview completed for run/node
// and wakes any WaitPreviewReady / early-finish watchers in the provider.
func (h *Host) SignalPreviewReady(runID, nodeID string) {
	if h == nil || runID == "" || nodeID == "" {
		return
	}
	key := previewKey(runID, nodeID)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.previewReady == nil {
		h.previewReady = map[string]chan struct{}{}
	}
	ch, ok := h.previewReady[key]
	if !ok {
		ch = make(chan struct{})
		h.previewReady[key] = ch
		close(ch)
		return
	}
	select {
	case <-ch:
		// already signaled
	default:
		close(ch)
	}
}

// PreviewReadyChan returns a channel closed when a healthy set_preview lands
// for this run/node. The channel may already be closed.
func (h *Host) PreviewReadyChan(runID, nodeID string) <-chan struct{} {
	key := previewKey(runID, nodeID)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.previewReady == nil {
		h.previewReady = map[string]chan struct{}{}
	}
	ch, ok := h.previewReady[key]
	if !ok {
		ch = make(chan struct{})
		h.previewReady[key] = ch
	}
	return ch
}

// ResetPreviewReady clears the ready signal for a fresh production attempt
// (e.g. nodeReq / ClearOutcome path). Idempotent.
func (h *Host) ResetPreviewReady(runID, nodeID string) {
	if h == nil {
		return
	}
	key := previewKey(runID, nodeID)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.previewReady == nil {
		return
	}
	delete(h.previewReady, key)
}

// ListPreviewPorts returns all preview ports for a run/node (memory + DB).
func (h *Host) ListPreviewPorts(runID, nodeID string) []PreviewPort {
	h.mu.RLock()
	mem := append([]PreviewPort(nil), h.previewMem[previewKey(runID, nodeID)]...)
	store := h.previewStore
	h.mu.RUnlock()
	if store == nil {
		return h.annotatePreviewTransport(runID, nodeID, mem)
	}
	dbPorts, err := store.ListPreviewPorts(runID, nodeID)
	if err != nil {
		log.Warn().Err(err).Str("run_id", runID).Str("node_id", nodeID).
			Msg("list preview ports from store failed; using memory")
		return h.annotatePreviewTransport(runID, nodeID, mem)
	}
	if len(dbPorts) == 0 {
		return h.annotatePreviewTransport(runID, nodeID, mem)
	}
	if len(mem) == 0 {
		return h.annotatePreviewTransport(runID, nodeID, dbPorts)
	}
	byKey := map[string]PreviewPort{}
	for _, p := range dbPorts {
		byKey[previewPortItemKey(p)] = p
	}
	for _, p := range mem {
		byKey[previewPortItemKey(p)] = p
	}
	out := make([]PreviewPort, 0, len(byKey))
	for _, p := range byKey {
		out = append(out, p)
	}
	return h.annotatePreviewTransport(runID, nodeID, out)
}

func (h *Host) upsertPreviewMem(runID, nodeID string, rec PreviewPort) {
	key := previewKey(runID, nodeID)
	if h.previewMem == nil {
		h.previewMem = map[string][]PreviewPort{}
	}
	ports := h.previewMem[key]
	itemKey := previewPortItemKey(rec)
	found := false
	for i, p := range ports {
		if previewPortItemKey(p) == itemKey {
			ports[i] = rec
			found = true
			break
		}
	}
	if !found {
		ports = append(ports, rec)
	}
	h.previewMem[key] = ports
}

// PreviewRegistration is what a healthy set_preview(port) registered.
type PreviewRegistration struct {
	ProxyURL string
	// PublicURL is the reviewer-facing http://IP:port/ (IP-direct only).
	PublicURL string
	// Picked is true when PublicURL goes through the in-sandbox injector, so
	// the new-tab page carries the pick script.
	Picked bool
	// Note explains why PublicURL has no pick script, when Agent can fix it.
	Note string
}

// previewPortResult is the set_preview(port) reply: the reviewer's entry and
// the desktop address are different on purpose, so name both.
func previewPortResult(port int, reg PreviewRegistration) string {
	var b strings.Builder
	b.WriteString("ok: 预览已注册")
	if reg.PublicURL != "" {
		fmt.Fprintf(&b, ";审批人入口 %s", reg.PublicURL)
		if reg.Picked {
			b.WriteString("(已注入取点脚本)")
		}
	}
	fmt.Fprintf(&b, ";沙箱桌面浏览器已切到 http://127.0.0.1:%d/(用户在远程桌面里看到的就是这一页,你自测也用这个地址)", port)
	fmt.Fprintf(&b, ";代理 URL: %s", reg.ProxyURL)
	if reg.Note != "" {
		b.WriteString(";注意:")
		b.WriteString(reg.Note)
	}
	return b.String()
}

func (h *Host) lockPreviewSandbox(sandboxName string) func() {
	h.mu.Lock()
	if h.previewLocks == nil {
		h.previewLocks = map[string]*sync.Mutex{}
	}
	l, ok := h.previewLocks[sandboxName]
	if !ok {
		l = &sync.Mutex{}
		h.previewLocks[sandboxName] = l
	}
	h.mu.Unlock()
	l.Lock()
	return l.Unlock
}

func (h *Host) setPreviewPort(runID, nodeID string, port int, label string) (PreviewRegistration, error) {
	if port <= 0 || port > 65535 {
		return PreviewRegistration{}, fmt.Errorf("port must be 1-65535")
	}
	if reservedPreviewPorts[port] {
		return PreviewRegistration{}, reservedPreviewPortError(port)
	}
	h.mu.RLock()
	ops := h.previewOps
	h.mu.RUnlock()
	sandboxName := ""
	if ops != nil {
		if name, ok := ops.SandboxForRunNode(runID, nodeID); ok {
			sandboxName = name
		}
	}
	if ops == nil || sandboxName == "" {
		return PreviewRegistration{}, fmt.Errorf("无法探测预览端口:沙箱未就绪")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	defer h.lockPreviewSandbox(sandboxName)()

	direct := h.previewDirect(runID, nodeID)
	sw := InjectSwitch{}
	switcher, canSwitch := ops.(previewInjectSwitcher)
	if direct && canSwitch {
		res, err := switcher.SwitchInjectUpstream(ctx, sandboxName, port)
		if err != nil {
			log.Warn().Err(err).Str("run_id", runID).Str("node_id", nodeID).
				Int("port", port).Msg("preview inject switch failed")
			res = InjectSwitch{Result: InjectUnsupported}
		}
		sw = res
	}
	switch sw.Result {
	case InjectDenied:
		return PreviewRegistration{}, reservedPreviewPortError(port)
	case InjectNoListen:
		return PreviewRegistration{}, fmt.Errorf("预览端口 %d 没有进程在监听：请先在沙箱里启动服务，然后重试 set_preview", port)
	case InjectSwitched:
		return h.registerSwitchedPreview(ctx, ops, switcher, runID, nodeID, sandboxName, port, sw.PreviewPort, label)
	}

	if direct {
		if pub, ok := ops.(previewPortPublisher); ok {
			_, _ = pub.EnsurePublishedPort(ctx, sandboxName, port)
		}
	}
	// Probe before keepalive: keepalive restarts the listener, which only makes
	// sense once the port is known to serve the platform.
	probe := func() bool { return probePreviewPort(ctx, ops, sandboxName, port) }
	unreachable := func() error { return previewUnreachableError(ctx, ops, sandboxName, port) }
	if !probe() {
		return PreviewRegistration{}, unreachable()
	}
	keepalivePID, err := h.keepalivePreview(ctx, ops, runID, nodeID, sandboxName, port, probe, unreachable)
	if err != nil {
		return PreviewRegistration{}, err
	}
	host := ""
	if base, ok := ops.PreviewUpstream(ctx, sandboxName, port); ok && base != "" {
		host = base
	}
	reg, err := h.savePreviewPort(ops, runID, nodeID, sandboxName, port, label, host, keepalivePID)
	if err != nil {
		return PreviewRegistration{}, err
	}
	if direct {
		reg.Picked = sw.Result == InjectReset
		switch sw.Result {
		case InjectBusy:
			reg.Note = fmt.Sprintf("$PREVIEW_PORT(=%d) 已被另一个页面占用，端口 %d 的新标签页不会注入取点脚本；需要取点时只保留一个前端，或把它开在 $PREVIEW_PORT", sw.PreviewPort, port)
		case InjectUnsupported:
			reg.Note = fmt.Sprintf("沙箱的预览注入代理不支持切换端口（运行时较旧），端口 %d 的新标签页可能没有取点；需要取点请把服务开在 $PREVIEW_PORT", port)
		}
	}
	return reg, nil
}

// registerSwitchedPreview finishes a set_preview whose port now sits behind
// PREVIEW_PORT's injector: probe through PREVIEW_PORT, keep the app alive, and
// record PREVIEW_PORT's address as the public one while Port stays the app's
// own (the VNC desktop opens 127.0.0.1:<Port>, which bypasses the injector).
func (h *Host) registerSwitchedPreview(ctx context.Context, ops PreviewSandboxOps, sw previewInjectSwitcher, runID, nodeID, sandboxName string, port, previewPort int, label string) (PreviewRegistration, error) {
	if pub, ok := ops.(previewPortPublisher); ok {
		_, _ = pub.EnsurePublishedPort(ctx, sandboxName, previewPort)
	}
	probe := func() bool { return probeInjectUpstream(ctx, sw, sandboxName, previewPort, port) }
	unreachable := func() error {
		return fmt.Errorf("预览端口 %d 已接到 $PREVIEW_PORT(=%d)，但平台经 $PREVIEW_PORT 访问不到它：确认服务能正常返回 HTTP 后重试 set_preview", port, previewPort)
	}
	if !probe() {
		h.resetInjectUpstream(ctx, sw, sandboxName, previewPort)
		return PreviewRegistration{}, unreachable()
	}
	keepalivePID, err := h.keepalivePreview(ctx, ops, runID, nodeID, sandboxName, port, probe, unreachable)
	if err != nil {
		h.resetInjectUpstream(ctx, sw, sandboxName, previewPort)
		return PreviewRegistration{}, err
	}
	host := ""
	if base, ok := ops.PreviewUpstream(ctx, sandboxName, previewPort); ok && base != "" {
		host = base
	}
	if host != "" {
		h.releaseDisplacedPreviews(ctx, ops, runID, nodeID, sandboxName, port, host)
	}
	reg, err := h.savePreviewPort(ops, runID, nodeID, sandboxName, port, label, host, keepalivePID)
	if err != nil {
		return PreviewRegistration{}, err
	}
	reg.Picked = true
	return reg, nil
}

func (h *Host) resetInjectUpstream(ctx context.Context, sw previewInjectSwitcher, sandboxName string, previewPort int) {
	if _, err := sw.SwitchInjectUpstream(ctx, sandboxName, previewPort); err != nil {
		log.Warn().Err(err).Str("sandbox", sandboxName).Msg("preview inject reset failed")
	}
}

// releaseDisplacedPreviews points earlier registrations that shared host
// (PREVIEW_PORT's address) back at their own address: the injector now serves
// port, so their new-tab link would open the wrong app.
func (h *Host) releaseDisplacedPreviews(ctx context.Context, ops PreviewSandboxOps, runID, nodeID, sandboxName string, port int, host string) {
	h.mu.RLock()
	store := h.previewStore
	h.mu.RUnlock()
	for _, p := range h.ListPreviewPorts(runID, nodeID) {
		if previewPortKind(p) != PreviewKindPort || p.Port == port || p.Host != host {
			continue
		}
		p.Host = ""
		if base, ok := ops.PreviewUpstream(ctx, sandboxName, p.Port); ok && base != host {
			p.Host = base
		}
		p.Mode, p.DirectURL = "", ""
		h.mu.Lock()
		h.upsertPreviewMem(runID, nodeID, p)
		h.mu.Unlock()
		if store != nil {
			if err := store.UpsertPreviewPort(p); err != nil {
				log.Warn().Err(err).Str("run_id", runID).Int("port", p.Port).Msg("release displaced preview failed")
			}
		}
	}
}

func (h *Host) keepalivePreview(ctx context.Context, ops PreviewSandboxOps, runID, nodeID, sandboxName string, port int, probe func() bool, unreachable func() error) (int, error) {
	keepalivePID, err := ops.KeepalivePort(ctx, sandboxName, port)
	if err != nil {
		log.Warn().Err(err).Str("run_id", runID).Str("node_id", nodeID).
			Int("port", port).Msg("preview keepalive failed")
		return 0, fmt.Errorf("预览端口 %d 可以访问，但脱离 Agent 进程保活失败：%w；服务仍在运行，可稍后重试 set_preview", port, err)
	}
	if !probe() {
		return 0, fmt.Errorf("预览端口 %d 保活重启后不可达：%w", port, unreachable())
	}
	return keepalivePID, nil
}

func (h *Host) savePreviewPort(ops PreviewSandboxOps, runID, nodeID, sandboxName string, port int, label, host string, keepalivePID int) (PreviewRegistration, error) {
	h.mu.RLock()
	store := h.previewStore
	h.mu.RUnlock()
	proxyURL := h.previewProxyURL(runID, nodeID, port)
	rec := PreviewPort{
		RunID: runID, NodeID: nodeID, Kind: PreviewKindPort,
		ItemKey: previewItemKey(PreviewKindPort, port, ""),
		Port:    port, Label: strings.TrimSpace(label),
		ProxyURL: proxyURL, SandboxName: sandboxName, Host: host, Healthy: true,
		KeepalivePID: keepalivePID, RegisteredAt: time.Now(),
	}
	h.mu.Lock()
	h.upsertPreviewMem(runID, nodeID, rec)
	h.mu.Unlock()
	if store != nil {
		if err := store.UpsertPreviewPort(rec); err != nil {
			return PreviewRegistration{}, err
		}
	}
	// IP-direct only adds a new-tab link; the VNC desktop shows the app either way.
	if desk, ok := ops.(PreviewDesktop); ok {
		desk.ShowPreviewOnDesktop(sandboxName, port)
	}
	if l, ok := ops.(previewLeaser); ok {
		l.LeasePreviewSandbox(sandboxName)
	}
	h.SignalPreviewReady(runID, nodeID)
	reg := PreviewRegistration{ProxyURL: proxyURL}
	if h.previewDirect(runID, nodeID) && host != "" {
		reg.PublicURL = strings.TrimRight(host, "/") + "/"
	}
	return reg, nil
}

func (h *Host) setPreviewURL(runID, nodeID, rawURL, label string) (string, error) {
	normalized, err := ValidatePreviewURL(rawURL)
	if err != nil {
		return "", err
	}
	h.mu.RLock()
	store := h.previewStore
	h.mu.RUnlock()
	rec := PreviewPort{
		RunID: runID, NodeID: nodeID, Kind: PreviewKindURL,
		ItemKey: previewItemKey(PreviewKindURL, 0, normalized),
		URL:     normalized, Label: strings.TrimSpace(label),
		Healthy: true, RegisteredAt: time.Now(),
	}
	h.mu.Lock()
	h.upsertPreviewMem(runID, nodeID, rec)
	h.mu.Unlock()
	if store != nil {
		if err := store.UpsertPreviewPort(rec); err != nil {
			return "", err
		}
	}
	h.SignalPreviewReady(runID, nodeID)
	return normalized, nil
}

// PutPreviewPortForTest seeds a memory-only healthy preview port without sandbox ops.
// Used by engine unit tests that exercise preview pause paths offline.
func (h *Host) PutPreviewPortForTest(runID, nodeID string, port int, label string) {
	if h == nil || port <= 0 {
		return
	}
	rec := PreviewPort{
		RunID: runID, NodeID: nodeID, Kind: PreviewKindPort,
		ItemKey: previewItemKey(PreviewKindPort, port, ""),
		Port:    port, Label: strings.TrimSpace(label),
		ProxyURL: h.previewProxyURL(runID, nodeID, port), Healthy: true,
		// Non-zero sentinel so ListPreviewKeepalivePIDs / AbortRun whitelist
		// paths exercise the same registration shape as real set_preview.
		KeepalivePID: 1, RegisteredAt: time.Now(),
	}
	h.mu.Lock()
	h.upsertPreviewMem(runID, nodeID, rec)
	h.mu.Unlock()
	h.SignalPreviewReady(runID, nodeID)
}

func parsePreviewPort(v any) (int, error) {
	switch t := v.(type) {
	case float64:
		return int(t), nil
	case int:
		return t, nil
	case int64:
		return int(t), nil
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(t))
		if err != nil {
			return 0, fmt.Errorf("invalid port")
		}
		return n, nil
	default:
		return 0, fmt.Errorf("port is required")
	}
}

func (h *Host) previewDirect(runID, nodeID string) bool {
	h.mu.RLock()
	ops := h.previewOps
	h.mu.RUnlock()
	c, ok := ops.(previewDirectChecker)
	return ok && c.DirectPreview(runID, nodeID)
}

func (h *Host) annotatePreviewTransport(runID, nodeID string, ports []PreviewPort) []PreviewPort {
	if !h.previewDirect(runID, nodeID) {
		return ports
	}
	out := make([]PreviewPort, len(ports))
	copy(out, ports)
	for i := range out {
		out[i].Mode = "direct"
		if out[i].Host != "" {
			out[i].DirectURL = strings.TrimRight(out[i].Host, "/") + "/"
		}
	}
	return out
}
