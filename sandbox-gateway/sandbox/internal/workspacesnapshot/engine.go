package workspacesnapshot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"backend/internal/workspacebinding"
	"backend/internal/workspacejob"
)

const nofollow = syscall.O_NOFOLLOW | syscall.O_NONBLOCK

type Engine struct {
	config  Config
	root    *os.Root
	policy  string
	binding *workspacebinding.Manager
}

func New(config Config) (*Engine, error) {
	if config.Limits == (Limits{}) {
		config.Limits = DefaultLimits()
	}
	if !validLimits(config.Limits) {
		return nil, ErrLimit
	}
	config.PrivatePaths = slices.Clone(config.PrivatePaths)
	slices.Sort(config.PrivatePaths)
	for _, name := range config.PrivatePaths {
		if !safePath(name) || name == "." {
			return nil, ErrUnsafe
		}
	}
	root, err := filepath.Abs(config.Root)
	if err != nil || config.Root == "" {
		return nil, ErrUnsafe
	}
	config.Root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	if config.GitBinary == "" {
		config.GitBinary, err = exec.LookPath("git")
		if err != nil {
			return nil, err
		}
	}
	config.GitBinary, err = filepath.Abs(config.GitBinary)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(struct {
		Version string
		Limits  Limits
		Private []string
	}{PolicyVersion, config.Limits, config.PrivatePaths})
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(data)
	engine := &Engine{config: config, policy: hex.EncodeToString(hash[:])}
	engine.root, err = os.OpenRoot(config.Root)
	if err != nil {
		return nil, err
	}
	return engine, nil
}

func (e *Engine) Close() error { return e.root.Close() }

func (e *Engine) NewSpec(paths, ignored []string) (Spec, error) {
	spec := Spec{PolicyVersion: PolicyVersion, PolicySHA256: e.policy, Limits: e.config.Limits, Paths: paths, IncludeIgnored: ignored}
	if e.binding != nil {
		view, err := e.binding.Head()
		if err != nil {
			return Spec{}, err
		}
		if !view.Ready {
			return Spec{}, workspacebinding.ErrUnknown
		}
		spec.Binding = &view.Head.Reference
	}
	return normalizeSpec(spec, e.config.PrivatePaths)
}

func (e *Engine) decode(body json.RawMessage) (Spec, error) {
	if len(body) > 64<<10 {
		return Spec{}, ErrLimit
	}
	var spec Spec
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if err := d.Decode(&spec); err != nil {
		return spec, ErrUnsafe
	}
	var more any
	if err := d.Decode(&more); !errors.Is(err, io.EOF) || spec.PolicySHA256 != e.policy || spec.Limits != e.config.Limits {
		return spec, ErrUnsafe
	}
	return normalizeSpec(spec, e.config.PrivatePaths)
}

func (e *Engine) Validate(body json.RawMessage) error {
	_, err := e.decode(body)
	return err
}

func (e *Engine) Executors() map[string]workspacejob.Executor {
	return map[string]workspacejob.Executor{"capture_files": e.captureCurrent}
}

func (e *Engine) captureCurrent(ctx context.Context, h *workspacejob.Handle, body json.RawMessage) (json.RawMessage, error) {
	if e.binding == nil {
		return e.capture(ctx, h, body)
	}
	spec, err := e.decode(body)
	if err != nil {
		return nil, err
	}
	view, release, err := e.binding.Acquire(spec.Binding, true)
	if err != nil {
		return nil, err
	}
	defer release()
	copy := *e
	copy.binding = nil
	copy.config.Root = view.Path
	copy.root, err = os.OpenRoot(view.Path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = copy.root.Close() }()
	return copy.capture(ctx, h, body)
}

func descriptor(f *os.File) string {
	return fmt.Sprintf("/proc/%d/fd/%d", os.Getpid(), f.Fd())
}

type boundedOutput struct {
	writer io.Writer
	left   int64
}

func (w *boundedOutput) Write(p []byte) (int, error) {
	if int64(len(p)) > w.left {
		return 0, ErrLimit
	}
	n, err := w.writer.Write(p)
	w.left -= int64(n)
	return n, err
}

func (e *Engine) git(ctx context.Context, h *workspacejob.Handle, dir string, input io.Reader, output io.Writer, extraEnv []string, args ...string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=/nonexistent", "LC_ALL=C", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_NO_REPLACE_OBJECTS=1", "GIT_NO_LAZY_FETCH=1", "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0"}
	env = append(env, extraEnv...)
	base := []string{e.config.GitBinary, "-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false", "-c", "core.attributesFile=/dev/null", "-c", "core.excludesFile=/dev/null", "-c", "submodule.recurse=false", "-c", "gc.auto=0", "-c", "maintenance.auto=false", "-c", "protocol.allow=never", "-c", "pack.threads=1", "-c", "uploadpack.packObjectsHook="}
	var result bytes.Buffer
	if output == nil {
		output = &boundedOutput{writer: &result, left: min(e.config.Limits.MaxManifestBytes, 64<<20)}
	}
	var stderr bytes.Buffer
	err := h.Run(workspacejob.Process{Dir: dir, Env: env, Input: input, Stdout: output, Stderr: &boundedOutput{writer: &stderr, left: 16 << 10}, Argv: append(base, args...)})
	if err != nil {
		// Raw Git diagnostics can disclose private source paths/config. Keep
		// them out of durable receipts and browser messages.
		return nil, fmt.Errorf("workspace Git operation failed: %w", err)
	}
	return result.Bytes(), nil
}

func readSmall(root *os.Root, name string, maxBytes int64) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxBytes {
		return nil, ErrUnsafe
	}
	f, err := root.OpenFile(name, os.O_RDONLY|nofollow, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	actual, err := f.Stat()
	if err != nil || !os.SameFile(info, actual) {
		return nil, ErrChanged
	}
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if int64(len(data)) > maxBytes {
		return nil, ErrLimit
	}
	return data, err
}

func writePrivate(h *workspacejob.Handle, root *os.Root, name string, data []byte) error {
	if !safePath(name) || name == "." {
		return ErrUnsafe
	}
	return h.Guard(func() error {
		if err := root.MkdirAll(filepath.ToSlash(filepath.Dir(name)), 0700); err != nil {
			return err
		}
		f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY|nofollow, 0600)
		if err != nil {
			return err
		}
		_, err = f.Write(data)
		if err == nil {
			err = f.Sync()
		}
		return errors.Join(err, f.Close())
	})
}

func nativeRelative(root, name string) (string, error) {
	if !filepath.IsAbs(name) {
		return "", ErrUnsafe
	}
	rel, err := filepath.Rel(root, filepath.Clean(name))
	if err != nil || !safePath(filepath.ToSlash(rel)) || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrUnsafe
	}
	return filepath.ToSlash(rel), nil
}
