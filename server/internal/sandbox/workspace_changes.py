"""Git tree capture and bounded review payloads for one sandbox workspace."""

import base64
import json
import os
import re
import shutil
import subprocess
import tempfile

MAX_FILES = 2000
MAX_FILE_BYTES = 512 * 1024
MAX_CONTENT_BYTES = 6 * 1024 * 1024
OID = re.compile(r"(?:[0-9a-f]{40}|[0-9a-f]{64})\Z")
DIFF_FLAGS = ("--no-color", "--no-ext-diff", "--no-textconv", "--find-renames")


def git_command(repo, *args):
    return [
        "git", "--literal-pathspecs", "-C", repo,
        "-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", *args,
    ]


def git(repo, *args, env=None, limit=None):
    with subprocess.Popen(git_command(repo, *args), stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, env=env) as proc:
        out = proc.stdout.read() if limit is None else proc.stdout.read(limit + 1)
        if limit is not None and len(out) > limit:
            proc.kill()
            proc.wait()
            return None
        if proc.wait():
            raise ValueError("Git operation failed")
        return out


def optional(repo, *args):
    try:
        return git(repo, *args).rstrip(b"\n").decode()
    except (ValueError, UnicodeError):
        return ""


def repository(root, folder):
    if not folder or folder in (".", "..") or "/" in folder or "\\" in folder:
        raise ValueError("invalid repository directory")
    repo = os.path.join(root, folder)
    if os.path.realpath(repo) != repo or optional(repo, "rev-parse", "--show-toplevel") != repo:
        raise ValueError("repository unavailable")
    return repo


def clean_filters(repo):
    # Capture is a platform read operation. A repository-local Git filter
    # may invoke a program, so use Git's built-in conversions only.
    names = optional(repo, "config", "--name-only", "--get-regexp", r"^filter\..*\.(clean|process|required)$")
    return [arg for key in names.splitlines() for arg in ("-c", key + ("=false" if key.endswith(".required") else "="))]


def capture(repo, pin):
    head = optional(repo, "rev-parse", "--verify", "HEAD")
    branch = optional(repo, "symbolic-ref", "--quiet", "--short", "HEAD")
    index = git(repo, "rev-parse", "--path-format=absolute", "--git-path", "index").rstrip(b"\n")
    with tempfile.TemporaryDirectory(prefix="grasp-review-") as scratch:
        copy = os.path.join(scratch, "index")
        try:
            with open(index, "rb") as source, open(copy, "wb") as destination:
                stamp = os.fstat(source.fileno()).st_mtime_ns
                shutil.copyfileobj(source, destination)
        except FileNotFoundError:
            pass  # A new repository may have no index yet.
        else:
            # Git rechecks contents when a file's mtime matches the index.
            # Use the timestamp of the same index we read, even if Git replaced it.
            os.utime(copy, ns=(stamp, stamp))
        env = dict(os.environ, GIT_INDEX_FILE=copy, GIT_TERMINAL_PROMPT="0")
        git(repo, "-c", "core.splitIndex=false", *clean_filters(repo), "add", "--all", "--", ".", env=env)
        tree = git(repo, "write-tree", env=env).rstrip(b"\n").decode()
    if head and tree == optional(repo, "rev-parse", "HEAD^{tree}"):
        tree = head
    elif pin:
        if not re.fullmatch(r"refs/grasp/changes/[0-9a-f]{64}", pin):
            raise ValueError("invalid baseline ref")
        git(repo, "update-ref", pin, tree)
    return {"ref": tree, "head": head, "branch": branch}


def changed_files(repo, first, last):
    fields = iter(git(repo, "diff", *DIFF_FLAGS, "--raw", "--no-abbrev", "-z", first, last, "--").split(b"\0"))
    for header in fields:
        if not header:
            break
        _, _, old_object, new_object, code = header.split()
        status = chr(code[0])
        path = next(fields)
        previous = path if status in ("R", "C") else b""
        if previous:
            path = next(fields)
        yield status, path, previous, old_object, new_object


