package workspacebinding

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func syntheticListener(t *testing.T, proc, root string, port int, inode string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(proc, "net"), 0700); err != nil {
		t.Fatal(err)
	}
	data := fmt.Sprintf("sl local_address rem_address st tx_queue tr retrnsmt uid timeout inode\n0: 0100007F:%04X 00000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 %s\n", port, inode)
	if err := os.WriteFile(filepath.Join(proc, "net", "tcp"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(proc, "123", "fd")
	if err := os.MkdirAll(base, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("socket:["+inode+"]", filepath.Join(base, "4")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root, filepath.Join(proc, "123", "cwd")); err != nil {
		t.Fatal(err)
	}
}
func TestWorkspaceBindingPortRejectsWrongOrUnobservedListenerAndIncompleteScan(t *testing.T) {
	for _, kind := range []string{"current", "other_root", "similar_prefix", "unobserved", "oversized", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			base := t.TempDir()
			root := filepath.Join(base, "current")
			proc := filepath.Join(base, "proc")
			syntheticListener(t, proc, root, 3456, "98765")
			switch kind {
			case "other_root", "similar_prefix":
				path := filepath.Join(base, "elsewhere")
				if kind == "similar_prefix" {
					path = root + "-old"
				}
				if err := os.Remove(filepath.Join(proc, "123", "cwd")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path, filepath.Join(proc, "123", "cwd")); err != nil {
					t.Fatal(err)
				}
			case "unobserved":
				if err := os.Remove(filepath.Join(proc, "123", "fd", "4")); err != nil {
					t.Fatal(err)
				}
			case "oversized":
				f, err := os.OpenFile(filepath.Join(proc, "net", "tcp"), os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				_, err = io.Copy(f, strings.NewReader(strings.Repeat(" ", 4<<20)))
				if closeErr := f.Close(); err != nil || closeErr != nil {
					t.Fatal(err, closeErr)
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if kind == "cancelled" {
				cancel()
			}
			err := listenerRoot(ctx, proc, root, 3456)
			if kind == "current" && err != nil || kind != "current" && err == nil {
				t.Fatal(kind, err)
			}
			if kind == "oversized" && !errors.Is(err, ErrBusy) {
				t.Fatal("truncated proc table accepted", err)
			}
		})
	}
}
func TestWorkspaceBindingPortListenerHelper(t *testing.T) {
	ready := os.Getenv("GRASP_BINDING_TEST_LISTENER_READY")
	if ready == "" {
		return
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := os.WriteFile(ready, []byte(listener.Addr().String()), 0600); err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(15 * time.Second)
	for time.Now().Before(until) {
		if _, err := os.Stat(ready + ".stop"); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("listener helper not stopped")
}
func TestWorkspaceBindingPortActualOldServerCannotBeRelabeledAfterSwitch(t *testing.T) {
	f := newFixture(t)
	ready := filepath.Join(t.TempDir(), "ready")
	child := exec.Command(os.Args[0], "-test.run=^TestWorkspaceBindingPortListenerHelper$")
	child.Dir = f.cfg.OriginalRoot
	child.Env = append(os.Environ(), "GRASP_BINDING_TEST_LISTENER_READY="+ready)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.WriteFile(ready+".stop", nil, 0600)
		if err := child.Wait(); err != nil {
			t.Error(err)
		}
	})
	var data []byte
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		data, _ = os.ReadFile(ready)
		if len(data) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	_, portText, err := net.SplitHostPort(string(data))
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portText)
	view, err := f.manager.Port(t.Context(), port)
	if err != nil || !view.Verified {
		t.Fatal(view, err)
	}
	req := f.request(t, "operation-port-switch", "activate_workspace", 1, Selector{Kind: "restore", ID: f.restoreID})
	if receipt := f.run(t, req); receipt.State != "completed" {
		t.Fatal(receipt)
	}
	if _, err := f.manager.Port(t.Context(), port); !errors.Is(err, ErrConflict) {
		t.Fatal("old server acquired new root label", err)
	}
	// The user's old server remains alive and its directory remains readable.
	connection, err := net.DialTimeout("tcp", string(data), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = connection.Close()
	if _, err := os.Stat(filepath.Join(f.cfg.OriginalRoot, "marker")); err != nil {
		t.Fatal(err)
	}
}
