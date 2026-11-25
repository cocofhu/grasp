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

	"github.com/cocofhu/grasp/internal/previewexpose"
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
	Port        int    `json:"port"`
	URL         string `json:"url,omitempty"`
	Label       string `json:"label,omitempty"`
	ProxyURL    string `json:"proxyUrl"`
	SandboxName string `json:"-"`
	// Host is the resolved upstream base the proxy dials (e.g.
	// "http://172.17.0.5:9090"), persisted so the proxy needn't re-resolve the
	// container IP through the sandbox manager on every request.
	Host string `json:"-"`
	// InjectorPort is the published upstream when it differs from the app port.
	InjectorPort int `json:"-"`
	// ExposeStrategy is the persisted resolved preview exposure strategy (never "auto").
	ExposeStrategy string `json:"-"`
	// ExposeKey is the persisted opaque route key for inbound exposure strategies.
	ExposeKey string `json:"-"`
	// Mode is "direct" for an IP-direct preview; empty/vnc otherwise.
	Mode string `json:"mode,omitempty"`
	// DirectURL is the browser-facing http://IP:port/ when Mode=direct.
	DirectURL string `json:"directUrl,omitempty"`
	// Healthy is true when ProbeHTTPPort succeeded at registration time.
	// set_preview only succeeds when Healthy; HasHealthyPreview gates
	// production-phase completion / early park into review.
	Healthy bool `json:"healthy"`
	// KeepalivePID is the setsid-detached listener pid (0 when unknown).
	KeepalivePID int       `json:"keepalivePid,omitempty"`
	RegisteredAt time.Time `json:"registeredAt"`
}

