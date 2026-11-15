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
		name        string
		processes   map[string][]any
		runtimeRoot string
		busy        bool
	}{
		{name: "idle infrastructure", processes: map[string][]any{"1": {0, "init", "/"}, "10": {1, "/usr/bin/node|/usr/lib/code-server/out/node/entry.js", "/workspace"}, "11": {10, "bash", "/workspace"}, "20": {1, "agent-cli|acp", "/workspace"}}},
		{name: "idle runtime startup supervisor", processes: map[string][]any{"10": {1, "/bin/bash|/opt/grasp-runtime/current/scripts/startup.sh", "/workspace"}, "11": {10, "/usr/bin/node|/usr/lib/code-server/out/node/entry.js", "/workspace"}}},
		{name: "custom runtime startup supervisor", runtimeRoot: "/custom/runtime", processes: map[string][]any{"10": {1, "/bin/bash|/custom/runtime/current/scripts/startup.sh", "/workspace"}}},
		{name: "idle E2B supervisors and preview poll", processes: map[string][]any{"10": {1, "bash", "/"}, "11": {10, "python3|/run/grasp-e2b/relay.py|/run/grasp-e2b", "/"}, "12": {10, "bash|/run/grasp-e2b/bootstrap.sh", "/"}, "13": {1, "bash|/usr/local/bin/preview-inject.sh", "/workspace"}, "14": {13, "sleep|5", "/"}}},
		{name: "installed preview binary", processes: map[string][]any{"10": {1, "/usr/local/bin/preview-inject|--listen|19999", "/workspace"}}},
		{name: "installed backend binary", processes: map[string][]any{"10": {1, "/usr/local/bin/backend|-listen|:8765", "/workspace"}}},
		{name: "installed sshd with changed process title", processes: map[string][]any{"10": {1, "sshd:|listener", "/workspace", "/usr/sbin/sshd"}}},
		{name: "user shell binary remains work", processes: map[string][]any{"10": {1, "bash", "/workspace", "/workspace/bash"}}, busy: true},
		{name: "user sleep binary under preview remains work", processes: map[string][]any{"10": {1, "bash|/usr/local/bin/preview-inject.sh", "/workspace"}, "11": {10, "sleep|5", "/", "/workspace/sleep"}}, busy: true},
		{name: "user backend binary remains work", processes: map[string][]any{"10": {1, "./backend", "/workspace", "/workspace/backend"}}, busy: true},
		{name: "user sshd binary remains work", processes: map[string][]any{"10": {1, "sshd", "/workspace", "/workspace/sshd"}}, busy: true},
		{name: "spoofed platform argv remains work", processes: map[string][]any{"10": {1, "/usr/local/bin/backend", "/workspace", "/workspace/program"}}, busy: true},
		{name: "user code-server directory remains work", processes: map[string][]any{"10": {1, "python3|/workspace/code-server/build.py", "/workspace"}}, busy: true},
		{name: "user vscode directory remains work", processes: map[string][]any{"10": {1, "node|/workspace/vscode/build.js", "/workspace"}}, busy: true},
		{name: "installed node running user work remains work", processes: map[string][]any{"10": {1, "/usr/lib/code-server/lib/node|/workspace/build.js", "/workspace"}}, busy: true},
		{name: "node eval mentioning installed script remains work", processes: map[string][]any{"10": {1, "node|-e|require('/usr/lib/code-server/out/node/entry.js')", "/workspace"}}, busy: true},
		{name: "user interpreter named bash remains work", processes: map[string][]any{"10": {1, "bash|/opt/grasp-runtime/current/scripts/startup.sh", "/workspace", "/workspace/bash"}}, busy: true},
		{name: "user preview script remains work", processes: map[string][]any{"10": {1, "bash|/workspace/preview-inject.sh", "/workspace"}}, busy: true},
		{name: "user services script remains work", processes: map[string][]any{"10": {1, "bash|/workspace/services.sh", "/workspace"}}, busy: true},
		{name: "user bootstrap-like script remains work", processes: map[string][]any{"10": {1, "bash|/workspace/grasp-bootstrap.sh", "/workspace"}}, busy: true},
		{name: "command mentioning preview remains work", processes: map[string][]any{"10": {1, "python3|/workspace/build.py|--label|preview-inject", "/workspace"}}, busy: true},
		{name: "preview long sleep remains work", processes: map[string][]any{"10": {1, "bash|/usr/local/bin/preview-inject.sh", "/workspace"}, "11": {10, "sleep|3600", "/"}}, busy: true},
		{name: "user relay remains work", processes: map[string][]any{"10": {1, "python3|/workspace/relay.py", "/workspace"}}, busy: true},
		{name: "user bootstrap remains work", processes: map[string][]any{"10": {1, "bash|/workspace/bootstrap.sh", "/workspace"}}, busy: true},
		{name: "user sleep remains work", processes: map[string][]any{"10": {1, "bash", "/tmp"}, "11": {10, "sleep|5", "/tmp"}}, busy: true},
		{name: "preview non-poll child remains work", processes: map[string][]any{"10": {1, "bash|/usr/local/bin/preview-inject.sh", "/workspace"}, "11": {10, "make", "/tmp"}}, busy: true},
		{name: "user startup script remains work", processes: map[string][]any{"10": {1, "/bin/bash|/workspace/startup.sh", "/workspace"}}, busy: true},
		{name: "user runtime-like path remains work", processes: map[string][]any{"10": {1, "/bin/bash|/workspace/opt/grasp-runtime/current/scripts/startup.sh", "/workspace"}}, busy: true},
		{name: "trusted startup child remains work", processes: map[string][]any{"10": {1, "/bin/bash|/opt/grasp-runtime/current/scripts/startup.sh", "/workspace"}, "11": {10, "make", "/tmp"}}, busy: true},
		{name: "shell mentioning runtime startup remains work", processes: map[string][]any{"10": {1, "/bin/bash|-c|/opt/grasp-runtime/current/scripts/startup.sh", "/workspace"}}, busy: true},
		{name: "detached build", processes: map[string][]any{"22": {1, "python3|build.py", "/workspace/repo"}}, busy: true},
		{name: "terminal command", processes: map[string][]any{"11": {1, "bash", "/tmp"}, "22": {11, "make", "/tmp"}}, busy: true},
		{name: "ACP terminal remains work", processes: map[string][]any{"22": {1, "node|long-task.js", "/workspace"}}, busy: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload, _ := json.Marshal(tc.processes)
			quoted, _ := json.Marshal(string(payload))
			runtimeRoot, _ := json.Marshal(tc.runtimeRoot)
			// Execute the actual probe with deterministic /proc and runtime
			// endpoints, including NUL-delimited Linux command lines.
			setup := `import io,json,os,pathlib,sys,urllib.request
fixtureProcs=json.loads(` + string(quoted) + `)
os.environ['GRASP_RUNTIME_ROOT']=` + string(runtimeRoot) + `
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
realReadlink=os.readlink
def readlink(p,**kwargs):
 if not isinstance(p,ProcPath): return realReadlink(p,**kwargs)
 row=fixtureProcs[p.p.split('/')[2]]
 if p.name=='cwd': return row[2]
 if len(row)>3: return row[3]
 arg=row[1].split('|')[0]
 return arg if arg.startswith('/') else '/usr/bin/'+arg
os.readlink=readlink
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
