package router

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"backend/internal/auth"
	"backend/internal/service"
	"backend/internal/workspacegit"
)

// Exercise real HTTP bodies, the router's authenticated renewal endpoint and
// native Git. The archive takes longer to upload than the initial lease.
func TestWorkspaceCodeHTTPImportNeedsAcknowledgedRenewalsForLongUploads(t *testing.T) {
	source := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = source
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
		b, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("native Git: %v: %s", err, b)
		}
		return strings.TrimSpace(string(b))
	}
	git("init", "-q")
	content := make([]byte, 256<<10)
	if _, err := rand.Read(content); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "native.bin"), content, 0600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "-qm", "native transfer")
	commit := git("rev-parse", "HEAD")
	var archive bytes.Buffer
	if _, err := (&workspacegit.Service{Root: source}).Export(t.Context(), []workspacegit.Selection{{Path: ".", Commit: commit}}, &archive); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(archive.Bytes())

	for _, renewed := range []bool{true, false} {
		t.Run(strconv.FormatBool(renewed), func(t *testing.T) {
			target := t.TempDir()
			t.Setenv("WORKSPACE_DIR", target)
			runtime := httptest.NewServer(New(&Dependencies{WebRoot: t.TempDir(), Chats: service.NewChatManager(), Auth: auth.NewGuard("lease-fixture-password")}))
			defer runtime.Close()
			client := runtime.Client()
			client.Timeout = 10 * time.Second
			login, err := client.Post(runtime.URL+"/api/login", "application/json", strings.NewReader(`{"password":"lease-fixture-password"}`))
			if err != nil {
				t.Fatal(err)
			}
			cookies := login.Cookies()
			_ = login.Body.Close()
			if login.StatusCode != http.StatusOK || len(cookies) == 0 {
				t.Fatal("native fixture login failed")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			reader, writer := io.Pipe()
			writerDone := make(chan struct{})
			go func() {
				defer close(writerDone)
				defer func() { _ = writer.Close() }()
				data := archive.Bytes()
				for len(data) > 0 {
					select {
					case <-ctx.Done():
						return
					case <-time.After(30 * time.Millisecond):
					}
					n := min(4096, len(data))
					if _, err := writer.Write(data[:n]); err != nil {
						return
					}
					data = data[n:]
				}
			}()
			defer func() { cancel(); _ = reader.Close(); <-writerDone }()
			request, err := http.NewRequestWithContext(ctx, http.MethodPost, runtime.URL+"/api/workspace/code/imports/long-upload", reader)
			if err != nil {
				t.Fatal(err)
			}
			request.AddCookie(cookies[0])
			request.Header.Set("X-Grasp-Operation-Generation", "5")
			initial := time.Now().UTC().Add(time.Second)
			request.Header.Set("X-Grasp-Operation-Expires-At", initial.Format(time.RFC3339Nano))
			request.Header.Set("X-Grasp-Archive-SHA256", hex.EncodeToString(digest[:]))
			renewStop, renewDone := make(chan struct{}), make(chan error, 1)
			if renewed {
				go func() {
					ticker := time.NewTicker(250 * time.Millisecond)
					defer ticker.Stop()
					for {
						select {
						case <-renewStop:
							renewDone <- nil
							return
						case <-ctx.Done():
							renewDone <- ctx.Err()
							return
						case <-ticker.C:
						}
						expires := time.Now().UTC().Add(time.Second)
						renew, _ := http.NewRequestWithContext(ctx, http.MethodPatch, runtime.URL+"/api/workspace/code/fence", nil)
						renew.AddCookie(cookies[0])
						renew.Header.Set("X-Grasp-Operation-Generation", "5")
						renew.Header.Set("X-Grasp-Operation-Expires-At", expires.Format(time.RFC3339Nano))
						response, err := client.Do(renew)
						if err != nil {
							renewDone <- err
							return
						}
						var ack struct {
							Generation uint64    `json:"generation"`
							ExpiresAt  time.Time `json:"expiresAt"`
						}
						err = json.NewDecoder(response.Body).Decode(&ack)
						_ = response.Body.Close()
						if err != nil || response.StatusCode != http.StatusOK || ack.Generation != 5 || !ack.ExpiresAt.Equal(expires) {
							renewDone <- io.ErrUnexpectedEOF
							return
						}
					}
				}()
			}
			response, requestErr := client.Do(request)
			close(renewStop)
			if renewed {
				if err := <-renewDone; err != nil {
					t.Fatal("native renewal failed", err)
				}
			}
			var receipt workspacegit.Receipt
			if response != nil {
				defer func() { _ = response.Body.Close() }()
				_ = json.NewDecoder(response.Body).Decode(&receipt)
			}
			if renewed {
				if requestErr != nil || response == nil || response.StatusCode != http.StatusOK || receipt.ID != "long-upload" || !time.Now().After(initial) {
					t.Fatalf("renewed native upload failed: receipt=%+v error=%v response=%v", receipt, requestErr, response)
				}
				persisted, err := (&workspacegit.Service{Root: target}).Receipt("long-upload")
				if err != nil || persisted.ArchiveSHA256 != hex.EncodeToString(digest[:]) || len(persisted.Repositories) != 1 || persisted.Repositories[0].Commit != commit {
					t.Fatalf("native receipt changed: %+v %v", persisted, err)
				}
			} else {
				if requestErr == nil && response != nil && response.StatusCode == http.StatusOK {
					t.Fatal("upload survived its last acknowledged lease")
				}
				if persisted, err := (&workspacegit.Service{Root: target}).Receipt("long-upload"); err == nil {
					t.Fatalf("expired upload published a completed receipt: %+v", persisted)
				}
			}
		})
	}
}
