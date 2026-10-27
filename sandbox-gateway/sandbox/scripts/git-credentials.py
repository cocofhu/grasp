#!/usr/bin/env python3
"""File-only Git credentials. A request chooses one identity, never retries another."""

import json
import ipaddress
import os
from pathlib import Path
import re
import shlex
import shutil
import subprocess
import sys
import tempfile
from urllib.parse import unquote, urlsplit


KINDS = {"github_https", "gitlab_https", "ssh"}
HOST = re.compile(r"[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?\Z")
USER = re.compile(r"[A-Za-z0-9_.-]+\Z")


def valid_host(authority):
    try:
        parsed = urlsplit("//" + authority)
        if not authority or parsed.path or parsed.username or parsed.password:
            return False
        _ = parsed.port
        host = parsed.hostname or ""
        if ":" in host:
            return isinstance(ipaddress.ip_address(host), ipaddress.IPv6Address)
        return bool(HOST.fullmatch(host))
    except ValueError:
        return False


def location(value, kind):
    """Return (scheme, authority, repo). Reject config/protocol injection."""
    if not isinstance(value, str) or any(c.isspace() or ord(c) < 32 for c in value):
        raise ValueError("invalid scope")
    if not value:
        return ("", "", "") if kind == "ssh" else (
            "", "github.com" if kind == "github_https" else "gitlab.com", "")
    if "://" not in value and re.match(r"^[^/@:]+@", value):
        user, host_path = value.split("@", 1)
        if host_path.startswith("["):
            closing = host_path.find("]:")
            if closing < 0:
                raise ValueError("invalid SSH scope")
            value = "ssh://" + user + "@" + host_path[:closing + 1] + "/" + host_path[closing + 2:]
        else:
            value = "ssh://" + user + "@" + host_path.replace(":", "/", 1)
    if "://" not in value:
        if not valid_host(value):
            raise ValueError("invalid host")
        return "", value.lower(), ""
    parsed = urlsplit(value)
    if parsed.scheme not in ({"ssh"} if kind == "ssh" else {"http", "https"}):
        raise ValueError("wrong scope protocol")
    if parsed.password is not None or parsed.query or parsed.fragment:
        raise ValueError("invalid URL components")
    authority = parsed.netloc.rsplit("@", 1)[-1].lower()
    if not valid_host(authority):
        raise ValueError("invalid host")
    # Reading .port also rejects numeric ports outside their valid range.
    _ = parsed.port
    repo = unquote(parsed.path).strip("/")
    if any(c.isspace() or ord(c) < 32 for c in repo) or any(p in {".", ".."} for p in repo.split("/")):
        raise ValueError("invalid repository path")
    if repo.endswith(".git"):
        repo = repo[:-4]
    return parsed.scheme, authority, repo


def catalog(directory):
    if (directory / "manifest.json").is_symlink():
        raise ValueError("invalid catalog file")
    records = json.loads((directory / "manifest.json").read_text())
    if not isinstance(records, list):
        raise ValueError("invalid catalog")
    valid = []
    indexes, ids = set(), set()
    for record in records:
        if not isinstance(record, dict) or record.get("kind") not in KINDS:
            raise ValueError("invalid catalog record")
        index, identity = record.get("index"), record.get("id")
        if (isinstance(index, bool) or not isinstance(index, int) or index < 0 or index in indexes
                or not isinstance(identity, str) or not identity or identity in ids
                or not isinstance(record.get("preferred"), bool)):
            raise ValueError("invalid catalog identity")
        indexes.add(index)
        ids.add(identity)
        scope = location(record.get("scope", ""), record["kind"])
        secret = directory / f"{index}.secret"
        hosts = directory / f"{index}.known_hosts"
        if not secret.is_file() or secret.is_symlink() or not secret.read_bytes().strip() or hosts.is_symlink():
            raise ValueError("missing catalog credential")
        # Credential output is line-oriented; multiline tokens must not add fields.
        if record["kind"] != "ssh" and any(c in secret.read_text().strip() for c in "\r\n\0"):
            raise ValueError("invalid token")
        valid.append(dict(record, location=scope))
    return valid


def choose(records, scheme, host, repo, ssh=False, kind=None, ssh_api=False):
    """Exact repository > host > SSH global. Equal scopes keep the earlier row."""
    repo = repo.strip("/")
    if repo.endswith(".git"):
        repo = repo[:-4]
    candidates = []
    for order, record in enumerate(records):
        if kind and record["kind"] != kind:
            continue
        if (record["kind"] == "ssh") != ssh:
            continue
        scoped_scheme, scoped_host, scoped_repo = record["location"]
        if scoped_scheme and scoped_scheme != scheme and not (
                ssh_api and scheme == "ssh" and record["kind"] == "gitlab_https" and scoped_scheme in {"http", "https"}):
            continue
        if scoped_host and scoped_host != host.lower():
            continue
        if scoped_repo and scoped_repo != repo:
            continue
        score = (2 if scoped_repo else 1 if scoped_host else 0, -order)
        candidates.append((score, record))
    return max(candidates, key=lambda candidate: candidate[0])[1] if candidates else None


