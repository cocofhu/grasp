package driver

import "strings"

// BuildColdStartCommand is for a fresh Kubernetes process root. It never
// provides a persistent-Docker-root authorization cleanup contract. URLs and
// credentials stay in the private environment, outside the command string.
func BuildColdStartCommand(opts StartOptions) (string, error) {
	if err := ValidateColdStartOptions(opts); err != nil {
		return "", err
	}
	// Do not permit an interpreter/loader hook to execute user state before
	// the configuration proof. The Python verifier also runs in isolated mode.
	for key := range opts.ColdConfig.Env {
		if key == "BASH_ENV" || key == "ENV" || strings.HasPrefix(key, "LD_") {
			return "", ErrColdStartConflict
		}
	}
	return coldStartPrelude + strings.TrimPrefix(opts.BootstrapScript, "#!/bin/bash\n"), nil
}

const coldStartPrelude = `#!/bin/bash
set -euo pipefail
umask 077
/usr/bin/python3 -I -S - <<'GRASP_COLD_CONFIGURATION_PY'
import gzip, hashlib, io, json, os, pathlib, re, shutil, stat, sys, tarfile, tempfile, time, urllib.parse, urllib.request

SSH_ROOT = '/tmp/grasp-ssh-inject'
MAX_ARCHIVE = 64 * 1024 * 1024
MAX_EXPANDED = 128 * 1024 * 1024
MAX_ENTRIES = 8192
MAX_TAR = MAX_EXPANDED + MAX_ENTRIES * 8192
DEADLINE = time.monotonic() + 120

def check_time():
    if time.monotonic() >= DEADLINE:
        raise ValueError('deadline')

def safe_path(value):
    p = pathlib.Path(value)
    if not p.is_absolute() or str(p) != value or '..' in p.parts or any(ord(x) < 32 for x in value):
        raise ValueError('path')
    current = pathlib.Path('/')
    for part in p.parts[1:]:
        current = current / part
        if current.is_symlink():
            raise ValueError('symlink ancestor')
        if current.exists() and current != p and not current.is_dir():
            raise ValueError('ancestor')
    return p

def overlap(a, b):
    return a == b or a in b.parents or b in a.parents

def private_directory(path):
    safe_path(str(path))
    path.mkdir(parents=True, exist_ok=True, mode=0o700)
    if not path.is_dir() or stat.S_IMODE(path.stat().st_mode) != 0o700:
        raise ValueError('private directory')

def read_private(path, maximum):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd, 'rb') as f:
        info = os.fstat(f.fileno())
        if not stat.S_ISREG(info.st_mode) or stat.S_IMODE(info.st_mode) != 0o600 or info.st_size < 1 or info.st_size > maximum:
            raise ValueError('private file')
        data = f.read(maximum + 1)
        if len(data) != info.st_size:
            raise ValueError('changed file')
        return data

class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        raise ValueError('redirect')

def download(item, target, headers):
    check_time()
    u = urllib.parse.urlsplit(item['url'])
    if u.scheme not in ('http', 'https') or not u.netloc or u.username or u.password:
        raise ValueError('download source')
    opener = urllib.request.build_opener(NoRedirect(), urllib.request.ProxyHandler({}))
    request = urllib.request.Request(item['url'], headers=headers)
    fd = os.open(target, os.O_CREAT | os.O_EXCL | os.O_WRONLY | os.O_NOFOLLOW, 0o600)
    digest, size = hashlib.sha256(), 0
    with os.fdopen(fd, 'wb') as f, opener.open(request, timeout=min(15, max(1, DEADLINE-time.monotonic()))) as response:
        if response.status != 200:
            raise ValueError('download status')
        while True:
            check_time()
            block = response.read(min(65536, item['bytes'] + 1 - size))
            if not block:
                break
            size += len(block)
            if size > item['bytes']:
                raise ValueError('download quota')
            digest.update(block)
            f.write(block)
        f.flush()
        os.fsync(f.fileno())
    if size != item['bytes'] or digest.hexdigest() != item['sha256']:
        raise ValueError('download proof')

def inspect_archive(raw, destination):
    # Bound decompression before tarfile parses PAX/long-name headers. Those
    # headers can otherwise allocate unbounded data before yielding a member.
    unpacked = io.BytesIO()
    with gzip.GzipFile(fileobj=io.BytesIO(raw), mode='rb') as compressed:
        while True:
            check_time()
            block = compressed.read(min(65536, MAX_TAR + 1 - unpacked.tell()))
            if not block:
                break
            unpacked.write(block)
            if unpacked.tell() > MAX_TAR:
                raise ValueError('tar quota')
    unpacked.seek(0)
    members, seen, expanded = [], {}, 0
    with tarfile.open(fileobj=unpacked, mode='r:') as archive:
        for member in archive:
            check_time()
            name = member.name.rstrip('/')
            path = pathlib.PurePosixPath(name)
            if path.is_absolute() or '..' in path.parts or str(path) != name or not name or len(name) > 4096 or len(path.parts) > 64 or any(ord(x) < 32 for x in name):
                raise ValueError('archive path')
            if name in seen or not (member.isdir() or member.isfile()) or member.size < 0 or member.size > 16*1024*1024 or (member.isdir() and member.size != 0):
                raise ValueError('archive member')
            seen[name] = member.isdir()
            expanded += member.size
            if len(seen) > MAX_ENTRIES or expanded > MAX_EXPANDED:
                raise ValueError('archive quota')
            data = None
            if member.isfile():
                data = archive.extractfile(member).read(member.size + 1)
                if len(data) != member.size:
                    raise ValueError('archive short file')
            members.append((name, member.isdir(), member.mode, data))
    for name in seen:
        for ancestor in pathlib.PurePosixPath(name).parents:
            if str(ancestor) in seen and not seen[str(ancestor)]:
                raise ValueError('archive file ancestor')
    expected = 'git/manifest.json' if destination == SSH_ROOT else 'mcp.json'
    mandatory = [m for m in members if m[0] == expected and not m[1]]
    if len(mandatory) != 1:
        raise ValueError('configuration manifest')
    content = json.loads(mandatory[0][3])
    if destination == SSH_ROOT:
        if not isinstance(content, list) or len(content) > 256:
            raise ValueError('Git manifest')
    elif not isinstance(content, dict):
        raise ValueError('MCP manifest')
    return members

def verified_archive(path, item):
    raw = read_private(path, MAX_ARCHIVE)
    if len(raw) != item['bytes'] or hashlib.sha256(raw).hexdigest() != item['sha256']:
        raise ValueError('cache proof')
    return inspect_archive(raw, item['destination'])

def extract_validated(destination, members):
    root = safe_path(destination)
    if root.exists():
        if not root.is_dir():
            raise ValueError('configuration root')
        shutil.rmtree(root)
    root.mkdir(parents=True, mode=0o700)
    for name, directory, mode, data in members:
        check_time()
        path = root / name
        safe_path(str(path))
        if directory:
            path.mkdir(parents=True, exist_ok=True, mode=0o700)
            continue
        path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
        fd = os.open(path, os.O_CREAT | os.O_EXCL | os.O_WRONLY | os.O_NOFOLLOW, 0o700 if mode & 0o100 else 0o600)
        with os.fdopen(fd, 'wb') as f:
            f.write(data)

def run():
    request_id = os.environ['GRASP_COLD_REQUEST_ID']
    if not re.fullmatch('[A-Za-z0-9_-]{8,128}', request_id):
        raise ValueError('request')
    root = safe_path(os.environ['CONFIG_ROOT'])
    workspace = safe_path(os.environ['WORKSPACE_DIR'])
    control = safe_path(os.environ['GRASP_WORKSPACE_CONTROL_ROOT'])
    if not str(root).startswith('/root/') or root == pathlib.Path('/root') or overlap(root, workspace) or overlap(control, workspace):
        raise ValueError('reserved path')
    safe_path(SSH_ROOT)
    items = json.loads(os.environ['GRASP_COLD_INJECTIONS'])
    if not isinstance(items, list) or len(items) != 2:
        raise ValueError('injections')
    seen = set()
    for item in items:
        if not isinstance(item, dict) or set(item) != {'url','destination','sha256','bytes'} or item['destination'] not in (str(root), SSH_ROOT) or item['destination'] in seen or not isinstance(item['bytes'],int) or isinstance(item['bytes'],bool) or not 1 <= item['bytes'] <= MAX_ARCHIVE or not re.fullmatch('[a-f0-9]{64}',item['sha256']):
            raise ValueError('injection identity')
        seen.add(item['destination'])
    if seen != {str(root),SSH_ROOT}:
        raise ValueError('injection set')
    items.sort(key=lambda x: x['destination'])
    cache_parent = control.parent / 'authorization'
    if overlap(cache_parent,workspace) or overlap(cache_parent,root) or overlap(cache_parent,pathlib.Path(SSH_ROOT)):
        raise ValueError('cache placement')
    private_directory(cache_parent)
    cache = cache_parent / request_id
    safe_path(str(cache))
    intent = json.dumps({'protocol':'grasp-cold-configuration-cache/v1','requestId':request_id,'parts':[{k:x[k] for k in ('destination','sha256','bytes')} for x in items]},sort_keys=True,separators=(',',':')).encode()
    if not cache.exists():
        headers = {}
        for line in os.environ.get('SANDBOX_INJECT_HEADERS','').splitlines():
            name,sep,value = line.partition(':')
            if not sep or not re.fullmatch('[A-Za-z0-9-]+',name) or name.lower() in {x.lower() for x in headers}:
                raise ValueError('headers')
            headers[name] = value.strip()
        stage = pathlib.Path(tempfile.mkdtemp(prefix='.'+request_id+'-',dir=cache_parent))
        try:
            for i,item in enumerate(items):
                download(item,stage/('archive-'+str(i)+'.tgz'),headers)
            for i,item in enumerate(items):
                verified_archive(stage/('archive-'+str(i)+'.tgz'),item)
            fd = os.open(stage/'intent.json',os.O_CREAT|os.O_EXCL|os.O_WRONLY|os.O_NOFOLLOW,0o600)
            with os.fdopen(fd,'wb') as f:
                f.write(intent); f.flush(); os.fsync(f.fileno())
            # The immutable canonical cache appears only after both complete
            # readbacks and semantic checks. An unpublished stage grants no
            # authority and is cleaned if validation fails.
            os.rename(stage,cache)
            fd = os.open(cache_parent,os.O_RDONLY|os.O_DIRECTORY)
            try: os.fsync(fd)
            finally: os.close(fd)
        finally:
            if stage.exists(): shutil.rmtree(stage)
    private_directory(cache)
    if read_private(cache/'intent.json',16384) != intent:
        raise ValueError('cache intent')
    validated = []
    for i,item in enumerate(items):
        validated.append((item['destination'],verified_archive(cache/('archive-'+str(i)+'.tgz'),item)))
    # Every archive passes full readback and semantics before changing either
    # private destination. No extraction traverses links or the workspace.
    for destination,members in validated:
        extract_validated(destination,members)

try:
    run()
except Exception:
    sys.stderr.write('cold prestart configuration verification failed\n')
    sys.exit(1)
GRASP_COLD_CONFIGURATION_PY
export SANDBOX_INJECT=''
unset GRASP_COLD_INJECTIONS
export GRASP_BOOTSTRAP_FORCE_FETCH=1
`
