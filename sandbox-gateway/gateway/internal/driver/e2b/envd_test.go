package e2b

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func bootstrapTestSandbox() *Sandbox {
	return &Sandbox{ID: "sandbox-a", EnvdVersion: "0.6.4", EnvdAccessToken: testProviderSecret + "-envd",
		TrafficAccessToken: testProviderSecret + "-traffic"}
}

func assertEnvdHeaders(t *testing.T, r *http.Request) {
	t.Helper()
	if r.Header.Get("X-API-Key") != "" || r.Header.Get("X-Access-Token") != testProviderSecret+"-envd" ||
		r.Header.Get("e2b-traffic-access-token") != testProviderSecret+"-traffic" || r.Header.Get("E2b-Sandbox-Port") != "49983" ||
		r.Header.Get("E2b-Sandbox-Id") != "sandbox-a" {
		t.Error("envd authentication or port routing differs from the official protocol")
	}
}

func TestEnvdWriteFileUploadsBinaryMultipartAsRoot(t *testing.T) {
	filePath := "/run/grasp compat/env.sh"
	content := []byte("export PRIVATE='" + testProviderSecret + "'\n\x00binary")
	client, _ := newControlTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertEnvdHeaders(t, r)
		if r.Method != http.MethodPost || r.URL.Path != "/files" || r.URL.Query().Get("path") != filePath || r.URL.Query().Get("username") != "root" {
			t.Error("wrong envd upload path, ownership or destination")
		}
		reader, err := r.MultipartReader()
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		part, err := reader.NextPart()
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		got, _ := io.ReadAll(part)
		if part.FormName() != "file" || part.FileName() != "env.sh" || !bytes.Equal(content, got) {
			t.Error("binary multipart contents or file field changed")
		}
		if _, err = reader.NextPart(); !errors.Is(err, io.EOF) {
			t.Error("unexpected additional multipart content")
		}
		_ = json.NewEncoder(w).Encode([]map[string]string{{"path": filePath, "type": "file"}})
	})
	if err := client.WriteFile(t.Context(), bootstrapTestSandbox(), filePath, content); err != nil {
		t.Fatal(err)
	}
}

func processTestFrame(flag byte, body string) []byte {
	frame := make([]byte, 5+len(body))
	frame[0] = flag
	binary.BigEndian.PutUint32(frame[1:], uint32(len(body)))
	copy(frame[5:], body)
	return frame
}

func TestEnvdStartDetachedUsesConnectFramesAndReturnsAfterFirstPID(t *testing.T) {
	closed := make(chan struct{})
	command := "exec setsid bash /run/bootstrap.sh </dev/null >/run/bootstrap.log 2>&1"
	client, _ := newControlTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertEnvdHeaders(t, r)
		username, password, ok := r.BasicAuth()
		if !ok || username != "root" || password != "" || r.Header.Get("Content-Type") != "application/connect+json" ||
			r.Header.Get("Connect-Protocol-Version") != "1" || r.Header.Get("Connect-Timeout-Ms") != "" ||
			r.URL.Path != "/process.Process/Start" || r.Method != http.MethodPost {
			t.Error("wrong root process auth, Connect protocol or process lifetime")
		}
		var header [5]byte
		_, err := io.ReadFull(r.Body, header[:])
		if err != nil || header[0] != 0 {
			t.Error("missing Connect request envelope")
			w.WriteHeader(400)
			return
		}
		payload := make([]byte, binary.BigEndian.Uint32(header[1:]))
		_, _ = io.ReadFull(r.Body, payload)
		var body struct {
			Process struct {
				Cmd  string   `json:"cmd"`
				Args []string `json:"args"`
			} `json:"process"`
			Stdin *bool `json:"stdin"`
		}
		if json.Unmarshal(payload, &body) != nil || body.Process.Cmd != "/bin/bash" || len(body.Process.Args) != 3 ||
			body.Process.Args[0] != "-l" || body.Process.Args[1] != "-c" || body.Process.Args[2] != command || body.Stdin == nil || *body.Stdin {
			t.Error("process config or explicit closed stdin is wrong")
		}
		w.Header().Set("Content-Type", "application/connect+json")
		frame := processTestFrame(0, `{"event":{"start":{"pid":12345}}}`)
		_, _ = w.Write(frame[:3])
		w.(http.Flusher).Flush()
		_, _ = w.Write(frame[3:])
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
			close(closed)
		case <-time.After(5 * time.Second):
			t.Error("detached start waited for completion or retained the stream")
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	pid, err := client.StartDetached(ctx, bootstrapTestSandbox(), command)
	if err != nil || pid != 12345 {
		t.Fatalf("did not receive initial PID: pid=%d err=%v", pid, err)
	}
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("client did not close the detached start stream")
	}
}

func TestEnvdStartRejectsMalformedTruncatedOrErrorFramesWithoutBodyLeaks(t *testing.T) {
	oversized := []byte{0, 0xff, 0xff, 0xff, 0xff}
	for name, frame := range map[string][]byte{
		"header truncated": {0, 0}, "payload truncated": {0, 0, 0, 0, 15, '{'},
		"too large": oversized, "zero frame": {0, 0, 0, 0, 0},
		"compressed": processTestFrame(1, testProviderSecret),
		"end error":  processTestFrame(2, `{"error":{"code":"permission_denied","message":"`+testProviderSecret+`"}}`),
		"bad JSON":   processTestFrame(0, testProviderSecret), "missing event": processTestFrame(0, `{}`),
		"no PID":          processTestFrame(0, `{"event":{"start":{}}}`),
		"negative PID":    processTestFrame(0, `{"event":{"start":{"pid":-1}}}`),
		"overflow PID":    processTestFrame(0, `{"event":{"start":{"pid":4294967296}}}`),
		"unexpected data": processTestFrame(0, `{"event":{"data":{"stdout":"`+testProviderSecret+`"}}}`),
		"multiple oneof":  processTestFrame(0, `{"event":{"start":{"pid":1},"end":{"error":"`+testProviderSecret+`"}}}`),
	} {
		t.Run(name, func(t *testing.T) {
			client, _ := newControlTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/connect+json")
				_, _ = w.Write(frame)
			})
			pid, err := client.StartDetached(t.Context(), bootstrapTestSandbox(), "echo "+testProviderSecret)
			assertSafeControlError(t, err)
			if pid != 0 {
				t.Fatal("malformed stream returned a process identity")
			}
		})
	}
}

