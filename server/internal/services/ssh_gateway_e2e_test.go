package services

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/sshgateway"
	"golang.org/x/crypto/ssh"
)

// startEchoSSHD answers every exec with "sandbox:<command>".
func startEchoSSHD(t *testing.T) string {
	t.Helper()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	host, _ := ssh.NewSignerFromKey(priv)
	cfg := &ssh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(host)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				sconn, chans, reqs, err := ssh.NewServerConn(nc, cfg)
				if err != nil {
					return
				}
				defer func() { _ = sconn.Close() }()
				go ssh.DiscardRequests(reqs)
				for nch := range chans {
					ch, reqs, err := nch.Accept()
					if err != nil {
						continue
					}
					go func() {
						defer func() { _ = ch.Close() }()
						for req := range reqs {
							if req.Type != "exec" {
								_ = req.Reply(true, nil)
								continue
							}
							var p struct{ Command string }
							_ = ssh.Unmarshal(req.Payload, &p)
							_ = req.Reply(true, nil)
							_, _ = fmt.Fprintf(ch, "sandbox:%s", p.Command)
							_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
							return
						}
					}()
				}
			}()
		}
	}()
	return ln.Addr().String()
}

func TestSSHGatewayEndToEndThroughSandboxService(t *testing.T) {
	f := newStopFixture(t)
	row := f.recovery(t)
	gw, err := f.sbx.mgr.Gateway().Get(context.Background(), row.Name)
	if err != nil {
		t.Fatal(err)
	}
	endpoints := map[string]string{}
	for k, v := range gw.Endpoints {
		endpoints[k] = v
	}
	endpoints["ssh"] = startEchoSSHD(t)
	f.fg.SetEndpoints(row.Name, endpoints)

	cfg := &config.Config{Auth: config.AuthConfig{Users: []config.AuthUser{{Username: "alice", PasswordHash: "h"}}}}
	keys := NewSSHKeyService(f.db, func() *config.Config { return cfg })
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(priv)
	stored, err := keys.Add("alice", "laptop", string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	if err != nil {
		t.Fatal(err)
	}

	srv, err := sshgateway.New(sshgateway.Options{HostKeyPath: filepath.Join(t.TempDir(), "host")}, keys, f.sbx)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	keys.SetRevokeHook(func(fp string) { srv.DisconnectFingerprint(fp) })
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()

	cli, err := ssh.Dial("tcp", ln.Addr().String(), &ssh.ClientConfig{
		User: fmt.Sprintf("sbx-%d", row.ID), Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	sess, err := cli.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	out, err := sess.Output("uname -a")
	if err != nil || string(out) != "sandbox:uname -a" {
		t.Fatalf("exec through the gateway = %q %v", out, err)
	}

	waitFor(t, "external session", func() bool { return f.sbx.ExternalSessions(row.ID) == 1 })
	view, err := f.solo.Get(context.Background(), f.id, "alice")
	if err != nil || view.Sandbox.ExternalSessions != 1 {
		t.Fatalf("session view = %+v %v", view.Sandbox, err)
	}

	if err := keys.Delete("alice", stored.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cli.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("deleting the key did not disconnect the IDE")
	}
	waitFor(t, "released", func() bool { return f.sbx.ExternalSessions(row.ID) == 0 })

	if _, err := ssh.Dial("tcp", ln.Addr().String(), &ssh.ClientConfig{
		User: fmt.Sprintf("sbx-%d", row.ID), Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 5 * time.Second,
	}); err == nil || !strings.Contains(err.Error(), "unable to authenticate") {
		t.Fatalf("deleted key reconnected: %v", err)
	}
}