def change_counts(repo, first, last):
    fields = iter(git(repo, "diff", *DIFF_FLAGS, "--numstat", "-z", first, last, "--").split(b"\0"))
    counts = {}
    for field in fields:
        if not field:
            break
        plus, minus, path = field.split(b"\t", 2)
        if not path:
            next(fields)  # old path for a rename or copy
            path = next(fields)
        counts[path] = (None, None) if plus == b"-" else (int(plus), int(minus))
    return counts


def batch_query(process, object_id):
    process.stdin.write(object_id + b"\n")
    process.stdin.flush()
    return process.stdout.readline().split()


def read_blob(metadata, blobs, object_id, limit):
    if not object_id.strip(b"0"):
        return None
    info = batch_query(metadata, object_id)
    if len(info) != 2 or info[0] != b"blob":
        return None
    size = int(info[1])
    if size > limit:
        return None
    header = batch_query(blobs, object_id)
    if len(header) != 3 or header[1] != b"blob" or int(header[2]) != size:
        raise ValueError("unexpected Git blob header")
    data = blobs.stdout.read(size)
    if len(data) != size or blobs.stdout.read(1) != b"\n":
        raise ValueError("incomplete Git blob")
    try:
        data.decode("utf-8")
    except UnicodeDecodeError:
        return None
    return data


def encode(value):
    return base64.b64encode(value).decode()


def compare(repo, first, last):
    if not OID.fullmatch(first) or not OID.fullmatch(last):
        raise ValueError("invalid snapshot")
    if first == last:
        return {"added": 0, "deleted": 0, "fileCount": 0, "files": []}
    changes = list(changed_files(repo, first, last))
    counts = change_counts(repo, first, last)
    added = sum(plus or 0 for plus, _ in counts.values())
    deleted = sum(minus or 0 for _, minus in counts.values())
    result = {"added": added, "deleted": deleted, "fileCount": len(changes), "files": []}
    remaining = MAX_CONTENT_BYTES
    pipes = dict(stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
    with (
        subprocess.Popen(git_command(repo, "cat-file", "--batch-check=%(objecttype) %(objectsize)"), **pipes) as metadata,
        subprocess.Popen(git_command(repo, "cat-file", "--batch"), **pipes) as blobs,
    ):
        for status, path, previous, old_object, new_object in changes[:MAX_FILES]:
            plus, minus = counts.get(path, (0, 0))
            row = {"path": os.fsdecode(path), "status": status, "added": plus, "deleted": minus}
            if previous:
                row["previousPath"] = os.fsdecode(previous)
            result["files"].append(row)
            if plus is None or remaining <= 0:
                continue
            # Read by object ID so the batch protocol never sees filenames.
            limit = min(MAX_FILE_BYTES, remaining)
            before = read_blob(metadata, blobs, old_object, limit)
            after = read_blob(metadata, blobs, new_object, limit)
            paths = [os.fsdecode(p) for p in (previous, path) if p]
            patch = git(repo, "diff", *DIFF_FLAGS, first, last, "--", *paths, limit=limit)
            for key, value in (("patch", patch), ("before", before), ("after", after)):
                if value is not None and len(value) <= remaining:
                    row[key] = encode(value)
                    remaining -= len(value)
    return result


def main(request):
    root = os.path.realpath(request["workspace"])
    results = []
    for query in request["repositories"]:
        try:
            repo = repository(root, query["directory"])
            if request["operation"] == "capture":
                result = capture(repo, query.get("pin", ""))
            else:
                result = compare(repo, query["from"], query["to"])
        except (OSError, ValueError, StopIteration):
            result = {"unavailable": True}
        results.append(result)
    return results


print(json.dumps(main(request), ensure_ascii=True, separators=(",", ":")))
