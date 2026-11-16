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
        exe = os.path.realpath(os.readlink(entry / 'exe'))
        procs[int(entry.name)] = (int(rest[1]), rest[0], args, cwd, exe)
    except (FileNotFoundError, ProcessLookupError): pass
ignored = set()
pid = os.getpid()
while pid in procs and pid not in ignored:
    ignored.add(pid); pid = procs[pid][0]
ignored.update(activity.get('resident_pids') or [])
workspace = os.path.realpath(sys.argv[1]).rstrip('/')
runtime_root = os.environ.get('GRASP_RUNTIME_ROOT') or '/opt/grasp-runtime'
runtime_scripts = os.path.join(runtime_root, 'current', 'scripts')
def paths(values):
    return {os.path.realpath(p) for p in values}
preview_scripts = paths([os.path.join(runtime_scripts, 'preview-inject.sh'), '/usr/local/bin/preview-inject.sh'])
supervisor_scripts = preview_scripts | paths([
    os.path.join(runtime_scripts, 'startup.sh'), os.path.join(runtime_scripts, 'services.sh'),
    os.path.join(runtime_scripts, 'grasp-bootstrap.sh'), '/usr/local/bin/services.sh',
    '/grasp-bootstrap.sh', '/run/grasp-e2b/bootstrap.sh', '/run/grasp-e2b/relay.py', '/usr/bin/supervisord',
])
system_bins = ['/usr/bin', '/usr/sbin', '/bin', '/sbin']
native_names = ['sshd', 'systemd', 'init', 'tini', 'dumb-init', 'dockerd', 'containerd',
                'containerd-shim-runc-v2', 'Xvfb', 'Xtigervnc', 'xfce4-session', 'xfwm4', 'dbus-daemon']
native_binaries = paths([os.path.join(root, name) for root in system_bins for name in native_names])
native_binaries |= paths([os.path.join(runtime_root, 'current', 'bin', name)
                         for name in ['backend', 'preview-inject']])
native_binaries |= paths(['/usr/local/bin/backend', '/usr/local/bin/preview-inject'])
shell_binaries = paths([os.path.join(root, name) for root in ['/usr/bin', '/bin']
                        for name in ['bash', 'sh', 'dash', 'zsh', 'fish']])
interpreters = shell_binaries | paths(['/usr/bin/python', '/usr/bin/python3', '/bin/python', '/bin/python3'])
sleep_binaries = paths(['/usr/bin/sleep', '/bin/sleep'])
code_server = os.path.realpath('/usr/lib/code-server')
node_binaries = paths(['/usr/bin/node', '/usr/local/bin/node', '/usr/lib/code-server/lib/node'])
def script_path(args, exe):
    parts = args.split()
    if exe not in interpreters | node_binaries or len(parts) < 2:
        return None
    return os.path.realpath(parts[1]) if os.path.isabs(parts[1]) else None
def infrastructure(args, exe):
    # Exempt installed executable/script identities, never filenames or text
    # mentioned in user arguments. Their children are still inspected.
    script = script_path(args, exe)
    if exe in native_binaries or (exe in interpreters and script in supervisor_scripts):
        return True
    return exe in node_binaries and script is not None and (script == code_server or script.startswith(code_server + '/'))
busy = False
for pid, (ppid, state, args, cwd, exe) in procs.items():
    if pid in ignored or state == 'Z' or not args: continue
    if infrastructure(args, exe): continue
    if exe in shell_binaries:
        # An executing shell script is work even when it has no children yet.
        parts = args.split()
        if '-c' not in parts and len(parts) == 1: continue
        if '-l' in parts and len(parts) <= 2: continue
    parent = procs.get(ppid)
    # The preview supervisor polls its child with sleep. Other children, even
    # under a trusted supervisor, can still represent user work.
    if exe in sleep_binaries and args.split()[1:] in [['2'], ['5']] and parent is not None and script_path(parent[2], parent[4]) in preview_scripts:
        continue
    in_workspace = cwd == workspace or cwd.startswith(workspace + '/')
    shell_child = parent is not None and parent[4] in shell_binaries
    if in_workspace or shell_child:
        busy = True; break
print(json.dumps({'busy': busy}))
`
