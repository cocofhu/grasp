package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"backend/internal/workspacebinding"
	"backend/internal/workspacesnapshot"
)

type runtimeWorkspaceConfiguration struct {
	Version     int                      `json:"version"`
	Resource    string                   `json:"resource"`
	Lineage     string                   `json:"lineage"`
	ControlRoot string                   `json:"controlRoot"`
	RestoreRoot string                   `json:"restoreRoot"`
	Limits      workspacesnapshot.Limits `json:"limits"`
}
type runtimeWorkspaceConfigurationReceipt struct {
	Version     int    `json:"version"`
	SHA256      string `json:"sha256"`
	Incarnation string `json:"incarnation"`
	State       string `json:"state"`
}
type runtimeWorkspaceConfigurer struct {
	runDir  string
	getenv  func(string) string
	restart func(context.Context) error
	verify  func(context.Context, runtimeWorkspaceConfiguration) error
}

func configureWorkspaceRuntime(input io.Reader) error {
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	services := filepath.Join(filepath.Dir(bin), "..", "scripts", "services.sh")
	options := runtimeWorkspaceConfigurer{runDir: "/run/grasp-runtime", getenv: os.Getenv}
	options.restart = func(ctx context.Context) error {
		cmd := exec.CommandContext(ctx, "bash", services, "restart", "backend")
		// These are runtime installation paths, never a tool's env override.
		for _, item := range os.Environ() {
			key, _, _ := strings.Cut(item, "=")
			if key != "GRASP_RUNTIME_RUN_DIR" && key != "GRASP_RUNTIME_BIN_DIR" && key != "GRASP_RUNTIME_SHARE_DIR" {
				cmd.Env = append(cmd.Env, item)
			}
		}
		cmd.Env = append(cmd.Env, "GRASP_RUNTIME_RUN_DIR=/run/grasp-runtime")
		return cmd.Run()
	}
	options.verify = options.observe
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	return options.configure(ctx, input)
}

func decodeWorkspaceConfiguration(input io.Reader) (runtimeWorkspaceConfiguration, string, error) {
	var cfg runtimeWorkspaceConfiguration
	data, err := io.ReadAll(io.LimitReader(input, (64<<10)+1))
	if err != nil {
		return cfg, "", err
	}
	if len(data) > 64<<10 {
		return cfg, "", workspacebinding.ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&cfg) != nil {
		return cfg, "", workspacebinding.ErrInvalid
	}
	var extra any
	if !errors.Is(d.Decode(&extra), io.EOF) {
		return cfg, "", workspacebinding.ErrInvalid
	}
	limits, _ := json.Marshal(cfg.Limits)
	if _, err := workspaceRuntimeLimits(string(limits)); err != nil {
		return cfg, "", err
	}
	if cfg.Version != 1 || !workspaceRuntimeID.MatchString(cfg.Resource) || !workspaceRuntimeID.MatchString(cfg.Lineage) || !filepath.IsAbs(cfg.ControlRoot) || !filepath.IsAbs(cfg.RestoreRoot) || filepath.Clean(cfg.ControlRoot) != cfg.ControlRoot || filepath.Clean(cfg.RestoreRoot) != cfg.RestoreRoot || filepath.Base(cfg.ControlRoot) != "control" || filepath.Base(cfg.RestoreRoot) != "restores" || filepath.Dir(cfg.ControlRoot) != filepath.Dir(cfg.RestoreRoot) || filepath.Base(filepath.Dir(cfg.ControlRoot)) != cfg.Resource {
		return cfg, "", workspacebinding.ErrInvalid
	}
	canonical, _ := json.Marshal(cfg)
	sum := sha256.Sum256(canonical)
	return cfg, hex.EncodeToString(sum[:]), nil
}