func TestEnvdRejectsMissingTokensBeforeAnyBootstrapRequests(t *testing.T) {
	requests := 0
	client, _ := newControlTestClient(t, func(w http.ResponseWriter, r *http.Request) { requests++ })
	for _, sb := range []*Sandbox{nil, {ID: "sandbox-a", EnvdVersion: "0.6.4", EnvdAccessToken: "envd"},
		{ID: "sandbox-a", EnvdVersion: "0.6.4", TrafficAccessToken: "traffic"},
		{ID: "sandbox-a", EnvdVersion: "0.6.4", TrafficAccessToken: "traffic\nvalue", EnvdAccessToken: "envd"}} {
		assertSafeControlError(t, client.WriteFile(t.Context(), sb, "/run/env.sh", []byte(testProviderSecret)))
		_, err := client.StartDetached(t.Context(), sb, "echo "+testProviderSecret)
		assertSafeControlError(t, err)
	}
	if requests != 0 {
		t.Fatal("bootstrap reached a public or unauthenticated guest")
	}
}

func TestEnvdRequiresVersionSupportingExplicitClosedStdin(t *testing.T) {
	for _, version := range []string{"0.2.9", "0.3.0-rc.1", "invalid", "0.3", "999999999999999999999999.3.1"} {
		sb := bootstrapTestSandbox()
		sb.EnvdVersion = version
		if validateCredentials(sb) == nil {
			t.Fatalf("accepted unsupported bootstrap envd version %q", version)
		}
	}
	for _, version := range []string{"0.3.0", "0.3.1", "0.4.0-dev.1", "v1.0.0", "0.6.4+build.1"} {
		sb := bootstrapTestSandbox()
		sb.EnvdVersion = version
		if err := validateCredentials(sb); err != nil {
			t.Fatalf("rejected supported bootstrap envd version %q: %v", version, err)
		}
	}
}

func TestEnvdUploadRequiresExactConfirmation(t *testing.T) {
	for name, body := range map[string]string{
		"empty": "[]", "invalid": testProviderSecret,
		"wrong path": `[{"path":"/different","type":"file"}]`,
		"wrong type": `[{"path":"/run/env.sh","type":"dir"}]`,
		"oversized":  strings.Repeat(" ", maxControlResponseBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			client, _ := newControlTestClient(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) })
			assertSafeControlError(t, client.WriteFile(t.Context(), bootstrapTestSandbox(), "/run/env.sh", []byte(testProviderSecret)))
		})
	}
}

func TestEnvdHTTPErrorAndContentTypeAreSafe(t *testing.T) {
	for _, status := range []int{401, 403, 404, 502} {
		client, _ := newControlTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(testProviderSecret))
		})
		assertSafeControlError(t, client.WriteFile(t.Context(), bootstrapTestSandbox(), "/run/env.sh", nil))
		_, err := client.StartDetached(t.Context(), bootstrapTestSandbox(), "echo "+testProviderSecret)
		assertSafeControlError(t, err)
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.HTTPStatus != status {
			t.Fatal("envd status was lost")
		}
	}
	client, _ := newControlTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write(processTestFrame(0, `{"event":{"start":{"pid":1}}}`))
	})
	_, err := client.StartDetached(t.Context(), bootstrapTestSandbox(), "true")
	assertSafeControlError(t, err)
}

func TestEnvdCanceledUploadAndBlockedStreamStayBounded(t *testing.T) {
	client, _ := newControlTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if r.URL.Path == "/process.Process/Start" {
			w.Header().Set("Content-Type", "application/connect+json")
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
		}
		<-r.Context().Done()
	})
	for _, operation := range []string{"upload", "start"} {
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		var err error
		if operation == "upload" {
			err = client.WriteFile(ctx, bootstrapTestSandbox(), "/run/env.sh", []byte(testProviderSecret))
		} else {
			_, err = client.StartDetached(ctx, bootstrapTestSandbox(), "true")
		}
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("%s lost request cancellation: %v", operation, err)
		}
		assertSafeControlError(t, err)
	}
}

func TestInvalidBootstrapInputsDoNotReachEnvd(t *testing.T) {
	requests := 0
	client, _ := newControlTestClient(t, func(w http.ResponseWriter, r *http.Request) { requests++ })
	for _, path := range []string{"relative", "/", "/tmp/..", "/run/bad\x00file"} {
		assertSafeControlError(t, client.WriteFile(t.Context(), bootstrapTestSandbox(), path, nil))
	}
	assertSafeControlError(t, client.WriteFile(t.Context(), bootstrapTestSandbox(), "/run/too-large", make([]byte, maxBootstrapFileBytes+1)))
	for _, command := range []string{"", "  ", "bad\x00command", strings.Repeat("x", maxProcessFrameBytes+1)} {
		_, err := client.StartDetached(t.Context(), bootstrapTestSandbox(), command)
		assertSafeControlError(t, err)
	}
	if requests != 0 {
		t.Fatal("invalid bootstrap request reached provider")
	}
}