def credential(directory, operation):
    if operation != "get":
        return
    request = {}
    for line in sys.stdin:
        line = line.rstrip("\n")
        if not line:
            break
        key, sep, value = line.partition("=")
        if sep:
            request[key] = value
    protocol, host = request.get("protocol", ""), request.get("host", "")
    if protocol not in {"http", "https"} or not valid_host(host):
        print("quit=true\n")
        return
    record = choose(catalog(directory), protocol, host, request.get("path", ""))
    if record is None:
        print("quit=true\n")
        return
    token = (directory / f'{record["index"]}.secret').read_text().strip()
    user = "oauth2" if record["kind"] == "gitlab_https" else "x-access-token"
    print(f"username={user}\npassword={token}\n")


def write_private(path, body):
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    # Never follow an old file or an account-controlled link when installing.
    path.unlink(missing_ok=True)
    with path.open("w", encoding="utf-8") as stream:
        os.chmod(path, 0o600)
        stream.write(body)


def cli_configs(directory, records, home):
    """Write the CLI's per-host default offline; no token enters env or argv."""
    for kind, relative in (("github_https", ".config/gh/hosts.yml"),
                           ("gitlab_https", ".config/glab-cli/config.yml")):
        grouped = {}
        for record in records:
            if record["kind"] == kind:
                grouped.setdefault(record["location"][1], []).append(record)
        lines = [] if kind == "github_https" else ["git_protocol: https", "api_protocol: https", "hosts:"]
        for host, entries in grouped.items():
            entry = entries[0]
            token = (directory / f'{entry["index"]}.secret').read_text().strip()
            indent = "" if kind == "github_https" else "  "
            lines.append(f"{indent}{json.dumps(host)}:")
            key = "oauth_token" if kind == "github_https" else "token"
            lines.append(f"{indent}  {key}: {json.dumps(token)}")
            lines.append(f"{indent}  git_protocol: https")
            if kind == "github_https":
                # gh migrates legacy hosts without a user by querying /user.
                # A stable local account label keeps configuration offline.
                lines.append(f"{indent}  user: grasp-selected")
            if kind == "gitlab_https":
                protocol = "http" if entry["location"][0] == "http" else "https"
                lines.append(f"{indent}  api_protocol: {protocol}")
                lines.append(f"{indent}  api_host: {json.dumps(host)}")
        target = home / relative
        target.unlink(missing_ok=True)
        if grouped:
            write_private(target, "\n".join(lines) + "\n")