// PreviewStore persists the active preview registration. A registration
// replaces any other registration of the same run/node or sandbox.
type PreviewStore interface {
	ReplacePreview(rec PreviewPort) error
	ActivePreview(runID, nodeID string) (*PreviewPort, error)
	// ActivePreviewForSandbox finds the registration a new node replaces when
	// workflow nodes share one sandbox.
	ActivePreviewForSandbox(sandboxName string) (*PreviewPort, error)
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

// previewConnectionResolver is optionally implemented by PreviewSandboxOps to
// identify the owning gateway connection for a sandbox.
type previewConnectionResolver interface {
	PreviewConnection(sandboxName string) (connID string, ok bool)
}

// previewExposeKeyStore is optionally implemented by PreviewStore to look up a
// registration by its opaque inbound expose key.
type previewExposeKeyStore interface {
	PreviewByExposeKey(key string) (*PreviewPort, error)
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
	if _, ok := ops.PreviewUpstream(ctx, sandboxName, port); !ok {
		return fmt.Errorf("预览端口 %d 已在 %s 监听，但网关没有可用的端口映射：请使用沙箱已映射的端口后重试 set_preview", port, list)
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

// Desktop-image services (launcher 6091, Blender MCP add-on 6092, …) only
// take ports from this loopback range, so the whole range is refused.
const (
	reservedDesktopPortMin = 6090
	reservedDesktopPortMax = 6099
)

func previewPortReserved(port int) bool {
	return reservedPreviewPorts[port] || (port >= reservedDesktopPortMin && port <= reservedDesktopPortMax)
}

func reservedPreviewPortError(port int) error {
	return fmt.Errorf("端口 %d 是沙箱自带的服务（SSH、远程桌面、IDE、浏览器调试、沙箱后端或桌面内部服务 6090–6099），不能登记为预览；请登记你启动的前端页面端口", port)
}

// previewPortPublisher is optionally implemented to PATCH k8s Services for unpublished ports.
type previewPortPublisher interface {
	EnsurePublishedPort(ctx context.Context, sandboxName string, port int) (string, bool)
}

// PreviewDesktop optionally puts a registered app port on the sandbox desktop
// (the VNC screen the Agent's browser also drives) after set_preview.
type PreviewDesktop interface {
	ShowPreviewOnDesktop(sandboxName string, port int)
}

func previewKey(runID, nodeID string) string { return runID + "|" + nodeID }

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

// SetPreviewProxyHardening enables generation, upstream-port, and reserved-header
// checks on the path preview proxy and public preview-api proxy.
func (h *Host) SetPreviewProxyHardening(enabled bool) {
	h.mu.Lock()
	h.previewProxyHardening = enabled
	h.mu.Unlock()
}

// PreviewProxyHardening reports whether path preview proxy hardening is enabled.
func (h *Host) PreviewProxyHardening() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.previewProxyHardening
}

// SetPreviewExposePolicy wires the per-connection preview exposure policy.
func (h *Host) SetPreviewExposePolicy(p *previewexpose.Policy) {
	h.mu.Lock()
	h.previewExpose = p
	h.mu.Unlock()
}

// SetPreviewStore wires DB persistence for preview ports.
func (h *Host) SetPreviewStore(s PreviewStore) { h.previewStore = s }

// SetPreviewSandboxOps wires sandbox lookup and health probes.
func (h *Host) SetPreviewSandboxOps(ops PreviewSandboxOps) { h.previewOps = ops }

func (h *Host) previewProxyURL(runID, nodeID string, port int) string {
	return fmt.Sprintf("/preview/%s/%s/%d/", runID, nodeID, port)
}

// HasHealthyPreview reports whether the active preview is Healthy.
// Unreachable registrations do not count (set_preview fails closed on probe).
func (h *Host) HasHealthyPreview(runID, nodeID string) bool {
	p, ok := h.ActivePreview(runID, nodeID)
	return ok && p.Healthy
}

// PreviewKeepalivePID returns the active preview's detached pid (0 when none)
// for the whitelist during Cancel/Abort session cleanup.
func (h *Host) PreviewKeepalivePID(runID, nodeID string) int {
	p, ok := h.ActivePreview(runID, nodeID)
	if !ok {
		return 0
	}
	return p.KeepalivePID
}

// ActivePreview returns the active preview for a run/node.
func (h *Host) ActivePreview(runID, nodeID string) (PreviewPort, bool) {
	raw, ok := h.activePreviewRaw(runID, nodeID)
	if !ok {
		return PreviewPort{}, false
	}
	return h.annotatePreviewTransport(raw), true
}

func (h *Host) activePreviewRaw(runID, nodeID string) (PreviewPort, bool) {
	h.mu.RLock()
	mem, hasMem := h.previewMem[previewKey(runID, nodeID)]
	store := h.previewStore
	h.mu.RUnlock()
	if hasMem {
		return mem, true
	}
	if store == nil {
		return PreviewPort{}, false
	}
	stored, err := store.ActivePreview(runID, nodeID)
	if err != nil {
		log.Warn().Err(err).Str("run_id", runID).Str("node_id", nodeID).
			Msg("load active preview from store failed")
		return PreviewPort{}, false
	}
	if stored == nil {
		return PreviewPort{}, false
	}
	return *stored, true
}

// PreviewByExposeKey looks up the active preview registration bound to key.
func (h *Host) PreviewByExposeKey(key string) (PreviewPort, bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return PreviewPort{}, false
	}
	h.mu.RLock()
	store := h.previewStore
	for _, candidate := range h.previewMem {
		if candidate.ExposeKey == key {
			h.mu.RUnlock()
			return h.annotatePreviewTransport(candidate), true
		}
	}
	h.mu.RUnlock()
	keyStore, ok := store.(previewExposeKeyStore)
	if !ok || keyStore == nil {
		return PreviewPort{}, false
	}
	stored, err := keyStore.PreviewByExposeKey(key)
	if err != nil {
		log.Warn().Err(err).Msg("load preview by expose key failed")
		return PreviewPort{}, false
	}
	if stored == nil {
		return PreviewPort{}, false
	}
	return h.annotatePreviewTransport(*stored), true
}

func (h *Host) replacePreviewMem(rec PreviewPort) {
	if h.previewMem == nil {
		h.previewMem = map[string]PreviewPort{}
	}
	for key, current := range h.previewMem {
		if key == previewKey(rec.RunID, rec.NodeID) ||
			(rec.SandboxName != "" && current.SandboxName == rec.SandboxName) {
			delete(h.previewMem, key)
		}
	}
	h.previewMem[previewKey(rec.RunID, rec.NodeID)] = rec
}

// sandboxPreview returns the registration currently owning sandboxName's
// injector, whichever run/node registered it.
func (h *Host) sandboxPreview(sandboxName string) *PreviewPort {
	h.mu.RLock()
	store := h.previewStore
	for _, candidate := range h.previewMem {
		if candidate.SandboxName == sandboxName {
			h.mu.RUnlock()
			return &candidate
		}
	}
	h.mu.RUnlock()
	if store == nil {
		return nil
	}
	stored, err := store.ActivePreviewForSandbox(sandboxName)
	if err != nil {
		log.Warn().Err(err).Str("sandbox", sandboxName).Msg("load sandbox preview from store failed")
		return nil
	}
	return stored
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

func (h *Host) lockPreviewKey(key string) func() {
	h.mu.Lock()
	if h.previewLocks == nil {
		h.previewLocks = map[string]*sync.Mutex{}
	}
	l, ok := h.previewLocks[key]
	if !ok {
		l = &sync.Mutex{}
		h.previewLocks[key] = l
	}
	h.mu.Unlock()
	l.Lock()
	return l.Unlock
}

func (h *Host) setPreviewPort(runID, nodeID string, port int, label string) (PreviewRegistration, error) {
	if port <= 0 || port > 65535 {
		return PreviewRegistration{}, fmt.Errorf("port must be 1-65535")
	}
	if previewPortReserved(port) {
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
	defer h.lockPreviewKey("node:" + previewKey(runID, nodeID))()
	defer h.lockPreviewKey("sandbox:" + sandboxName)()

	direct := h.previewDirect(runID, nodeID)
	sw := InjectSwitch{}
	switcher, canSwitch := ops.(previewInjectSwitcher)
	var previous *PreviewPort
	if canSwitch {
		previous = h.sandboxPreview(sandboxName)
	}
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
		return h.registerSwitchedPreview(ctx, ops, switcher, runID, nodeID, sandboxName, port, sw.PreviewPort, label, previous)
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
		if sw.Result == InjectReset {
			h.restorePreviousInjectUpstream(ctx, switcher, sandboxName, sw.PreviewPort, previous)
		}
		return PreviewRegistration{}, unreachable()
	}
	keepalivePID, err := h.keepalivePreview(ctx, ops, runID, nodeID, sandboxName, port, probe, unreachable)
	if err != nil {
		if sw.Result == InjectReset {
			h.restorePreviousInjectUpstream(ctx, switcher, sandboxName, sw.PreviewPort, previous)
		}
		return PreviewRegistration{}, err
	}
	host := ""
	if base, ok := ops.PreviewUpstream(ctx, sandboxName, port); ok && base != "" {
		host = base
	}
	reg, err := h.savePreviewPort(ctx, ops, runID, nodeID, sandboxName, port, 0, label, host, keepalivePID)
	if err != nil {
		if sw.Result == InjectReset {
			h.restorePreviousInjectUpstream(ctx, switcher, sandboxName, sw.PreviewPort, previous)
		}
		return PreviewRegistration{}, err
	}
	if direct {
		reg.Picked = sw.Result == InjectReset
		var injectNote string
		switch sw.Result {
		case InjectBusy:
			injectNote = fmt.Sprintf("$PREVIEW_PORT(=%d) 当前无法切换，端口 %d 的活动预览不会注入取点脚本；需要取点时请把服务开在 $PREVIEW_PORT", sw.PreviewPort, port)
		case InjectUnsupported:
			injectNote = fmt.Sprintf("沙箱的预览注入代理不支持切换端口（运行时较旧），端口 %d 的活动预览可能没有取点；需要取点请把服务开在 $PREVIEW_PORT", port)
		}
		if injectNote != "" {
			if reg.Note != "" {
				reg.Note = injectNote + "；" + reg.Note
			} else {
				reg.Note = injectNote
			}
		}
	}
	return reg, nil
}

// registerSwitchedPreview finishes a set_preview whose port now sits behind
// PREVIEW_PORT's injector: probe through PREVIEW_PORT, keep the app alive, and
// record PREVIEW_PORT's address as the public one while Port stays the app's
// own (the VNC desktop opens 127.0.0.1:<Port>, which bypasses the injector).
func (h *Host) registerSwitchedPreview(ctx context.Context, ops PreviewSandboxOps, sw previewInjectSwitcher, runID, nodeID, sandboxName string, port, previewPort int, label string, previous *PreviewPort) (PreviewRegistration, error) {
	if pub, ok := ops.(previewPortPublisher); ok {
		_, _ = pub.EnsurePublishedPort(ctx, sandboxName, previewPort)
	}
	probe := func() bool { return probeInjectUpstream(ctx, sw, sandboxName, previewPort, port) }
	unreachable := func() error {
		return fmt.Errorf("预览端口 %d 已接到 $PREVIEW_PORT(=%d)，但平台经 $PREVIEW_PORT 访问不到它：确认服务能正常返回 HTTP 后重试 set_preview", port, previewPort)
	}
	if !probe() {
		h.restorePreviousInjectUpstream(ctx, sw, sandboxName, previewPort, previous)
		return PreviewRegistration{}, unreachable()
	}
	keepalivePID, err := h.keepalivePreview(ctx, ops, runID, nodeID, sandboxName, port, probe, unreachable)
	if err != nil {
		h.restorePreviousInjectUpstream(ctx, sw, sandboxName, previewPort, previous)
		return PreviewRegistration{}, err
	}
	host := ""
	if base, ok := ops.PreviewUpstream(ctx, sandboxName, previewPort); ok && base != "" {
		host = base
	}
	reg, err := h.savePreviewPort(ctx, ops, runID, nodeID, sandboxName, port, previewPort, label, host, keepalivePID)
	if err != nil {
		h.restorePreviousInjectUpstream(ctx, sw, sandboxName, previewPort, previous)
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

func (h *Host) restorePreviousInjectUpstream(ctx context.Context, sw previewInjectSwitcher, sandboxName string, previewPort int, previous *PreviewPort) {
	if previous == nil {
		h.resetInjectUpstream(ctx, sw, sandboxName, previewPort)
		return
	}
	if previous.InjectorPort != previewPort || previous.Port <= 0 || previous.Port == previewPort {
		return
	}
	result, err := sw.SwitchInjectUpstream(ctx, sandboxName, previous.Port)
	if err != nil {
		log.Warn().Err(err).Str("sandbox", sandboxName).Int("port", previous.Port).Msg("preview inject rollback failed")
		return
	}
	if result.Result == InjectDenied || result.Result == InjectNoListen ||
		!probeInjectUpstream(ctx, sw, sandboxName, previewPort, previous.Port) {
		log.Warn().Str("sandbox", sandboxName).Int("port", previous.Port).Msg("preview inject rollback probe failed")
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

func (h *Host) savePreviewPort(ctx context.Context, ops PreviewSandboxOps, runID, nodeID, sandboxName string, port, injectorPort int, label, host string, keepalivePID int) (PreviewRegistration, error) {
	h.mu.RLock()
	store := h.previewStore
	h.mu.RUnlock()

	displaced := h.displacedPreviews(runID, nodeID, sandboxName)
	exp := h.previewExposer(sandboxName)
	target := previewexpose.Target{Upstream: host}

	var binding previewexpose.Binding
	reused := false
	for _, old := range displaced {
		if old.SandboxName == sandboxName && old.Port == port &&
			old.ExposeStrategy == exp.Name() && old.ExposeKey != "" {
			binding = previewexpose.Binding{Strategy: old.ExposeStrategy, Key: old.ExposeKey}
			reused = true
			break
		}
	}
	var bindNote string
	if !reused {
		var err error
		binding, err = exp.Bind(ctx, target)
		if err != nil {
			log.Warn().Err(err).Str("run_id", runID).Str("node_id", nodeID).
				Str("sandbox", sandboxName).Str("strategy", exp.Name()).
				Msg("preview expose bind failed; degrading to none")
			binding = previewexpose.Binding{Strategy: previewexpose.StrategyNone}
			bindNote = "预览独立入口绑定失败，已降级为仅远程桌面预览"
		}
	}

	proxyURL := h.previewProxyURL(runID, nodeID, port)
	rec := PreviewPort{
		RunID: runID, NodeID: nodeID, Kind: PreviewKindPort,
		Port: port, Label: strings.TrimSpace(label),
		ProxyURL: proxyURL, SandboxName: sandboxName, Host: host, InjectorPort: injectorPort,
		ExposeStrategy: binding.Strategy, ExposeKey: binding.Key, Healthy: true,
		KeepalivePID: keepalivePID, RegisteredAt: time.Now(),
	}
	if store != nil {
		if err := store.ReplacePreview(rec); err != nil {
			return PreviewRegistration{}, err
		}
	}
	h.mu.Lock()
	h.replacePreviewMem(rec)
	h.mu.Unlock()

	// IP-direct only adds a new-tab link; the VNC desktop shows the app either way.
	if desk, ok := ops.(PreviewDesktop); ok {
		desk.ShowPreviewOnDesktop(sandboxName, port)
	}
	reg := PreviewRegistration{ProxyURL: proxyURL, Note: bindNote}
	if h.previewDirect(runID, nodeID) {
		reg.PublicURL = exp.URL(target, binding)
	}
	return reg, nil
}

func (h *Host) displacedPreviews(runID, nodeID, sandboxName string) []PreviewPort {
	var out []PreviewPort
	if sb := h.sandboxPreview(sandboxName); sb != nil {
		out = append(out, *sb)
	}
	if nodePrev, ok := h.activePreviewRaw(runID, nodeID); ok {
		dup := false
		for _, existing := range out {
			if existing.RunID == nodePrev.RunID && existing.NodeID == nodePrev.NodeID && existing.SandboxName == nodePrev.SandboxName {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, nodePrev)
		}
	}
	return out
}

func (h *Host) setPreviewURL(runID, nodeID, rawURL, label string) (string, error) {
	normalized, err := ValidatePreviewURL(rawURL)
	if err != nil {
		return "", err
	}
	defer h.lockPreviewKey("node:" + previewKey(runID, nodeID))()
	h.mu.RLock()
	ops := h.previewOps
	h.mu.RUnlock()
	sandboxName := ""
	if ops != nil {
		if name, ok := ops.SandboxForRunNode(runID, nodeID); ok {
			sandboxName = name
		}
	}
	if sandboxName != "" {
		defer h.lockPreviewKey("sandbox:" + sandboxName)()
	}
	h.mu.RLock()
	store := h.previewStore
	h.mu.RUnlock()
	rec := PreviewPort{
		RunID: runID, NodeID: nodeID, Kind: PreviewKindURL,
		SandboxName: sandboxName,
		URL:         normalized, Label: strings.TrimSpace(label),
		Healthy: true, RegisteredAt: time.Now(),
	}
	if store != nil {
		if err := store.ReplacePreview(rec); err != nil {
			return "", err
		}
	}
	h.mu.Lock()
	h.replacePreviewMem(rec)
	h.mu.Unlock()
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
		Port: port, Label: strings.TrimSpace(label),
		ProxyURL: h.previewProxyURL(runID, nodeID, port), Healthy: true,
		// Non-zero sentinel so PreviewKeepalivePID / AbortRun whitelist
		// paths exercise the same registration shape as real set_preview.
		KeepalivePID: 1, RegisteredAt: time.Now(),
	}
	h.mu.Lock()
	h.replacePreviewMem(rec)
	h.mu.Unlock()
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

func (h *Host) previewExposer(sandboxName string) previewexpose.Exposer {
	h.mu.RLock()
	policy := h.previewExpose
	ops := h.previewOps
	h.mu.RUnlock()
	res, ok := ops.(previewConnectionResolver)
	if !ok || strings.TrimSpace(sandboxName) == "" {
		return previewexpose.None()
	}
	connID, known := res.PreviewConnection(sandboxName)
	return policy.For(connID, known)
}

func (h *Host) annotatePreviewTransport(p PreviewPort) PreviewPort {
	if !h.previewDirect(p.RunID, p.NodeID) {
		return p
	}
	p.Mode = "direct"
	exp := h.previewExposer(p.SandboxName)
	var binding previewexpose.Binding
	if p.ExposeStrategy == exp.Name() {
		binding = previewexpose.Binding{Strategy: p.ExposeStrategy, Key: p.ExposeKey}
	}
	p.DirectURL = exp.URL(previewexpose.Target{Upstream: p.Host}, binding)
	return p
}
