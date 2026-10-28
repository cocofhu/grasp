package sandbox

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// runtimeReprime is the in-sandbox entry point that swaps a running sandbox
// over to another node's env / config home / credentials (shared run sandbox
// mode). See sandbox-gateway/sandbox/scripts/reprime.sh for the bundle layout.
const runtimeReprime = "/opt/grasp-runtime/current/scripts/reprime.sh"

// reprimeTimeout covers cloning repos a previous node did not have.
var reprimeTimeout = 5 * time.Minute

var envNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// reprimeSkipEnv are create-time plumbing keys that make no sense to re-apply
// to a running sandbox.
var reprimeSkipEnv = map[string]bool{
	"SANDBOX_INJECT":         true,
	"SANDBOX_INJECT_HEADERS": true,
	"GRASP_RUNTIME_URL":      true,
	"SSH_KEY":                true,
}

// Explicit bindings never inherit the legacy environment's Git identity.
var gitCredentialEnv = map[string]bool{
	"GITHUB_TOKEN": true, "GH_TOKEN": true, "GITHUB_ENTERPRISE_TOKEN": true, "GH_ENTERPRISE_TOKEN": true,
	"GITLAB_TOKEN": true, "GLAB_TOKEN": true, "GITLAB_ACCESS_TOKEN": true, "GITHUB_URL": true, "GITLAB_URL": true,
	"GIT_SSH_PRIVATE_KEY": true, "GIT_SSH_KNOWN_HOSTS": true,
	"GIT_SSH_COMMAND": true, "GIT_SSH": true, "SSH_AUTH_SOCK": true,
}

// ReprimeEnvKeys lists the spec env names Reprime applies, so the caller can
// hand them back as prevKeys on the next Reprime.
func ReprimeEnvKeys(spec Spec) []string {
	keys := make([]string, 0, len(spec.Env))
	for k := range spec.Env {
		if envNameRE.MatchString(k) && !reprimeSkipEnv[k] && !(spec.GitBindingsConfigured && gitCredentialEnv[k]) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// Reprime re-initializes a running sandbox for spec: prevKeys (the previous
// spec's env names) are dropped, spec's env, config home and Git/SSH
// credentials are applied, and the backend is restarted. The workspace and
// cloned repos are kept. Callers reconnect ACP afterwards.
func (m *Manager) Reprime(ctx context.Context, sb *Sandbox, spec Spec, prevKeys []string) error {
	if sb == nil {
		return fmt.Errorf("reprime: no sandbox")
	}
	bundle, err := packReprimeBundle(spec, prevKeys)
	if err != nil {
		return fmt.Errorf("reprime: %w", err)
	}
	cmd, err := newSafeCmd("bash", runtimeReprime)
	if err != nil {
		return err
	}
	out, err := sb.creds().runInput(ctx, reprimeTimeout, cmd, bytes.NewReader(bundle))
	if err != nil {
		return fmt.Errorf("reprime: %w: %s", err, lastLine(string(out)))
	}
	if spec.ConfigRoot != "" {
		sb.ConfigRoot = spec.ConfigRoot
	}
	log.Info().Str("sandbox", sb.ID).Str("provider", spec.Env["AGENT_PROVIDER"]).
		Msg("sandbox re-primed for next node")
	return nil
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return s[i+1:]
	}
	return s
}

// packReprimeBundle builds reprime.sh's stdin: env (NAME=base64 lines),
// unset, config/ and ssh/.
func packReprimeBundle(spec Spec, prevKeys []string) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	add := func(name string, body []byte, mode int64) error {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		_, err := tw.Write(body)
		return err
	}

	keys := ReprimeEnvKeys(spec)
	var env strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&env, "%s=%s\n", k, base64.StdEncoding.EncodeToString([]byte(spec.Env[k])))
	}
	if err := add("env", []byte(env.String()), 0o600); err != nil {
		return nil, err
	}
	next := map[string]bool{}
	for _, k := range keys {
		next[k] = true
	}
	var unset strings.Builder
	for _, k := range prevKeys {
		if envNameRE.MatchString(k) && !next[k] && !reprimeSkipEnv[k] {
			unset.WriteString(k + "\n")
		}
	}
	if unset.Len() > 0 {
		if err := add("unset", []byte(unset.String()), 0o600); err != nil {
			return nil, err
		}
	}
	if home := strings.TrimSpace(spec.ConfigHome); home != "" {
		if err := addDirToTar(tw, home, "config"); err != nil {
			return nil, fmt.Errorf("pack config home: %w", err)
		}
	}
	sshData, err := packGitInjectBundle(spec.SSHPrivateKey, spec.SSHKnownHosts, spec.GitBindingsConfigured, spec.GitCredentials)
	if err != nil {
		return nil, fmt.Errorf("pack ssh bundle: %w", err)
	}
	if sshData != nil {
		if err := copyTarGzInto(tw, sshData, "ssh"); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// addDirToTar adds every regular file and dir under root as prefix/<rel>.
func addDirToTar(tw *tar.Writer, root, prefix string) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		name := prefix
		if rel != "." {
			name = prefix + "/" + filepath.ToSlash(rel)
		}
		if d.IsDir() {
			return tw.WriteHeader(&tar.Header{Name: name + "/", Mode: 0o755, Typeflag: tar.TypeDir})
		}
		if !d.Type().IsRegular() {
			return nil
		}
		body, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: int64(fi.Mode().Perm()), Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		_, err = tw.Write(body)
		return err
	})
}

