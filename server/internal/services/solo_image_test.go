package services

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"golang.org/x/crypto/ssh"
)

func TestReadSoloImageValidatesBytesAndBoundsReads(t *testing.T) {
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)
	data, media, err := readSoloImage(bytes.NewReader(png))
	if err != nil || media != "image/png" || !bytes.Equal(data, png) {
		t.Fatalf("image=%x media=%s err=%v", data, media, err)
	}
	for _, tc := range []struct {
		name   string
		reader io.Reader
		status int
	}{
		{"empty", bytes.NewReader(nil), 415},
		{"active SVG", bytes.NewBufferString(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), 415},
		{"HTML disguised as image", bytes.NewBufferString("<html>private data</html>"), 415},
		{"too large", bytes.NewReader(make([]byte, maxSoloImageBytes+1)), 413},
		{"read failure", errorImageReader{}, 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, _, err := readSoloImage(tc.reader)
			var se *SoloError
			if len(data) != 0 || !errors.As(err, &se) || se.Status != tc.status {
				t.Fatalf("data=%d err=%v", len(data), err)
			}
		})
	}
}

type errorImageReader struct{}

func (errorImageReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestSoloImageReadsRealGuestExportAndRejectsOutsideRoots(t *testing.T) {
	sbx, row, _, _, control := recoveryAuthorizationFixture(t)
	control.provider.SetStatus(row.Name, "running")
	if err := sbx.db.Model(&row).Update("status", "running").Error; err != nil {
		t.Fatal(err)
	}
	source := imageExportSource(t)
	sbx.live[row.ID] = &liveSandbox{sb: source}
	solo := NewSoloService(sbx.db, sbx, sbx.skills, nil)
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)
	file := filepath.Join(source.WorkspaceDir, "full.png")
	if err := os.WriteFile(file, png, 0o600); err != nil {
		t.Fatal(err)
	}
	unicodeName := "截图 $(touch injected).png"
	if err := os.WriteFile(filepath.Join(source.WorkspaceDir, unicodeName), png, 0o600); err != nil {
		t.Fatal(err)
	}
	// The screenshot must load before its generating turn releases its lease.
	held, release, err := sbx.acquireSandboxOperation(t.Context(), row.ID, sandboxOperationInference)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	lease, _ := sbx.heldSandboxOperation(held, row.ID)
	for _, path := range []string{"full.png", file, unicodeName} {
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		data, media, err := solo.Image(ctx, row.ThreadID, "alice", path)
		cancel()
		if err != nil || media != "image/png" || !bytes.Equal(data, png) {
			t.Fatalf("%s: image=%x media=%s err=%v", path, data, media, err)
		}
	}
	current, err := sbx.Get(row.ID)
	if err != nil || current.OperationOwner != lease.Owner || current.Generation != lease.Generation {
		t.Fatal("image read changed inference ownership", current, err)
	}
	if _, err := os.Stat(filepath.Join(source.WorkspaceDir, "injected")); !os.IsNotExist(err) {
		t.Fatal("image filename was executed as shell syntax")
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(source.WorkspaceDir, "escape.png")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/etc/passwd", "escape.png", "missing.png"} {
		if data, _, err := solo.Image(t.Context(), row.ThreadID, "alice", path); err == nil || len(data) != 0 {
			t.Fatalf("unsafe export %s returned %d bytes: %v", path, len(data), err)
		}
	}
	if control.starts.Load() != 0 {
		t.Fatal("reading an image started the environment")
	}
}

// Execute OpenArtifactFile's real Python export over SSH, so authorization,
// command quoting, path confinement, binary transport and MIME sniffing are
// tested together rather than replacing the source with a successful stub.
func imageExportSource(t *testing.T, afterRead ...func()) *sandbox.Sandbox {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	config := &ssh.ServerConfig{NoClientAuth: true}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer conn.Close()
				server, channels, requests, err := ssh.NewServerConn(conn, config)
				if err != nil {
					return
				}
				defer server.Close()
				go ssh.DiscardRequests(requests)
				for incoming := range channels {
					channel, requests, err := incoming.Accept()
					if err != nil {
						return
					}
					func() {
						defer channel.Close()
						for request := range requests {
							var payload struct{ Command string }
							if request.Type != "exec" || ssh.Unmarshal(request.Payload, &payload) != nil {
								_ = request.Reply(false, nil)
								continue
							}
							_ = request.Reply(true, nil)
							command := exec.CommandContext(t.Context(), "sh", "-c", payload.Command)
							command.Stdin = channel
							command.Stdout, command.Stderr = channel, channel.Stderr()
							status := uint32(0)
							if err := command.Run(); err != nil {
								status = 1
							}
							for _, callback := range afterRead {
								callback()
							}
							_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
							return
						}
					}()
				}
			}()
		}
	}()
	t.Cleanup(func() { _ = listener.Close(); <-acceptDone; workers.Wait() })
	return &sandbox.Sandbox{WorkspaceDir: t.TempDir(), SSHHost: "127.0.0.1", SSHPort: listener.Addr().(*net.TCPAddr).Port, Password: "synthetic-image-export"}
}