func privateRuntimeDirectory(name string) error {
	if err := os.MkdirAll(name, 0700); err != nil {
		return err
	}
	actual, err := filepath.EvalSymlinks(name)
	if err != nil || actual != filepath.Clean(name) {
		return workspacebinding.ErrInvalid
	}
	info, err := os.Lstat(name)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return workspacebinding.ErrInvalid
	}
	return nil
}
func atomicRuntimeFile(name string, data []byte) error {
	dir := filepath.Dir(name)
	if err := privateRuntimeDirectory(dir); err != nil {
		return err
	}
	if info, err := os.Lstat(name); err == nil && (!info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0) {
		return workspacebinding.ErrInvalid
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	f, err := os.CreateTemp(dir, ".workspace-config-")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if err = errors.Join(err, f.Close()); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), name); err != nil {
		return err
	}
	parent, err := os.Open(dir)
	if err != nil {
		return err
	}
	return errors.Join(parent.Sync(), parent.Close())
}
func runtimeIncarnation(runDir string) (string, error) {
	pid, err := os.ReadFile(filepath.Join(runDir, "backend.pid"))
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(pid))
	if value == "" || strings.Trim(value, "0123456789") != "" {
		return "", workspacebinding.ErrInvalid
	}
	stat, err := os.ReadFile(filepath.Join("/proc", value, "stat"))
	if err != nil {
		return "", err
	}
	_, tail, ok := strings.Cut(string(stat), ") ")
	if !ok {
		return "", workspacebinding.ErrInvalid
	}
	fields := strings.Fields(tail)
	if len(fields) < 20 {
		return "", workspacebinding.ErrInvalid
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(boot)) + ":" + value + ":" + fields[19], nil
}
func runtimeQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

