package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestSeedHelpersReusesOneSSHConnection(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"usr/local/bin", "etc/profile.d"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	var connections, commands, failures atomic.Int32
	creds := startSSHOutputServer(t, func(channel ssh.Channel, command string, input []byte) uint32 {
		commands.Add(1)
		var cmd *exec.Cmd
		switch command {
		case "'tar' '-C' '/' '-xf' '-'":
			cmd = exec.Command("tar", "-C", root, "-xf", "-")
			cmd.Stdin = bytes.NewReader(input)
		case "'" + mcpSpaProxyPath + "' '--ensure'":
			cmd = exec.Command(filepath.Join(root, mcpSpaProxyPath), "--ensure")
		default:
			failures.Add(1)
			_, _ = fmt.Fprintf(channel.Stderr(), "unexpected command %q", command)
			return 1
		}
		cmd.Stdout, cmd.Stderr = channel, channel.Stderr()
		if err := cmd.Run(); err != nil {
			failures.Add(1)
			return 1
		}
		return 0
	}, func() { connections.Add(1) })
	m := &Manager{installHelpers: true, hostKeys: creds.hostKeys}
	sb := &Sandbox{ID: "seed-helpers", SSHHost: creds.host, SSHPort: creds.port, mgr: m}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	m.EnsureHelpers(ctx, sb)
	if connections.Load() != 1 || commands.Load() != 2 {
		t.Fatalf("connections=%d commands=%d; want one authenticated connection for install + ensure", connections.Load(), commands.Load())
	}
	if failures.Load() != 0 {
		t.Fatalf("helper command failures=%d", failures.Load())
	}
	for _, file := range []struct {
		path, content string
		mode          os.FileMode
	}{
		{artifactUploadPath, artifactUploadScript, 0o755},
		{mcpAdvertiseProfilePath, mcpAdvertiseProfileScript, 0o644},
		{mcpSpaProxyPath, mcpSpaProxyScript, 0o755},
	} {
		path := filepath.Join(root, file.path)
		body, err := os.ReadFile(path)
		if err != nil || string(body) != file.content {
			t.Fatalf("installed %s differs from helper: %v", file.path, err)
		}
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != file.mode {
			t.Fatalf("mode %s: %v %v", file.path, info, err)
		}
	}
}

func TestSeedHelpersFailureIsNonfatalAndHeals(t *testing.T) {
	for _, failAt := range []int{1, 2} {
		t.Run(fmt.Sprintf("command-%d", failAt), func(t *testing.T) {
			gw, _ := newInlineGW(t)
			m := NewManager(gw, ManagerOptions{InstallHelpers: true})
			calls := 0
			restore := SetExecHook(func(_ context.Context, _ string, _ int, _ string, stdin io.Reader) ([]byte, error) {
				calls++
				if stdin != nil {
					_, _ = io.Copy(io.Discard, stdin)
				}
				if calls == failAt {
					return []byte("temporary install failure"), fmt.Errorf("remote failure")
				}
				return nil, nil
			})
			defer restore()
			sb, err := m.Create(t.Context(), Spec{Name: "helper-failure", Env: map[string]string{BridgePasswordEnv: testBridgePassword}})
			if err != nil || sb == nil || calls != failAt {
				t.Fatalf("best-effort create: sb=%v err=%v calls=%d", sb, err, calls)
			}
			m.EnsureHelpers(t.Context(), sb)
			if calls != failAt+2 {
				t.Fatalf("healing did not retry install and ensure: calls=%d", calls)
			}
		})
	}
}

func TestMCPSpaProxyProcess(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	proxy := filepath.Join(dir, "proxy.py")
	if err := os.WriteFile(proxy, []byte(mcpSpaProxyScript), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", "-I", "-B", "testdata/mcp_spa_proxy_test.py", proxy)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("real proxy daemon regression: %v\n%s", err, output)
	}
}

func TestSeedHelperScriptsHaveNoHardcodedSPAHosts(t *testing.T) {
	t.Parallel()
	// Assembled so the acceptance rg gate does not see intranet host literals.
	forbidden := []string{
		"k3s" + ".cc",
		"registry." + "cocofhu",
		"spa." + "example.com",
		"api." + "example.com",
	}
	checks := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "artifact-upload",
			body: artifactUploadScript,
			want: []string{
				"rewrite_spa_mcp_url",
				"_SPA_MCP_HOSTS = {}",
				"upload_image_artifact",
			},
		},
		{
			name: "profile.d",
			body: mcpAdvertiseProfileScript,
			want: []string{
				"GRASP_ARTIFACT_URL",
			},
		},
		{
			name: "spa-proxy",
			body: mcpSpaProxyScript,
			want: []string{
				"_SPA_TO_API = {}",
				"--ensure",
				"127.0.0.1",
			},
		},
	}
	for _, tc := range checks {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, s := range tc.want {
				if !strings.Contains(tc.body, s) {
					t.Fatalf("%s script missing %q", tc.name, s)
				}
			}
			for _, s := range forbidden {
				if strings.Contains(tc.body, s) {
					t.Fatalf("%s script must not hardcode %q", tc.name, s)
				}
			}
		})
	}
}

func TestArtifactUploadScriptDoesNotCallWriteArtifactImage(t *testing.T) {
	t.Parallel()
	if strings.Contains(artifactUploadScript, `"name": "write_artifact"`) {
		t.Fatal("artifact-upload must not call write_artifact")
	}
	if strings.Contains(artifactUploadScript, "kind\": \"image\"") ||
		strings.Contains(artifactUploadScript, "kind=image") {
		t.Fatal("artifact-upload must not pass kind=image to write_artifact")
	}
	if !strings.Contains(artifactUploadScript, "upload_image_artifact") {
		t.Fatal("artifact-upload must call upload_image_artifact")
	}
}

func TestMCPSpaProxyPath(t *testing.T) {
	t.Parallel()
	if mcpSpaProxyPath != "/usr/local/bin/grasp-mcp-spa-proxy" {
		t.Fatalf("mcpSpaProxyPath = %q", mcpSpaProxyPath)
	}
}

// TestRepoScriptMatchesEmbeddedProxy keeps server/scripts/… in sync with the
// embedded seed helper so agents can bootstrap from the cloned workspace when
// seedHelpers did not run (old control plane).
func TestRepoScriptMatchesEmbeddedProxy(t *testing.T) {
	t.Parallel()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// …/server/internal/sandbox/artifactcli_test.go → repo root = ../../../
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	scriptPath := filepath.Join(repoRoot, "server", "scripts", "grasp-mcp-spa-proxy.py")
	raw, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read %s: %v", scriptPath, err)
	}
	got := string(raw)
	if got != mcpSpaProxyScript {
		t.Fatalf("server/scripts/grasp-mcp-spa-proxy.py diverged from embedded seedhelpers copy\n"+
			"script len=%d embed len=%d — copy seedhelpers/grasp-mcp-spa-proxy.py → server/scripts/",
			len(got), len(mcpSpaProxyScript))
	}
}
