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

func TestIdleActivityUsesStdinAndFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name      string
		response  string
		failure   error
		busy      bool
		wantError bool
	}{
		{name: "idle", response: `{"busy":false}`},
		{name: "background process", response: `{"busy":true}`, busy: true},
		{name: "missing state", response: `{}`, wantError: true},
		{name: "malformed state", response: `oops`, wantError: true},
		{name: "unreachable", failure: errors.New("SSH failed"), wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			t.Cleanup(SetExecHook(func(_ context.Context, _ string, _ int, command string, stdin io.Reader) ([]byte, error) {
				called = true
				body, err := io.ReadAll(stdin)
				if err != nil || command != "'python3' '-' '/workspace'" || !strings.Contains(string(body), "/api/runtime/activity") {
					t.Fatalf("probe must send script on stdin: command=%q err=%v", command, err)
				}
				return []byte(tc.response), tc.failure
			}))
			busy, err := (&Sandbox{WorkspaceDir: "/workspace"}).IdleActivity(context.Background())
			if !called || busy != tc.busy || (err != nil) != tc.wantError {
				t.Fatalf("called=%v busy=%v error=%v", called, busy, err)
			}
		})
	}
}

func TestIdleActivityProcessProbeProtectsBackgroundWork(t *testing.T) {
	for _, tc := range []struct {
		name      string
		processes map[string][]any
		busy      bool
	}{
		{name: "idle infrastructure", processes: map[string][]any{"1": {0, "init", "/"}, "10": {1, "/usr/bin/node|/usr/lib/code-server/out/node/entry.js", "/workspace"}, "11": {10, "bash", "/workspace"}, "20": {1, "agent-cli|acp", "/workspace"}}},
		{name: "detached build", processes: map[string][]any{"22": {1, "python3|build.py", "/workspace/repo"}}, busy: true},
		{name: "terminal command", processes: map[string][]any{"11": {1, "bash", "/tmp"}, "22": {11, "make", "/tmp"}}, busy: true},
		{name: "ACP terminal remains work", processes: map[string][]any{"22": {1, "node|long-task.js", "/workspace"}}, busy: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload, _ := json.Marshal(tc.processes)
			quoted, _ := json.Marshal(string(payload))
			// Execute the actual probe with deterministic /proc and runtime
			// endpoints, including NUL-delimited Linux command lines.
			setup := `import io,json,os,pathlib,sys,urllib.request
fixtureProcs=json.loads(` + string(quoted) + `)
class ProcPath:
 def __init__(self,p): self.p=str(p)
 @property
 def name(self): return self.p.split('/')[-1]
 def iterdir(self): return [ProcPath('/proc/'+p) for p in fixtureProcs]
 def __truediv__(self,s): return ProcPath(self.p+'/'+s)
 def read_text(self):
  pid=self.p.split('/')[2]; return pid+' (process) S '+str(fixtureProcs[pid][0])
 def read_bytes(self): return fixtureProcs[self.p.split('/')[2]][1].replace('|','\0').encode()
pathlib.Path=ProcPath
os.readlink=lambda p:fixtureProcs[p.p.split('/')[2]][2]
os.getpid=lambda:999
urllib.request.urlopen=lambda *a,**kw:io.BytesIO(b'{"busy":false,"resident_pids":[20]}')
sys.argv=['probe','/workspace']
`
			cmd := exec.Command("python3", "-")
			cmd.Stdin = strings.NewReader(setup + idleActivityScript)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("probe failed: %v %s", err, out)
			}
			var got struct {
				Busy bool `json:"busy"`
			}
			if json.Unmarshal(out, &got) != nil || got.Busy != tc.busy {
				t.Fatalf("busy=%v want %v: %s", got.Busy, tc.busy, out)
			}
		})
	}
}
