package driver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"path"
	"regexp"
	"strings"
)

var (
	ErrColdStartUnsupported = errors.New("cold configuration is unsupported by this environment")
	ErrColdStartConflict    = errors.New("cold configuration identity changed")
	ErrColdStartUnknown     = errors.New("cold configuration outcome requires read-only confirmation")
)

// ColdStartConfig replaces the entire authorization environment before an
// original, confirmed-stopped workload can execute. InputSHA256 identifies the
// original logical request; PayloadSHA256 freezes the actual bootstrap and URLs.
// Neither digest contains a secret in clear text.
type ColdStartConfig struct {
	ExpectedNativeIdentity string                `json:"expectedNativeIdentity"`
	RequestID              string                `json:"requestId"`
	InputSHA256            string                `json:"inputSha256"`
	PayloadSHA256          string                `json:"payloadSha256"`
	Env                    map[string]string     `json:"env"`
	Injections             []ColdConfigInjection `json:"injections"`
}

// ColdConfigInjection binds a short-lived archive to the exact authorized
// bytes. Only fresh private configuration and Git staging destinations qualify.
type ColdConfigInjection struct {
	URL         string `json:"url"`
	Destination string `json:"destination"`
	SHA256      string `json:"sha256"`
	Bytes       int64  `json:"bytes"`
}

var coldRequestID = regexp.MustCompile(`^[a-zA-Z0-9_-]{8,128}$`)
var coldEnvName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var coldBearerHeader = regexp.MustCompile(`^Authorization: Bearer [a-zA-Z0-9._~-]{1,256}$`)

func ColdSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && strings.ToLower(value) == value
}

// ColdStartPayloadSHA256 excludes its own digest; JSON sorts map keys.
func ColdStartPayloadSHA256(opts StartOptions) string {
	if opts.ColdConfig == nil {
		return ""
	}
	copy := *opts.ColdConfig
	copy.PayloadSHA256 = ""
	opts.ColdConfig = &copy
	data, err := json.Marshal(opts)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ValidateColdStartOptions performs no I/O and never includes inputs in errors.
func ValidateColdStartOptions(opts StartOptions) error {
	c := opts.ColdConfig
	if c == nil || !coldRequestID.MatchString(c.ExpectedNativeIdentity) || !coldRequestID.MatchString(c.RequestID) || !ColdSHA256(c.InputSHA256) || !ColdSHA256(c.PayloadSHA256) || ColdStartPayloadSHA256(opts) != c.PayloadSHA256 || !strings.HasPrefix(opts.BootstrapScript, "#!/bin/bash\n") || opts.RuntimeURL == "" || len(c.Env) == 0 || len(c.Injections) != 2 || !coldBearerHeader.MatchString(opts.RuntimeHeaders) {
		return ErrColdStartConflict
	}
	runtimeURL, err := url.Parse(opts.RuntimeURL)
	if err != nil || (runtimeURL.Scheme != "http" && runtimeURL.Scheme != "https") || runtimeURL.Host == "" || runtimeURL.User != nil || strings.ContainsAny(opts.RuntimeURL, "\"\\\r\n\x00") {
		return ErrColdStartConflict
	}
	for key, value := range c.Env {
		if !coldEnvName.MatchString(key) || strings.ContainsRune(value, 0) || len(key)+len(value) > 128<<10 {
			return ErrColdStartConflict
		}
	}
	root := c.Env["CONFIG_ROOT"]
	if !strings.HasPrefix(root, "/root/") || root != path.Clean(root) || root == "/root/workspace" || strings.HasPrefix(root, "/root/workspace/") || strings.ContainsAny(root, "\r\n\x00") || c.Env["GIT_REPOS"] != "" || c.Env["ROOT_PASSWORD"] == "" || c.Env["ACP_BRIDGE_PASSWORD"] == "" || c.Env["GRASP_RUNTIME_URL"] != opts.RuntimeURL || c.Env["SANDBOX_INJECT_HEADERS"] != opts.RuntimeHeaders || c.Env["SANDBOX_INJECT"] != "" {
		return ErrColdStartConflict
	}
	seen := map[string]bool{}
	for _, p := range c.Injections {
		u, err := url.Parse(p.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || strings.ContainsAny(p.URL, "\r\n\x00") || !ColdSHA256(p.SHA256) || p.Bytes < 1 || p.Bytes > 64<<20 || (p.Destination != root && p.Destination != "/tmp/grasp-ssh-inject") || seen[p.Destination] {
			return ErrColdStartConflict
		}
		seen[p.Destination] = true
	}
	if !seen[root] || !seen["/tmp/grasp-ssh-inject"] {
		return ErrColdStartConflict
	}
	return nil
}

// ColdStartReceipt is safe metadata, not permission to start or apply a grant.
// NativeIdentity is the immutable provider resource UID/container ID. Unknown
// receipts are only reconciled with GetColdStart; they never cause another start.
type ColdStartReceipt struct {
	RequestID      string `json:"requestId"`
	InputSHA256    string `json:"inputSha256"`
	PayloadSHA256  string `json:"payloadSha256"`
	NativeIdentity string `json:"nativeIdentity"`
	WorkspaceDir   string `json:"workspaceDir"`
	State          string `json:"state"`
}

// ColdConfiguredStarter is deliberately optional. Memory-resume providers must
// not resume old RAM in order to replace credentials after execution has begun.
type ColdConfiguredStarter interface {
	PreviewColdStart(context.Context, string) (*ColdStartTarget, error)
	StartConfigured(context.Context, string, StartOptions) (*ColdStartReceipt, error)
	GetColdStart(context.Context, string, string) (*ColdStartReceipt, error)
}

type ColdStartTarget struct {
	NativeIdentity string `json:"nativeIdentity"`
	WorkspaceDir   string `json:"workspaceDir"`
}