// copyTarGzInto re-roots every entry of a .tar.gz under prefix/.
func copyTarGzInto(tw *tar.Writer, data []byte, prefix string) error {
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer func() { _ = gr.Close() }()
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		hdr.Name = prefix + "/" + strings.TrimPrefix(hdr.Name, "./")
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err := io.Copy(tw, tr); err != nil {
			return err
		}
	}
}

// SpecFingerprint hashes everything Reprime would apply for spec (env, config
// home contents, SSH and repo credentials). Equal fingerprints mean a running
// sandbox needs no re-prime. Empty on a config home that cannot be read.
func SpecFingerprint(spec Spec) string {
	// HMAC keeps this deterministic change detector out of password-hashing
	// code paths while still making the fingerprint opaque to its inputs.
	h := hmac.New(sha256.New, []byte("grasp-reprime-spec-fingerprint-v1"))
	for _, k := range ReprimeEnvKeys(spec) {
		_, _ = fmt.Fprintf(h, "env\x00%s\x00%s\x00", k, spec.Env[k])
	}
	_, _ = fmt.Fprintf(h, "root\x00%s\x00ssh\x00%s\x00%s\x00", spec.ConfigRoot, spec.SSHPrivateKey, spec.SSHKnownHosts)
	_, _ = fmt.Fprintf(h, "git-configured\x00%t\x00", spec.GitBindingsConfigured)
	for _, credential := range spec.GitCredentials {
		_, _ = fmt.Fprintf(h, "git\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%t\x00",
			credential.ID, credential.Name, credential.Kind, credential.Scope, credential.Secret, credential.KnownHosts, credential.Preferred)
	}
	if home := strings.TrimSpace(spec.ConfigHome); home != "" {
		err := filepath.WalkDir(home, func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if !d.Type().IsRegular() {
				return nil
			}
			rel, _ := filepath.Rel(home, p)
			body, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(h, "file\x00%s\x00%d\x00", filepath.ToSlash(rel), len(body))
			_, _ = h.Write(body)
			return nil
		})
		if err != nil {
			return ""
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// SyncBranch switches the repo at dir to branch for the next node of a shared
// run sandbox. A repo already on branch, missing, or with uncommitted changes
// (the previous node's work) is left alone; the returned note says why.
func (s *Sandbox) SyncBranch(ctx context.Context, dir, branch string) (string, error) {
	if err := validateShellArg(dir); err != nil {
		return "", err
	}
	if err := validateShellArg(branch); err != nil {
		return "", err
	}
	script := `d=` + shellQuote(dir) + `; b=` + shellQuote(branch) + `
cd "$d" 2>/dev/null || { echo missing; exit 0; }
git rev-parse --git-dir >/dev/null 2>&1 || { echo missing; exit 0; }
[ "$(git rev-parse --abbrev-ref HEAD 2>/dev/null)" = "$b" ] && { echo current; exit 0; }
[ -n "$(git status --porcelain 2>/dev/null)" ] && { echo dirty; exit 0; }
git fetch origin "$b" >/dev/null 2>&1 || true
git checkout "$b" >/dev/null 2>&1 && echo switched || echo absent
`
	return s.ExecScript(ctx, 60*time.Second, "bash", script)
}
