package oneshot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"backend/internal/provider"
)

// diagnosticFake executes real shell processes, including their stderr and exit
// status. Different invocations model an unavailable API recovering on retry.
type diagnosticFake struct {
	baseFake
	scripts []string
	resumes []string
}

func (f *diagnosticFake) Args(_ provider.OpenOptions, _, resumeID string) []string {
	i := len(f.resumes)
	f.resumes = append(f.resumes, resumeID)
	return []string{"sh", "-c", f.scripts[i]}
}

func (*diagnosticFake) ParseLine(line []byte) ParseResult {
	if s := string(line); strings.HasPrefix(s, "error:") {
		return ParseResult{Msgs: []Msg{{Kind: KindError, Text: strings.TrimPrefix(s, "error:")}}, StopReason: "failed"}
	}
	return (&fakeCodec{}).ParseLine(line)
}

type diagnosticFrames struct {
	mu     sync.Mutex
	frames []map[string]any
}

func (f *diagnosticFrames) receive(raw json.RawMessage) {
	var frame map[string]any
	if json.Unmarshal(raw, &frame) == nil {
		f.mu.Lock()
		f.frames = append(f.frames, frame)
		f.mu.Unlock()
	}
}

func (f *diagnosticFrames) take() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	frames := f.frames
	f.frames = nil
	return frames
}

// A consumer may detach as soon as it receives prompt_done. Assert the error
// arrives exactly once before that boundary, rather than merely somewhere in
// the final event collection.
func assertDiagnosticBoundary(t *testing.T, frames []map[string]any, stop string, wantErrors int) string {
	t.Helper()
	var done, errors int
	var diagnostic string
	for i, f := range frames {
		switch f["type"] {
		case "error_text":
			errors++
			diagnostic, _ = f["text"].(string)
			if done != 0 {
				t.Fatal("error_text arrived after prompt_done")
			}
		case "prompt_done":
			done++
			if f["stopReason"] != stop || i != len(frames)-1 {
				t.Fatalf("invalid final boundary: frames=%v", frames)
			}
		}
	}
	if done != 1 || errors != wantErrors {
		t.Fatalf("boundaries=%d errors=%d, want 1 and %d; frames=%v", done, errors, wantErrors, frames)
	}
	return diagnostic
}