def install(source, home):
    records = catalog(source)
    directory = home / ".grasp/git-credentials"
    if directory.exists() or directory.is_symlink():
        if directory.is_symlink():
            directory.unlink()
        else:
            shutil.rmtree(directory)
    directory.mkdir(parents=True, mode=0o700)
    os.chmod(directory, 0o700)
    manifest = []
    for record in records:
        index = record["index"]
        for suffix in ("secret", "known_hosts"):
            entry = source / f"{index}.{suffix}"
            if entry.is_file() and not entry.is_symlink():
                body = entry.read_text()
                if suffix == "secret" and record["kind"] == "ssh" and not body.endswith("\n"):
                    body += "\n"
                write_private(directory / entry.name, body)
        manifest.append({key: value for key, value in record.items() if key != "location"})
    write_private(directory / "manifest.json", json.dumps(manifest))
    helper = directory / "helper.py"
    write_private(helper, Path(__file__).read_text())
    command = f"python3 {shlex.quote(str(helper))}"
    subprocess.run(["git", "config", "--global", "--unset-all", "credential.helper"], check=False,
                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    # The empty helper resets any inherited system helper before ours.
    for value in ("", "!" + command):
        subprocess.run(["git", "config", "--global", "--add", "credential.helper", value], check=True)
    subprocess.run(["git", "config", "--global", "credential.useHttpPath", "true"], check=True)
    subprocess.run(["git", "config", "--global", "core.sshCommand", command + " ssh"], check=True)
    cli_configs(directory, records, home)
    shutil.rmtree(source)


def ssh(directory, args):
    # Git probes ssh with -G before a real command. Describe an OpenSSH-compatible
    # variant without contacting a host or requiring a repository argument.
    if "-G" in args:
        return 0
    destination = None
    command_index = None
    skip = False
    port = ""
    user = ""
    for index, argument in enumerate(args):
        if skip:
            skip = False
            continue
        if argument in {"-p", "-l", "-o", "-i", "-F", "-J", "-S", "-b", "-c", "-D", "-E", "-e", "-L", "-m", "-O", "-Q", "-R", "-W", "-w"}:
            if index + 1 >= len(args):
                raise ValueError("invalid ssh arguments")
            if argument == "-p":
                port = args[index + 1]
            elif argument == "-l":
                user = args[index + 1]
            skip = True
        elif not argument.startswith("-"):
            destination, command_index = argument, index + 1
            break
    if not destination:
        raise ValueError("missing SSH destination")
    host = destination.rsplit("@", 1)[-1]
    if "@" in destination:
        user = destination.rsplit("@", 1)[0]
    # Git removes URL brackets before invoking SSH (git@::1 plus -p 2222).
    # Restore them for catalog matching while retaining its OpenSSH destination.
    authority_host = f"[{host}]" if ":" in host and not host.startswith("[") else host
    authority = authority_host.lower() + (":" + port if port else "")
    if not valid_host(authority) or (user and not USER.fullmatch(user)):
        raise ValueError("invalid SSH destination")
    remote = shlex.split(" ".join(args[command_index:]))
    if len(remote) != 2 or remote[0] not in {"git-upload-pack", "git-receive-pack", "git-upload-archive"}:
        raise ValueError("unsupported SSH command")
    record = choose(catalog(directory), "ssh", authority, remote[1], ssh=True)
    if record is None:
        raise ValueError("no selected SSH credential matches")
    key = directory / f'{record["index"]}.secret'
    hosts = directory / f'{record["index"]}.known_hosts'
    strict = "yes" if hosts.exists() and hosts.stat().st_size else "accept-new"
    if not hosts.exists():
        write_private(hosts, "")
    # Discard caller/config identities and agents: exactly one selected key may
    # authenticate. Keep only the parsed host/port/user and Git remote command.
    command = ["ssh", "-F", "/dev/null", "-i", str(key),
               "-o", "IdentitiesOnly=yes", "-o", "IdentityAgent=none",
               "-o", "PasswordAuthentication=no", "-o", "KbdInteractiveAuthentication=no",
               "-o", "UserKnownHostsFile=" + str(hosts), "-o", "GlobalKnownHostsFile=/dev/null",
               "-o", "StrictHostKeyChecking=" + strict]
    if port:
        if not port.isdigit() or not 0 < int(port) < 65536:
            raise ValueError("invalid SSH port")
        command.extend(["-p", port])
    if user and "@" not in destination:
        command.extend(["-l", user])
    command.extend([destination, " ".join(args[command_index:])])
    os.execvp("ssh", command)


def exec_cli(directory, tool, repo_url, arguments):
    if tool not in {"gh", "glab"} or not arguments or arguments[0] != "--":
        raise ValueError("invalid Git CLI request")
    kind = "github_https" if tool == "gh" else "gitlab_https"
    repo_kind = "ssh" if repo_url.startswith("ssh://") or "://" not in repo_url else kind
    scheme, host, repo = location(repo_url, repo_kind)
    if not scheme or not repo:
        raise ValueError("CLI requires a repository URL")
    record = choose(catalog(directory), scheme, host, repo, kind=kind, ssh_api=tool == "glab")
    if record is None or record["kind"] != kind:
        raise ValueError("no selected CLI credential matches")
    token = (directory / f'{record["index"]}.secret').read_text().strip()
    api_protocol = "http" if record["location"][0] == "http" else "https"
    if not token or any(c in token for c in "\r\n\0"):
        raise ValueError("invalid CLI token")
    environment = dict(os.environ)
    for name in ("GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN",
                 "GITLAB_TOKEN", "GLAB_TOKEN", "GITLAB_ACCESS_TOKEN"):
        environment.pop(name, None)
    if tool == "gh":
        environment["GH_TOKEN" if host == "github.com" else "GH_ENTERPRISE_TOKEN"] = token
        environment["GH_HOST"] = host
    else:
        environment["GITLAB_TOKEN"] = token
        environment["GITLAB_HOST"] = host
        # A repository-scoped API token may use a different protocol from the
        # host's CLI default. Isolate that request without rewriting defaults.
        with tempfile.TemporaryDirectory(prefix="cli-", dir=directory) as config:
            write_private(Path(config) / "config.yml", "hosts:\n" + f"  {json.dumps(host)}:\n"
                          + f"    api_protocol: {api_protocol}\n    api_host: {json.dumps(host)}\n"
                          + "    git_protocol: https\n")
            environment["GLAB_CONFIG_DIR"] = config
            return subprocess.run([tool] + arguments[1:], env=environment, check=False).returncode
    # This one child may consume the token. The sandbox/process environment,
    # command line and persisted runtime env contain none of it.
    return subprocess.run([tool] + arguments[1:], env=environment, check=False).returncode


def main():
    try:
        if len(sys.argv) == 4 and sys.argv[1] == "install":
            install(Path(sys.argv[2]), Path(sys.argv[3]))
        elif len(sys.argv) >= 2 and sys.argv[1] == "ssh":
            return ssh(Path(__file__).resolve().parent, sys.argv[2:])
        elif len(sys.argv) >= 6 and sys.argv[1] == "exec-cli":
            return exec_cli(Path(__file__).resolve().parent, sys.argv[2], sys.argv[3], sys.argv[4:])
        elif len(sys.argv) == 2:
            credential(Path(__file__).resolve().parent, sys.argv[1])
        else:
            raise ValueError("invalid arguments")
        return 0
    except (OSError, ValueError, TypeError, KeyError, subprocess.SubprocessError):
        # Do not include the exception: malformed records/CLI errors can contain
        # credentials. A failing helper must not fall through to another token.
        if len(sys.argv) == 2 and sys.argv[1] == "get":
            print("quit=true\n")
        print("grasp: Git credential configuration unavailable", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