func TestSoloImageDiscardsBytesWhenAuthorizationChangesDuringRead(t *testing.T) {
	sbx, row, credentials, ids, control := recoveryAuthorizationFixture(t)
	control.provider.SetStatus(row.Name, "running")
	if err := sbx.db.Model(&row).Update("status", "running").Error; err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	source := imageExportSource(t, func() {
		once.Do(func() {
			if err := credentials.Revoke(ids["model"]); err != nil {
				t.Error(err)
			}
		})
	})
	sbx.live[row.ID] = &liveSandbox{sb: source}
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)
	if err := os.WriteFile(filepath.Join(source.WorkspaceDir, "full.png"), png, 0o600); err != nil {
		t.Fatal(err)
	}
	solo := NewSoloService(sbx.db, sbx, sbx.skills, nil)
	data, _, err := solo.Image(t.Context(), row.ThreadID, "alice", "full.png")
	var se *SoloError
	if len(data) != 0 || !errors.As(err, &se) || se.Code != "environment_authorization_blocked" {
		t.Fatalf("changed authorization returned %d bytes: %v", len(data), err)
	}
}

func TestSoloImageRequiresOwnedCurrentlyAuthorizedEnvironment(t *testing.T) {
	for _, state := range []string{"other user", "revoked", "closed", "stopped", "invalid path"} {
		t.Run(state, func(t *testing.T) {
			sbx, row, credentials, ids, _ := recoveryAuthorizationFixture(t)
			if err := sbx.db.Model(&row).Update("status", "running").Error; err != nil {
				t.Fatal(err)
			}
			solo := NewSoloService(sbx.db, sbx, sbx.skills, nil)
			var parent models.SoloSession
			if err := sbx.db.First(&parent, "id = ?", row.ThreadID).Error; err != nil {
				t.Fatal(err)
			}
			user, path := parent.UserID, "/tmp/full.png"
			switch state {
			case "other user":
				user = "other-user"
			case "revoked":
				if err := credentials.Revoke(ids["model"]); err != nil {
					t.Fatal(err)
				}
			case "closed":
				if err := sbx.db.Model(&parent).Update("status", models.SoloStatusClosed).Error; err != nil {
					t.Fatal(err)
				}
			case "stopped":
				if err := sbx.db.Model(&row).Update("desired_state", "stopped").Error; err != nil {
					t.Fatal(err)
				}
			case "invalid path":
				path = "bad\x00path"
			}
			if data, _, err := solo.Image(context.Background(), parent.ID, user, path); err == nil || len(data) != 0 {
				t.Fatalf("denied image access returned %d bytes: %v", len(data), err)
			}
		})
	}
}

func TestSoloImagePinsReadRootWithoutRetargetingConcurrentConsumers(t *testing.T) {
	for _, change := range []string{"none", "workspace", "owner", "lease"} {
		t.Run(change, func(t *testing.T) {
			bindings, remote, parent := bindingAccessFixture(t)
			sbx := bindings.sandbox
			var once sync.Once
			var source *sandbox.Sandbox
			nextRoot := t.TempDir()
			source = imageExportSource(t, func() {
				once.Do(func() {
					switch change {
					case "workspace":
						remote.view.Path = nextRoot
						remote.view.Head.Reference.Revision++
						remote.view.Head.Reference.Fingerprint = nativeBindingFingerprint(remote.view.Head)
						if err := sbx.db.Model(&models.WorkspaceBindingHead{}).Where("sandbox_id = ?", parent.SandboxID).Updates(map[string]any{
							"revision": remote.view.Head.Reference.Revision, "native_fingerprint": remote.view.Head.Reference.Fingerprint,
						}).Error; err != nil {
							t.Error(err)
						}
						source.SetActiveWorkspace(nextRoot, remote.view.Head.Reference.Fingerprint)
					case "owner":
						if err := sbx.db.Model(&models.SoloSession{}).Where("id = ?", parent.ID).Update("user_id", "other").Error; err != nil {
							t.Error(err)
						}
					case "lease":
						if err := sbx.db.Model(&models.Sandbox{}).Where("id = ?", parent.SandboxID).Update("operation_owner", "successor").Error; err != nil {
							t.Error(err)
						}
					}
				})
			})
			sbx.live[parent.SandboxID] = &liveSandbox{sb: source}
			originalRoot := source.EffectiveWorkspaceDir()
			png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)
			if err := os.WriteFile(filepath.Join(remote.view.Path, "full.png"), png, 0o600); err != nil {
				t.Fatal(err)
			}
			_, release, err := sbx.acquireSandboxOperation(t.Context(), parent.SandboxID, sandboxOperationInference)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			solo := NewSoloService(sbx.db, sbx, sbx.skills, nil)
			data, media, err := solo.Image(t.Context(), parent.ID, "alice", "full.png")
			if change == "none" {
				if err != nil || media != "image/png" || !bytes.Equal(data, png) {
					t.Fatalf("canonical image: %x %s %v", data, media, err)
				}
			} else if err == nil || len(data) != 0 {
				t.Fatalf("changed %s returned %d bytes: %v", change, len(data), err)
			}
			wantRoot := originalRoot
			if change == "workspace" {
				wantRoot = nextRoot
			}
			if got := source.EffectiveWorkspaceDir(); got != wantRoot {
				t.Fatalf("image read changed the shared workspace from %s to %s", wantRoot, got)
			}
		})
	}
}