func openDiagnosticSession(t *testing.T, c Codec, resumeID string) (provider.Session, *diagnosticFrames) {
	t.Helper()
	frames := &diagnosticFrames{}
	s, err := NewProvider(c).Open(context.Background(), context.Background(),
		provider.OpenOptions{Cwd: t.TempDir(), ResumeSessionID: resumeID}, frames.receive, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, frames
}

func TestOneShotFailureExplainsBeforeDone(t *testing.T) {
	tests := []struct {
		name   string
		script string
		setup  func(*engine)
		images []provider.PromptImage
		want   string
	}{
		{name: "stderr", script: `printf '%s\n' 'Client network socket disconnected before secure TLS connection was established' >&2; exit 12`, want: "secure TLS connection"},
		{name: "spawn", setup: func(e *engine) { e.binPath = filepath.Join(t.TempDir(), "missing-cli") }, want: "missing-cli"},
		{name: "attachment", images: []provider.PromptImage{{Data: "!invalid-base64!"}}, want: "attach: decode #1"},
		{name: "structured error and stderr", script: `printf '%s\n' 'error:Failed to reach the Cursor API'; printf '%s\n' 'Failed to reach the Cursor API' >&2; exit 1`, want: "Failed to reach the Cursor API"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &diagnosticFake{scripts: []string{tt.script}}
			s, frames := openDiagnosticSession(t, c, "")
			if tt.setup != nil {
				tt.setup(s.(*engine))
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result, err := s.Prompt(ctx, "change the icon", tt.images)
			if err == nil || result.StopReason != "failed" {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			text := assertDiagnosticBoundary(t, frames.take(), "failed", 1)
			if !strings.Contains(text, tt.want) {
				t.Fatalf("diagnostic=%q, want %q", text, tt.want)
			}
			if len(tt.images) > 0 && len(c.resumes) != 0 {
				t.Fatal("invalid attachment should fail before spawning the CLI")
			}
		})
	}
}

func TestOneShotFailedResumeAndRetry(t *testing.T) {
	c := &diagnosticFake{scripts: []string{
		`printf '%s\n' 'error:resume connection refused'; printf '%s\n' 'resume connection refused' >&2; exit 1`,
		`printf '%s\n' 'fresh session TLS handshake failed' >&2; exit 1`,
		`printf '%s\n' 'sid:S1' 'text:recovered' 'done'`,
	}}
	s, frames := openDiagnosticSession(t, c, "stale-session")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := s.Prompt(ctx, "change the icon", nil)
	if err == nil || result.StopReason != "failed" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	text := assertDiagnosticBoundary(t, frames.take(), "failed", 1)
	if !strings.Contains(text, "fresh session TLS handshake failed") || strings.Contains(text, "resume connection refused") {
		t.Fatalf("must explain the final failed attempt: %q", text)
	}
	result, err = s.Prompt(ctx, "change the icon", nil)
	if err != nil || result.StopReason != "end_turn" {
		t.Fatalf("retry result=%+v err=%v", result, err)
	}
	retryFrames := frames.take()
	assertDiagnosticBoundary(t, retryFrames, "end_turn", 0)
	if got, _ := firstText(retryFrames); got != "recovered" {
		t.Fatalf("retry answer=%q", got)
	}
	if got := strings.Join(c.resumes, "/"); got != "stale-session//" {
		t.Fatalf("resume sequence=%q", got)
	}
}

func TestOneShotSuccessfulResumeFallbackDoesNotReportTransientFailure(t *testing.T) {
	for _, first := range []string{
		`printf '%s\n' 'invalid saved session' >&2; exit 1`,
		`printf '%s\n' 'error:invalid saved session'; exit 1`,
	} {
		t.Run(first, func(t *testing.T) {
			c := &diagnosticFake{scripts: []string{first, `printf '%s\n' 'sid:S1' 'text:recovered' 'done'`}}
			s, frames := openDiagnosticSession(t, c, "stale-session")
			result, err := s.Prompt(context.Background(), "hi", nil)
			if err != nil || result.StopReason != "end_turn" {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			assertDiagnosticBoundary(t, frames.take(), "end_turn", 0)
		})
	}
}

func TestOneShotStructuredFailureWithoutExitError(t *testing.T) {
	c := &diagnosticFake{scripts: []string{`printf '%s\n' 'error:API unavailable' 'error:Check your proxy configuration'`}}
	s, frames := openDiagnosticSession(t, c, "")
	result, err := s.Prompt(context.Background(), "hi", nil)
	if err != nil || result.StopReason != "failed" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	got := frames.take()
	assertDiagnosticBoundary(t, got, "failed", 2)
	if got[0]["text"] != "API unavailable" || got[1]["text"] != "Check your proxy configuration" {
		t.Fatalf("lost final attempt diagnostics: %v", got)
	}
}

func TestOneShotCancelDoesNotReportCLIError(t *testing.T) {
	t.Setenv("CURSOR_API_KEY", "fake-cancel-credential")
	c := &diagnosticFake{scripts: []string{`printf '%s\n' "$CURSOR_API_KEY" >&2; printf '%s\n' 'text:working'; exec sleep 30`}}
	frames := &diagnosticFrames{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := NewProvider(c).Open(context.Background(), context.Background(), provider.OpenOptions{Cwd: t.TempDir()}, func(raw json.RawMessage) {
		frames.receive(raw)
		cancel()
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	result, err := s.Prompt(ctx, "hi", nil)
	if !errors.Is(err, context.Canceled) || result.StopReason != "cancelled" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if strings.Contains(err.Error(), "fake-cancel-credential") {
		t.Fatalf("cancel diagnostic exposed a credential: %v", err)
	}
	assertDiagnosticBoundary(t, frames.take(), "cancelled", 0)
}

func TestOneShotTimeoutAfterStructuredErrorKeepsWatchdogCause(t *testing.T) {
	c := &diagnosticFake{scripts: []string{`printf '%s\n' 'error:temporary API failure' 'text:retrying'; exec sleep 30`}}
	frames := &diagnosticFrames{}
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	s, err := NewProvider(c).Open(context.Background(), context.Background(), provider.OpenOptions{Cwd: t.TempDir(), ResumeSessionID: "S0"}, func(raw json.RawMessage) {
		frames.receive(raw)
		// The parser has already buffered the preceding structured error.
		if strings.Contains(string(raw), "session_update") {
			cancel(fmt.Errorf("%w: idle limit reached", provider.ErrTurnTimeout))
		}
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	result, err := s.Prompt(ctx, "hi", nil)
	if !errors.Is(err, provider.ErrTurnTimeout) || result.StopReason != provider.StopReasonTimeout {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	text := assertDiagnosticBoundary(t, frames.take(), provider.StopReasonTimeout, 2)
	if !strings.Contains(text, "idle limit reached") || len(c.resumes) != 1 {
		t.Fatalf("watchdog cause=%q attempts=%d", text, len(c.resumes))
	}
}

func TestOneShotErrorDiagnosticRedactsCredentials(t *testing.T) {
	t.Setenv("CURSOR_API_KEY", "fake-env-credential")
	c := &diagnosticFake{scripts: []string{`printf '%s\n' \
		'Failed to reach the Cursor API: TLS handshake failed' \
		'header Authorization: Bearer fake-header-credential' \
		'api_key="fake quoted credential"' \
		'https://proxy:fake-password@proxy.example/connect?token=fake-query-credential' \
		"credential rejected: $CURSOR_API_KEY" >&2; exit 1`}}
	s, frames := openDiagnosticSession(t, c, "")
	_, err := s.Prompt(context.Background(), "hi", nil)
	if err == nil {
		t.Fatal("expected CLI failure")
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || !errors.Is(err, exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("redacted error must preserve native exit cause: %v", err)
	}
	text := assertDiagnosticBoundary(t, frames.take(), "failed", 1)
	for _, secret := range []string{"fake-env-credential", "fake-header-credential", "fake quoted credential", "fake-password", "fake-query-credential"} {
		if strings.Contains(text, secret) || strings.Contains(err.Error(), secret) {
			t.Fatalf("diagnostic exposed a credential: event=%q err=%v", text, err)
		}
	}
	if !strings.Contains(text, "TLS handshake failed") || !strings.Contains(text, "exit status 1") {
		t.Fatalf("redaction lost the actionable failure: %q", text)
	}
}
