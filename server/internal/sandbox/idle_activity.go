package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// IdleActivity probes both agent turns and user processes inside the guest.
// Failure is deliberately unknown (an error), never evidence of inactivity.
func (s *Sandbox) IdleActivity(ctx context.Context) (bool, error) {
	if s.IsLocal() {
		return true, nil
	}
	cmd, err := newSafeCmd("python3", "-", s.workspaceDir())
	if err != nil {
		return false, err
	}
	out, err := s.creds().runInput(ctx, 10*time.Second, cmd, strings.NewReader(idleActivityScript))
	if err != nil {
		return false, err
	}
	var activity struct {
		Busy *bool `json:"busy"`
	}
	if err := json.Unmarshal(out, &activity); err != nil {
		return false, err
	}
	if activity.Busy == nil {
		return false, errors.New("sandbox activity probe omitted busy state")
	}
	return *activity.Busy, nil
}

// Shell children, disowned workspace processes and active agent turns keep a
// guest alive. Resident infrastructure is ignored; an idle shell alone does not
// keep an environment forever. The probe excludes itself and its SSH ancestry.
const idleActivityScript = `
import json, os, pathlib, sys, urllib.request
with urllib.request.urlopen('http://127.0.0.1:8765/api/runtime/activity', timeout=5) as r:
    activity = json.load(r)
if not isinstance(activity.get('busy'), bool):
    raise RuntimeError('runtime did not report activity')
if activity['busy']:
    print(json.dumps({'busy': True})); sys.exit(0)
procs = {}
for entry in pathlib.Path('/proc').iterdir():
    if not entry.name.isdigit(): continue
    try:
        stat = (entry / 'stat').read_text()
        rest = stat[stat.rfind(')')+2:].split()
        args = (entry / 'cmdline').read_bytes().replace(b'\0', b' ').decode(errors='replace')
        cwd = os.readlink(entry / 'cwd')
        procs[int(entry.name)] = (int(rest[1]), rest[0], args, cwd)
    except (FileNotFoundError, ProcessLookupError): pass
ignored = set()
pid = os.getpid()
while pid in procs and pid not in ignored:
    ignored.add(pid); pid = procs[pid][0]
ignored.update(activity.get('resident_pids') or [])
workspace = os.path.realpath(sys.argv[1]).rstrip('/')
shells = {'bash', 'sh', 'dash', 'zsh', 'fish', '-bash', '-sh', '-zsh'}
runtime_root = os.environ.get('GRASP_RUNTIME_ROOT') or '/opt/grasp-runtime'
runtime_scripts = os.path.join(runtime_root, 'current', 'scripts')
preview_scripts = {os.path.realpath(p) for p in [os.path.join(runtime_scripts, 'preview-inject.sh'), '/usr/local/bin/preview-inject.sh']}
supervisor_scripts = preview_scripts | {os.path.realpath(p) for p in [os.path.join(runtime_scripts, 'startup.sh'), os.path.join(runtime_scripts, 'services.sh'), os.path.join(runtime_scripts, 'grasp-bootstrap.sh'), '/usr/local/bin/services.sh', '/grasp-bootstrap.sh', '/run/grasp-e2b/bootstrap.sh', '/run/grasp-e2b/relay.py']}
preview_binaries = {os.path.realpath(p) for p in [os.path.join(runtime_root, 'current', 'bin', 'preview-inject'), '/usr/local/bin/preview-inject']}
def base(args):
    return os.path.basename(args.split()[0]) if args.split() else ''
def script_path(args):
    parts = args.split()
    if not parts:
        return None
    script = parts[1] if base(args) in (shells | {'python', 'python3'}) and len(parts) > 1 else parts[0]
    return os.path.realpath(script) if os.path.isabs(script) else None
def infrastructure(args):
    name = base(args)
    # Match installed supervisor scripts, never a user script with the same
    # filename. Only these processes are ignored; their children are inspected.
    if script_path(args) in supervisor_scripts | preview_binaries:
        return True
    return name in {'backend', 'sshd', 'systemd', 'init', 'tini', 'dumb-init', 'supervisord', 'dockerd', 'containerd', 'containerd-shim-runc-v2', 'Xvfb', 'Xtigervnc', 'xfce4-session', 'xfwm4', 'dbus-daemon'} or '/code-server/' in args or '/vscode/' in args
busy = False
for pid, (ppid, state, args, cwd) in procs.items():
    if pid in ignored or state == 'Z' or not args: continue
    name = base(args)
    if infrastructure(args): continue
    if name in shells:
        # An executing shell script is work even when it has no children yet.
        parts = args.split()
        if '-c' not in parts and len(parts) == 1: continue
        if '-l' in parts and len(parts) <= 2: continue
    parent = procs.get(ppid)
    # The preview supervisor polls its child with sleep. Other children, even
    # under a trusted supervisor, can still represent user work.
    if name == 'sleep' and args.split()[1:] in [['2'], ['5']] and parent is not None and script_path(parent[2]) in preview_scripts:
        continue
    in_workspace = cwd == workspace or cwd.startswith(workspace + '/')
    shell_child = parent is not None and base(parent[2]) in shells
    if in_workspace or shell_child:
        busy = True; break
print(json.dumps({'busy': busy}))
`