func (c runtimeWorkspaceConfigurer) configure(ctx context.Context, input io.Reader) error {
	cfg, digest, err := decodeWorkspaceConfiguration(input)
	if err != nil {
		return err
	}
	if c.getenv == nil || c.restart == nil || c.verify == nil || !filepath.IsAbs(c.runDir) {
		return workspacebinding.ErrInvalid
	}
	if strings.TrimSpace(c.getenv("GRASP_EXECUTION_LINEAGE")) != cfg.Lineage || strings.TrimSpace(c.getenv("ACP_BRIDGE_PASSWORD")) == "" {
		return workspacebinding.ErrInvalid
	}
	if old := c.getenv("GRASP_SANDBOX_RESOURCE_ID"); old != "" && old != cfg.Resource {
		return workspacebinding.ErrConflict
	}
	original, err := workspacebinding.StartupOriginalRoot(filepath.Join(cfg.ControlRoot, "workspace-bindings"), cfg.RestoreRoot, c.getenv("WORKSPACE_DIR"), cfg.Resource, cfg.Lineage)
	if err != nil {
		return err
	}
	if pathsOverlap(cfg.ControlRoot, original) || pathsOverlap(cfg.RestoreRoot, original) {
		return workspacebinding.ErrInvalid
	}
	for _, root := range []string{filepath.Dir(cfg.ControlRoot), cfg.ControlRoot, cfg.RestoreRoot, c.runDir} {
		if err := privateRuntimeDirectory(root); err != nil {
			return err
		}
	}
	lock, err := os.OpenFile(filepath.Join(cfg.ControlRoot, ".configuration.lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return workspacebinding.ErrBusy
	}
	incarnation, err := runtimeIncarnation(c.runDir)
	if err != nil {
		return err
	}
	receiptPath := filepath.Join(cfg.ControlRoot, "configuration.json")
	var prior runtimeWorkspaceConfigurationReceipt
	data, readErr := os.ReadFile(receiptPath)
	if readErr == nil {
		if len(data) > 64<<10 || json.Unmarshal(data, &prior) != nil || prior.Version != 1 || prior.SHA256 != digest || (prior.State != "prepared" && prior.State != "confirmed") {
			return workspacebinding.ErrConflict
		}
		if err := c.verify(ctx, cfg); err == nil {
			if prior.State == "prepared" {
				prior.State = "confirmed"
				data, _ := json.Marshal(prior)
				return atomicRuntimeFile(receiptPath, data)
			}
			return nil
		}
		// The same process/tuple is an uncertain original configuration. It
		// may only be observed again; no second backend restart is issued.
		if prior.Incarnation == incarnation || c.getenv("GRASP_SANDBOX_RESOURCE_ID") != "" {
			return workspacebinding.ErrUnknown
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	receipt := runtimeWorkspaceConfigurationReceipt{1, digest, incarnation, "prepared"}
	data, _ = json.Marshal(receipt)
	if err := atomicRuntimeFile(receiptPath, data); err != nil {
		return err
	}
	limits, _ := json.Marshal(cfg.Limits)
	oldEnv, err := os.ReadFile(filepath.Join(c.runDir, "env"))
	if err != nil {
		return err
	}
	if len(oldEnv) > 4<<20 {
		return workspacebinding.ErrInvalid
	}
	var next bytes.Buffer
	next.Write(oldEnv)
	next.WriteByte('\n')
	for _, pair := range [][2]string{{"GRASP_SANDBOX_RESOURCE_ID", cfg.Resource}, {"GRASP_EXECUTION_LINEAGE", cfg.Lineage}, {"GRASP_WORKSPACE_CONTROL_ROOT", cfg.ControlRoot}, {"GRASP_WORKSPACE_RESTORE_ROOT", cfg.RestoreRoot}, {"GRASP_WORKSPACE_FILE_LIMITS", string(limits)}} {
		fmt.Fprintf(&next, "export %s=%s\n", pair[0], runtimeQuote(pair[1]))
	}
	if err := atomicRuntimeFile(filepath.Join(c.runDir, "env"), next.Bytes()); err != nil {
		return err
	}
	if err := c.restart(ctx); err != nil {
		return workspacebinding.ErrUnknown
	}
	if err := c.verify(ctx, cfg); err != nil {
		return workspacebinding.ErrUnknown
	}
	receipt.State = "confirmed"
	data, _ = json.Marshal(receipt)
	return atomicRuntimeFile(receiptPath, data)
}

func (c runtimeWorkspaceConfigurer) observe(ctx context.Context, cfg runtimeWorkspaceConfiguration) error {
	port := c.getenv("ACP_BRIDGE_PORT")
	if port == "" {
		port = "8765"
	}
	if strings.Trim(port, "0123456789") != "" {
		return workspacebinding.ErrInvalid
	}
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Timeout: 3 * time.Second, Jar: jar}
	base := "http://127.0.0.1:" + port
	login, _ := json.Marshal(struct {
		Password string `json:"password"`
	}{c.getenv("ACP_BRIDGE_PASSWORD")})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/login", bytes.NewReader(login))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return workspacebinding.ErrUnknown
	}
	read := func(path string, out any) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
		if err != nil {
			return err
		}
		response, err := client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = response.Body.Close() }()
		if response.StatusCode != http.StatusOK {
			return workspacebinding.ErrUnknown
		}
		return json.NewDecoder(io.LimitReader(response.Body, 128<<10)).Decode(out)
	}
	var caps workspacebinding.Capabilities
	if err := read("/api/workspace/binding/capabilities", &caps); err != nil {
		return err
	}
	if caps.Version != 1 || caps.Resource != cfg.Resource || caps.Lineage != cfg.Lineage || !caps.Activate || !caps.Return || !caps.DurableHead || caps.ProcessMemory {
		return workspacebinding.ErrUnknown
	}
	var head workspacebinding.View
	if err := read("/api/workspace/binding/head", &head); err != nil {
		return err
	}
	if head.Head.Resource != cfg.Resource || head.Head.Lineage != cfg.Lineage || (!head.Ready && head.PendingOperationID == "") {
		return workspacebinding.ErrUnknown
	}
	var files workspacesnapshot.FileCapabilities
	if err := read("/api/workspace/files/capabilities", &files); err != nil {
		return err
	}
	if files.Limits != cfg.Limits {
		return workspacebinding.ErrConflict
	}
	return nil
}
