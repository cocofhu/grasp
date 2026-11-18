package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"strings"
	"testing"
)

func TestRuntimeActivityUsesStdinAndFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name      string
		response  string
		failure   error
		busy      bool
		wantError bool
	}{
		{name: "idle", response: `{"busy":false}`},
		{name: "model turn", response: `{"busy":true}`, busy: true},
		{name: "missing state", response: `{}`, wantError: true},
		{name: "malformed state", response: `oops`, wantError: true},
		{name: "unreachable", failure: errors.New("SSH failed"), wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			t.Cleanup(SetExecHook(func(_ context.Context, _ string, _ int, command string, stdin io.Reader) ([]byte, error) {
				called = true
				body, err := io.ReadAll(stdin)
				if err != nil || command != "'python3' '-'" || !strings.Contains(string(body), "/api/runtime/activity") {
					t.Fatalf("probe must send script on stdin: command=%q err=%v", command, err)
				}
				return []byte(tc.response), tc.failure
			}))
			busy, err := (&Sandbox{WorkspaceDir: "/workspace"}).RuntimeActivity(context.Background())
			if !called || busy != tc.busy || (err != nil) != tc.wantError {
				t.Fatalf("called=%v busy=%v error=%v", called, busy, err)
			}
		})
	}
}

// Resident desktop, browser and terminal processes are not model work: only
// the runtime's turn/queue state decides.
func TestRuntimeActivityScriptReportsOnlyRuntimeState(t *testing.T) {
	for _, tc := range []struct {
		name    string
		runtime string
		busy    bool
		fails   bool
	}{
		{name: "idle with resident processes", runtime: `{"busy":false,"resident_pids":[20]}`},
		{name: "turn", runtime: `{"busy":true,"reason":"turn:default"}`, busy: true},
		{name: "queue", runtime: `{"busy":true,"reason":"queue:default"}`, busy: true},
		{name: "missing state", runtime: `{}`, fails: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setup := "import io,urllib.request\nurllib.request.urlopen=lambda *a,**kw:io.BytesIO(b'" + tc.runtime + "')\n"
			cmd := exec.Command("python3", "-")
			cmd.Stdin = strings.NewReader(setup + runtimeActivityScript)
			out, err := cmd.CombinedOutput()
			if tc.fails {
				if err == nil {
					t.Fatalf("probe must fail without busy state: %s", out)
				}
				return
			}
			var got struct {
				Busy bool `json:"busy"`
			}
			if err != nil || json.Unmarshal(out, &got) != nil || got.Busy != tc.busy {
				t.Fatalf("busy=%v want %v: %v %s", got.Busy, tc.busy, err, out)
			}
		})
	}
}
