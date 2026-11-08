#!/usr/bin/env python3
"""grasp-mcp-spa-proxy: optional SPA host → API ingress proxy (sandbox-local).

When _SPA_TO_API is non-empty, maps SPA Host headers to an API upstream.
The public tree ships an empty map (no rewrite). Prefer setting mcp_advertise
to a URL that already serves /mcp/runs/:id.

Usage:
  grasp-mcp-spa-proxy --ensure   # idempotent install+daemonize
  grasp-mcp-spa-proxy            # foreground (for debugging)
"""
from __future__ import annotations

import argparse
import os
import signal
import sys
import time
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

# Keep in sync with artifact-upload _SPA_MCP_HOSTS (empty in public tree).
_SPA_TO_API = {}
_LISTEN = ("127.0.0.1", 80)
_PID_FILE = "/var/run/grasp-mcp-spa-proxy.pid"
_LOG_FILE = "/var/log/grasp-mcp-spa-proxy.log"
_HOSTS_FILE = "/etc/hosts"
_HOSTS_MARK = "# grasp-mcp-spa-proxy"


def eprint(*a):
    print(*a, file=sys.stderr)


def ensure_hosts() -> None:
    path = _HOSTS_FILE
    try:
        with open(path, "r", encoding="utf-8", errors="replace") as f:
            cur = f.read()
    except OSError as e:
        eprint(f"grasp-mcp-spa-proxy: read /etc/hosts failed: {e}")
        return
    lines = []
    changed = False
    for host in _SPA_TO_API:
        entry = f"127.0.0.1 {host} {_HOSTS_MARK}"
        if entry in cur or f"127.0.0.1 {host}" in cur:
            continue
        lines.append(entry)
        changed = True
    if not changed:
        return
    try:
        with open(path, "a", encoding="utf-8") as f:
            if not cur.endswith("\n"):
                f.write("\n")
            f.write("\n".join(lines) + "\n")
    except OSError as e:
        eprint(f"grasp-mcp-spa-proxy: write /etc/hosts failed: {e}")


def upstream_for_host(host: str) -> str | None:
    host = (host or "").split(":")[0].lower()
    return _SPA_TO_API.get(host)


class _Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, fmt, *args):
        eprint("grasp-mcp-spa-proxy:", fmt % args)

    def _proxy(self):
        host = self.headers.get("Host", "")
        api = upstream_for_host(host)
        if not api:
            self.send_error(404, "unknown SPA mcp host")
            return
        url = f"http://{api}{self.path}"
        length = int(self.headers.get("Content-Length") or 0)
        body = self.rfile.read(length) if length > 0 else None
        headers = {
            k: v
            for k, v in self.headers.items()
            if k.lower() not in ("host", "content-length", "connection", "transfer-encoding")
        }
        headers["Host"] = api
        req = urllib.request.Request(url, data=body, method=self.command, headers=headers)
        try:
            with urllib.request.urlopen(req, timeout=120) as resp:
                data = resp.read()
                self.send_response(resp.getcode())
                for k, v in resp.headers.items():
                    if k.lower() in ("transfer-encoding", "connection", "content-length"):
                        continue
                    self.send_header(k, v)
                self.send_header("Content-Length", str(len(data)))
                self.end_headers()
                self.wfile.write(data)
        except urllib.error.HTTPError as e:
            detail = e.read() if e.fp else b""
            self.send_response(e.code)
            self.send_header("Content-Type", e.headers.get("Content-Type", "text/plain"))
            self.send_header("Content-Length", str(len(detail)))
            self.end_headers()
            if detail:
                self.wfile.write(detail)
        except Exception as e:
            msg = f"upstream error: {e}".encode()
            self.send_response(502)
            self.send_header("Content-Type", "text/plain; charset=utf-8")
            self.send_header("Content-Length", str(len(msg)))
            self.end_headers()
            self.wfile.write(msg)

    def do_GET(self):
        self._proxy()

    def do_POST(self):
        self._proxy()

    def do_PUT(self):
        self._proxy()

    def do_DELETE(self):
        self._proxy()

    def do_PATCH(self):
        self._proxy()

    def do_HEAD(self):
        self._proxy()

    def do_OPTIONS(self):
        self._proxy()


def already_running() -> bool:
    try:
        with open(_PID_FILE, "r", encoding="utf-8") as f:
            pid = int(f.read().strip())
    except (OSError, ValueError):
        return False
    try:
        os.kill(pid, 0)
        return True
    except OSError:
        return False


def write_pid():
    try:
        with open(_PID_FILE, "w", encoding="utf-8") as f:
            f.write(str(os.getpid()))
    except OSError as e:
        eprint(f"grasp-mcp-spa-proxy: pid file: {e}")


def serve_forever():
    ensure_hosts()
    httpd = ThreadingHTTPServer(_LISTEN, _Handler)
    write_pid()
    eprint(f"grasp-mcp-spa-proxy: listening on {_LISTEN[0]}:{_LISTEN[1]}")
    try:
        httpd.serve_forever()
    finally:
        httpd.server_close()
        try:
            os.remove(_PID_FILE)
        except OSError:
            pass


def ensure_daemon() -> int:
    if not _SPA_TO_API:
        return 0
    ensure_hosts()
    if already_running():
        eprint("grasp-mcp-spa-proxy: already running")
        return 0
    # Double-fork daemon so SSH seedHelpers returns immediately.
    if os.fork() > 0:
        # Parent: give the child a moment to bind :80 / write pid.
        for _ in range(20):
            time.sleep(0.1)
            if already_running():
                return 0
        eprint("grasp-mcp-spa-proxy: daemon did not become ready")
        return 1
    os.setsid()
    if os.fork() > 0:
        os._exit(0)
    # Reassigning sys.stdout leaves fd 1 (and sys.__stdout__) attached to the
    # SSH channel. The daemon must release the actual inherited descriptors so
    # its caller can receive EOF and finish without waiting for a timeout.
    with open("/dev/null", "rb") as stdin, open(_LOG_FILE, "ab", buffering=0) as log:
        os.dup2(stdin.fileno(), 0)
        os.dup2(log.fileno(), 1)
        os.dup2(log.fileno(), 2)
    signal.signal(signal.SIGHUP, signal.SIG_IGN)
    serve_forever()
    return 0


def main() -> int:
    ap = argparse.ArgumentParser(prog="grasp-mcp-spa-proxy")
    ap.add_argument(
        "--ensure",
        action="store_true",
        help="idempotent: update /etc/hosts and daemonize if not running",
    )
    args = ap.parse_args()
    if args.ensure:
        return ensure_daemon()
    serve_forever()
    return 0


if __name__ == "__main__":
    sys.exit(main())
