package services

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"golang.org/x/crypto/ssh"
)

// The real SSH PTY/exec boundary used by OpenTerminal must accept the canonical
// command. Merely checking an argv slice missed the previous rejected bash -c.
func TestOpenTerminalCanonicalDirectoryThroughRealSSHPTY(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HISTFILE", "/dev/null")
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	acceptDone := make(chan struct{})
	initial := t.TempDir()
	commands := make(chan string, 4)
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
				server, incoming, requests, err := ssh.NewServerConn(conn, cfg)
				if err != nil {
					return
				}
				defer server.Close()
				go ssh.DiscardRequests(requests)
				for candidate := range incoming {
					channel, requests, err := candidate.Accept()
					if err != nil {
						return
					}
					func() {
						defer channel.Close()
						for request := range requests {
							if request.Type == "pty-req" || request.Type == "window-change" {
								_ = request.Reply(true, nil)
								continue
							}
							if request.Type != "exec" {
								_ = request.Reply(false, nil)
								continue
							}
							var payload struct{ Command string }
							if ssh.Unmarshal(request.Payload, &payload) != nil {
								_ = request.Reply(false, nil)
								return
							}
							commands <- payload.Command
							terminal, err := sandbox.OpenLocalTerminal(initial)
							if err != nil {
								_ = request.Reply(false, nil)
								return
							}
							defer terminal.Close()
							// Start in another directory, then execute the production
							// SSH command in a real PTY before accepting user input.
							if _, err := io.WriteString(terminal, "exec "+payload.Command+"\n"); err != nil {
								_ = request.Reply(false, nil)
								return
							}
							_ = request.Reply(true, nil)
							go func() { _, _ = io.Copy(terminal, channel) }()
							_, _ = io.Copy(channel, terminal)
							_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
							return
						}
					}()
				}
			}()
		}
	}()
	t.Cleanup(func() { _ = listener.Close(); <-acceptDone; workers.Wait() })
	db := newTestDB(t)
	ds := &dockerState{}
	service := newSandboxService(t, db, ds)
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	ds.fg.SSHPort, _ = strconv.Atoi(port)
	row := &models.Sandbox{Name: "canonical-terminal", Purpose: "test", Status: "running", Token: "owned-synthetic-terminal"}
	if err := db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
	ds.setStatus(row.Name, "running")
	canonical := t.TempDir()
	if err := os.WriteFile(filepath.Join(canonical, "canonical-marker.txt"), []byte("CANONICAL_DIRECTORY_PROOF\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, workspaceAccessKey{}, workspaceAccessProof{sandboxID: row.ID, path: canonical})
	terminal, err := service.OpenTerminal(ctx, row.ID)
	if err != nil {
		t.Fatal("canonical command rejected before SSH execution", err)
	}
	defer terminal.Close()
	if _, err := io.WriteString(terminal, "printf '__CANONICAL_CWD__%s\\n' \"$PWD\"; cat canonical-marker.txt; exit\n"); err != nil {
		t.Fatal(err)
	}
	output := make(chan []byte, 1)
	go func() { data, _ := io.ReadAll(terminal); output <- data }()
	select {
	case data := <-output:
		if !strings.Contains(string(data), "__CANONICAL_CWD__"+canonical) || !strings.Contains(string(data), "CANONICAL_DIRECTORY_PROOF") {
			t.Fatal("terminal did not use the canonical directory")
		}
	case <-ctx.Done():
		t.Fatal("real SSH PTY did not complete")
	}
	select {
	case command := <-commands:
		if !strings.HasPrefix(command, "'env' '-C' ") {
			t.Fatal("terminal bypassed the safe argv command")
		}
	default:
		t.Fatal("canonical terminal never reached the real SSH exec boundary")
	}
	bad := context.WithValue(ctx, workspaceAccessKey{}, workspaceAccessProof{sandboxID: row.ID, path: canonical + ";echo-injected"})
	if value, err := service.OpenTerminal(bad, row.ID); err == nil {
		_ = value.Close()
		t.Fatal("terminal weakened argv validation")
	}
	select {
	case <-commands:
		t.Fatal("unsafe canonical path reached SSH exec")
	default:
	}
}
