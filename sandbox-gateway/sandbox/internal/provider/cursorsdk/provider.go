// Package cursorsdk drives Cursor through @cursor/sdk: one long-lived Node
// bridge process (bridge.mjs) per session runs a local Cursor agent and talks
// to the backend in JSON lines over stdio. Input sent while a run is going
// is steered into it, a cancel stops the run without stopping the agent, and
// the agent id continues the conversation in a new process.
package cursorsdk

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"backend/internal/envutil"
	"backend/internal/provider"
)

//go:embed bridge.mjs
var bridgeScript []byte

// DefaultSDKDir is where the image installs @cursor/sdk.
const DefaultSDKDir = "/opt/grasp/cursor-sdk"

// Config parameterizes the Cursor SDK provider.
type Config struct {
	// Node is the Node.js binary (default "node").
	Node string
	// SDKDir holds node_modules/@cursor/sdk (default DefaultSDKDir, or
	// $GRASP_CURSOR_SDK_DIR).
	SDKDir     string
	Runtime    string
	ConfigRoot string
	AuthEnvFn  func(env []string) []string
}

type sdkProvider struct {
	c      Config
	models *modelCache
}

// New returns the Cursor SDK provider.
func New(c Config) provider.Provider { return &sdkProvider{c: c, models: &modelCache{}} }

func (*sdkProvider) Name() provider.Name                 { return provider.Cursor }
func (p *sdkProvider) Runtime() string                   { return p.c.Runtime }
func (p *sdkProvider) DefaultConfigRoot() string         { return p.c.ConfigRoot }
func (*sdkProvider) Transport() provider.TransportKind    { return provider.LongLived }
func (p *sdkProvider) AuthEnv(env []string) []string {
	if p.c.AuthEnvFn == nil {
		return env
	}
	return p.c.AuthEnvFn(env)
}

func (p *sdkProvider) node() string {
	if p.c.Node != "" {
		return p.c.Node
	}
	return "node"
}

func (p *sdkProvider) sdkDir(env []string) string {
	if dir := strings.TrimSpace(envutil.Get(env, "GRASP_CURSOR_SDK_DIR")); dir != "" {
		return dir
	}
	if p.c.SDKDir != "" {
		return p.c.SDKDir
	}
	return DefaultSDKDir
}

// ListModels lists the account's models; the list is cached for a while
// because each call starts a bridge.
func (p *sdkProvider) ListModels(ctx context.Context) ([]provider.Model, error) {
	catalog, err := p.catalog(ctx, p.AuthEnv(os.Environ()))
	if err != nil {
		return nil, err
	}
	out := make([]provider.Model, 0, len(catalog))
	for _, m := range catalog {
		out = append(out, provider.Model{ID: m.ID, Name: m.DisplayName})
	}
	return out, nil
}

func (p *sdkProvider) catalog(ctx context.Context, env []string) ([]sdkModel, error) {
	key := envutil.Get(env, "CURSOR_API_KEY")
	if key == "" {
		return nil, errors.New("Cursor 需要 CURSOR_API_KEY")
	}
	return p.models.get(key, func() ([]sdkModel, error) {
		proc, err := p.start(ctx, "", env)
		if err != nil {
			return nil, err
		}
		proc.serve(nil)
		defer proc.stop()
		r, err := proc.call(ctx, map[string]any{"type": "models", "apiKey": key})
		if err != nil {
			return nil, err
		}
		var models []sdkModel
		if err := json.Unmarshal(r.Data, &models); err != nil {
			return nil, fmt.Errorf("Cursor 模型列表无法解析: %w", err)
		}
		return models, nil
	})
}

// Open starts a bridge, resolves the chat's model, effort and speed tier
// against the account's models, and opens (or resumes) the agent.
func (p *sdkProvider) Open(procCtx, handshakeCtx context.Context, opts provider.OpenOptions,
	onEvent func(json.RawMessage), _ provider.PermissionChooser) (provider.Session, error) {
	env := p.AuthEnv(os.Environ())
	key := envutil.Get(env, "CURSOR_API_KEY")
	if key == "" {
		return nil, errors.New("Cursor 需要 CURSOR_API_KEY")
	}
	catalog, err := p.catalog(handshakeCtx, env)
	if err != nil {
		return nil, err
	}
	model, err := resolveModel(catalog, opts.Model, opts.ReasoningEffort, opts.ServiceTier)
	if err != nil {
		return nil, err
	}
	mcp, err := sdkMCPServers(opts.McpServers)
	if err != nil {
		return nil, err
	}
	proc, err := p.start(procCtx, opts.Cwd, env)
	if err != nil {
		return nil, err
	}
	s := newSession(opts, env, onEvent, proc, model)
	proc.serve(s.onEvent)
	if err := s.open(handshakeCtx, key, mcp); err != nil {
		_ = s.Close()
		return nil, errors.New(provider.RedactDiagnostic(err.Error(), env))
	}
	log.Printf("cursor: 已就绪 agent=%s model=%s transport=sdk", s.agentID, model.label())
	return s, nil
}

// start runs a bridge in dir. Nothing is read from it until serve.
func (p *sdkProvider) start(ctx context.Context, dir string, env []string) (*process, error) {
	node, err := exec.LookPath(p.node())
	if err != nil {
		return nil, fmt.Errorf("Cursor 需要 Node.js（%s）: %w", p.node(), err)
	}
	sdkDir := p.sdkDir(env)
	if _, err := os.Stat(filepath.Join(sdkDir, "node_modules", "@cursor", "sdk", "package.json")); err != nil {
		return nil, fmt.Errorf("沙箱镜像缺少 @cursor/sdk（%s）；请用包含 cursor 的 AGENT_PROVIDERS 重建镜像: %w", sdkDir, err)
	}
	script, err := writeBridge()
	if err != nil {
		return nil, err
	}
	env = envutil.Upsert(env, "GRASP_CURSOR_SDK_DIR", sdkDir)
	if dir == "" {
		dir = os.TempDir()
	}
	proc, err := startProcess(ctx, node, dir, env, []string{script})
	if err != nil {
		return nil, fmt.Errorf("start cursor sdk bridge: %w", err)
	}
	return proc, nil
}

var bridgeOnce struct {
	sync.Mutex
	path string
}

// writeBridge puts the embedded bridge on disk once per binary, named by its
// content so a newer backend never runs an older bridge.
func writeBridge() (string, error) {
	bridgeOnce.Lock()
	defer bridgeOnce.Unlock()
	if bridgeOnce.path != "" {
		if _, err := os.Stat(bridgeOnce.path); err == nil {
			return bridgeOnce.path, nil
		}
	}
	sum := sha256.Sum256(bridgeScript)
	dir := filepath.Join(os.TempDir(), "grasp-cursor-sdk")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "bridge-"+hex.EncodeToString(sum[:6])+".mjs")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, bridgeScript, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	bridgeOnce.path = path
	return path, nil
}

// modelCache keeps the account's model list per API key for modelCacheTTL.
type modelCache struct {
	mu     sync.Mutex
	key    string
	at     time.Time
	models []sdkModel
}

var modelCacheTTL = 30 * time.Minute

func (c *modelCache) get(key string, load func() ([]sdkModel, error)) ([]sdkModel, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.key == key && c.models != nil && time.Since(c.at) < modelCacheTTL {
		return c.models, nil
	}
	models, err := load()
	if err != nil {
		return nil, err
	}
	c.key, c.at, c.models = key, time.Now(), models
	return models, nil
}
