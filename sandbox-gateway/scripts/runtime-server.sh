#!/usr/bin/env bash
# Serve one runtime bundle the way the Grasp server does (/sandbox-inject/*.tgz
# behind a Bearer token), for bootstrap tests. Runs in the foreground.
# Usage: runtime-server.sh <bundle.tgz> <port> <token>
set -euo pipefail
[ $# -eq 3 ] || { echo "usage: $0 <bundle.tgz> <port> <token>" >&2; exit 2; }
exec python3 - "$@" <<'PY'
import http.server, sys

bundle, port, token = sys.argv[1], int(sys.argv[2]), sys.argv[3]

class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        if self.headers.get("Authorization") != "Bearer " + token:
            self.send_response(401)
            self.end_headers()
            return
        if not (self.path.startswith("/sandbox-inject/") and self.path.endswith(".tgz")):
            self.send_response(404)
            self.end_headers()
            return
        with open(bundle, "rb") as f:
            data = f.read()
        self.send_response(200)
        self.send_header("Content-Type", "application/gzip")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def log_message(self, fmt, *args):
        sys.stderr.write("runtime-server: " + (fmt % args) + "\n")

http.server.ThreadingHTTPServer(("0.0.0.0", port), H).serve_forever()
PY
