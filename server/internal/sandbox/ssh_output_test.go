package sandbox

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// This server uses real SSH channels, including the separate stderr stream and
// stdin EOF. Returning a successful exit status must not erase stdout when the
// stderr stream closes without producing any bytes.
func startSSHOutputServer(t *testing.T, serve func(ssh.Channel, string, []byte) uint32) sshCreds {
	t.Helper()
	signer, _ := mustSigner(t)
	cfg := &ssh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(signer)
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
				server, channels, requests, err := ssh.NewServerConn(conn, cfg)
				if err != nil {
					return
				}
				defer server.Close()
				go ssh.DiscardRequests(requests)
				incoming, ok := <-channels
				if !ok {
					return
				}
				channel, reqs, err := incoming.Accept()
				if err != nil {
					return
				}
				defer channel.Close()
				for request := range reqs {
					if request.Type != "exec" {
						_ = request.Reply(false, nil)
						continue
					}
					var payload struct{ Command string }
					if ssh.Unmarshal(request.Payload, &payload) != nil {
						_ = request.Reply(false, nil)
						return
					}
					_ = request.Reply(true, nil)
					input, _ := io.ReadAll(channel)
					code := serve(channel, payload.Command, input)
					_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{code}))
					return
				}
			}()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-acceptDone
		workers.Wait()
	})
	host, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	return sshCreds{host: host, port: port, hostKeys: newHostKeyCache()}
}

func TestSSHOutputPreservesStdoutWhenStderrIsEmpty(t *testing.T) {
	const marker = "result=ok preview_port=18080 upstream=5174\n"
	const script = "printf '%s\\n' 'result=ok preview_port=18080 upstream=5174'\n"
	creds := startSSHOutputServer(t, func(channel ssh.Channel, command string, input []byte) uint32 {
		if strings.Contains(command, "bash") && string(input) != script {
			return 90
		}
		_, _ = io.WriteString(channel, marker)
		// Keep the empty stderr stream open while stdout is already received.
		time.Sleep(time.Millisecond)
		return 0
	})
	for _, withInput := range []bool{false, true} {
		name := "run"
		if withInput {
			name = "runInput"
		}
		t.Run(name, func(t *testing.T) {
			for i := 0; i < 40; i++ {
				var output []byte
				var err error
				if withInput {
					command, _ := newSafeCmd("bash", "-s")
					output, err = creds.runInput(context.Background(), 3*time.Second, command, strings.NewReader(script))
				} else {
					command, _ := newSafeCmd("true")
					output, err = creds.run(context.Background(), 3*time.Second, command)
				}
				if err != nil || string(output) != marker {
					t.Fatalf("execution %d: output=%q err=%v", i, output, err)
				}
			}
		})
	}
}

func TestSSHOutputCombinesConcurrentBinaryStreamsAndRetainsExitStatus(t *testing.T) {
	stdout := bytes.Repeat([]byte{0, 1, 2, 3, 255}, 1024)
	stderr := bytes.Repeat([]byte("stderr-marker\n"), 1024)
	creds := startSSHOutputServer(t, func(channel ssh.Channel, _ string, _ []byte) uint32 {
		var writes sync.WaitGroup
		writes.Add(2)
		go func() { defer writes.Done(); _, _ = channel.Write(stdout) }()
		go func() { defer writes.Done(); _, _ = channel.Stderr().Write(stderr) }()
		writes.Wait()
		return 7
	})
	command, _ := newSafeCmd("true")
	output, err := creds.run(context.Background(), 3*time.Second, command)
	var exit *ssh.ExitError
	if !errors.As(err, &exit) || exit.ExitStatus() != 7 {
		t.Fatalf("exit status = %v", err)
	}
	// Cross-stream ordering is intentionally unspecified; each stream's bytes
	// must be retained exactly, even for a nonzero remote exit status.
	if len(output) != len(stdout)+len(stderr) || bytes.Count(output, []byte{255}) != 1024 || bytes.Count(output, []byte("stderr-marker\n")) != 1024 {
		t.Fatalf("combined output was truncated or corrupted: bytes=%d", len(output))
	}
}

func TestSSHOutputSnapshotDoesNotAliasSubsequentWrites(t *testing.T) {
	var output sshOutputBuffer
	_, _ = output.Write([]byte("partial"))
	snapshot := output.Bytes()
	snapshot[0] = '!'
	_, _ = output.Write([]byte(" complete"))
	if string(output.Bytes()) != "partial complete" || string(snapshot) != "!artial" {
		t.Fatal("returned SSH output aliases the active output buffer")
	}
}
