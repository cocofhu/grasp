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
def base(args):
    return os.path.basename(args.split()[0]) if args.split() else ''
def infrastructure(args):
    name = base(args)
    return name in {'backend', 'sshd', 'systemd', 'init', 'tini', 'dumb-init', 'supervisord', 'dockerd', 'containerd', 'containerd-shim-runc-v2', 'Xvfb', 'Xtigervnc', 'xfce4-session', 'xfwm4', 'dbus-daemon'} or '/code-server/' in args or '/vscode/' in args or 'preview-inject' in args or '/grasp-bootstrap.sh' in args or 'services.sh' in args
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
    in_workspace = cwd == workspace or cwd.startswith(workspace + '/')
    shell_child = parent is not None and base(parent[2]) in shells
    if in_workspace or shell_child:
        busy = True; break
print(json.dumps({'busy': busy}))
`
