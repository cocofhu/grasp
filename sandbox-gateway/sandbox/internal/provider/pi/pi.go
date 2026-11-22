// Package pi drives the Pi coding agent through `pi --mode rpc`: one
// long-lived process per session, commands on stdin and events on stdout as
// JSON lines. Input sent while a run is going is queued as a steer and joins
// before the next model call, abort stops a run without stopping the
// process, and the session file continues the conversation in a new process.
package pi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"backend/internal/provider"
	"backend/internal/provider/modelconnection"
)

// Config parameterizes the Pi provider.
type Config struct {
	Bin        string
	Runtime    string
	ConfigRoot string
	// SessionDir holds the session files (default: $HOME/.pi/sessions).
	SessionDir string
	AuthEnvFn  func(env []string) []string
	Catalog    []provider.Model
}

type piProvider struct{ c Config }

// New returns the Pi RPC provider.
func New(c Config) provider.Provider { return piProvider{c: c} }

func (piProvider) Name() provider.Name                                    { return provider.Pi }
func (p piProvider) Runtime() string                                      { return p.c.Runtime }
func (p piProvider) DefaultConfigRoot() string                            { return p.c.ConfigRoot }
func (piProvider) Transport() provider.TransportKind                      { return provider.LongLived }
func (p piProvider) ListModels(context.Context) ([]provider.Model, error) { return p.c.Catalog, nil }

func (p piProvider) AuthEnv(env []string) []string {
	if p.c.AuthEnvFn == nil {
		return env
	}
	return p.c.AuthEnvFn(env)
}

// thinkingLevels are the levels `--thinking` accepts.
var thinkingLevels = map[string]bool{"off": true, "minimal": true, "low": true, "medium": true, "high": true, "xhigh": true}

// Open starts `pi --mode rpc` on the session file (the resumed one when it
// still exists, otherwise a new one) and reads back the session it opened.
func (p piProvider) Open(procCtx, handshakeCtx context.Context, opts provider.OpenOptions,
	onEvent func(json.RawMessage), _ provider.PermissionChooser) (provider.Session, error) {
	prepared, err := modelconnection.Prepare(p.Name(), p.AuthEnv(os.Environ()), opts.Model, p.c.ConfigRoot)
	if err != nil {
		return nil, err
	}
	if err := prepared.ValidateOptions(p.Name(), opts); err != nil {
		prepared.Cleanup()
		return nil, err
	}
	path, err := exec.LookPath(p.c.Bin)
	if err != nil {
		prepared.Cleanup()
		return nil, fmt.Errorf("agent %q not found on PATH (sandbox image lacks this CLI; rebuild it with AGENT_PROVIDERS including %q): %w",
			p.c.Bin, provider.Pi, err)
	}
	sessionFile, err := p.sessionFile(opts.ResumeSessionID)
	if err != nil {
		prepared.Cleanup()
		return nil, err
	}
	args := []string{"--mode", "rpc", "--session", sessionFile}
	if model := strings.TrimSpace(prepared.Model); model != "" && model != "auto" {
		args = append(args, "--model", model)
	}
	if effort := strings.TrimSpace(opts.ReasoningEffort); thinkingLevels[effort] {
		args = append(args, "--thinking", effort)
	}
	args = append(append(args, prepared.Args...), opts.CustomArgs...)
	s := newSession(opts, prepared.Env, onEvent, prepared.Cleanup)
	if s.proc, err = startProcess(procCtx, path, opts.Cwd, prepared.Env, args); err != nil {
		prepared.Cleanup()
		return nil, fmt.Errorf("start pi rpc: %w", err)
	}
	s.proc.serve(s.onEvent)
	if err := s.handshake(handshakeCtx); err != nil {
		_ = s.Close()
		return nil, errors.New(provider.RedactDiagnostic(err.Error(), prepared.Env))
	}
	log.Printf("pi: 已就绪 session=%s bin=%s transport=rpc", s.sessionFile, path)
	return s, nil
}

// sessionFile is the resumed session file when it still exists, otherwise a
// new path; Pi creates the file on its first message.
func (p piProvider) sessionFile(resumeID string) (string, error) {
	if resumeID != "" {
		if _, err := os.Stat(resumeID); err == nil {
			return resumeID, nil
		}
		log.Printf("pi: 会话文件 %s 已不存在，改开新会话", resumeID)
	}
	dir := p.c.SessionDir
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".pi", "sessions")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, time.Now().UTC().Format("20060102T150405.000000000")+".jsonl"), nil
}
