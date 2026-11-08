"""Exercise the real double-forked helper with captured SSH-like pipes."""
import contextlib
import http.server
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import sys
import tempfile
import threading
import unittest
import urllib.request

SOURCE = sys.argv.pop()


class ProxyProcessTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.pid_file = self.root / "proxy.pid"
        self.hosts = self.root / "hosts"
        self.hosts.write_text("127.0.0.1 localhost\n")
        # Run before deleting the PID file, even when communicate times out.
        self.addCleanup(self.stop_daemon)

    def stop_daemon(self):
        if self.pid_file.exists():
            with contextlib.suppress(ProcessLookupError):
                os.kill(int(self.pid_file.read_text()), signal.SIGTERM)

    def ensure(self, mapping, port):
        launcher = self.root / "launch.py"
        launcher.write_text(
            "import importlib.util\n"
            f"spec = importlib.util.spec_from_file_location('proxy', {SOURCE!r})\n"
            "proxy = importlib.util.module_from_spec(spec)\n"
            "spec.loader.exec_module(proxy)\n"
            f"proxy._SPA_TO_API = {mapping!r}\n"
            f"proxy._LISTEN = ('127.0.0.1', {port})\n"
            f"proxy._PID_FILE = {str(self.pid_file)!r}\n"
            f"proxy._HOSTS_FILE = {str(self.hosts)!r}\n"
            f"proxy._LOG_FILE = {str(self.root / 'proxy.log')!r}\n"
            "raise SystemExit(proxy.ensure_daemon())\n"
        )
        child = subprocess.Popen(
            [sys.executable, "-I", "-B", str(launcher)],
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        )
        try:
            output, error = child.communicate(timeout=5)
        except subprocess.TimeoutExpired:
            self.stop_daemon()
            child.kill()
            child.communicate(timeout=5)
            self.fail("--ensure kept inherited stdin/stdout/stderr open")
        self.assertEqual(output, b"")
        return child.returncode, error

    def test_empty_map_is_a_noop(self):
        # An occupied listener must not matter when there is nothing to proxy.
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            listener.listen()
            code, error = self.ensure({}, listener.getsockname()[1])
        self.assertEqual((code, error), (0, b""))
        self.assertFalse(self.pid_file.exists())
        self.assertFalse((self.root / "proxy.log").exists())
        self.assertEqual(self.hosts.read_text(), "127.0.0.1 localhost\n")

    def test_daemon_releases_pipes_and_still_proxies(self):
        class Upstream(http.server.BaseHTTPRequestHandler):
            def do_POST(self):
                body = self.rfile.read(int(self.headers["Content-Length"]))
                result = json.dumps({
                    "path": self.path, "host": self.headers["Host"],
                    "auth": self.headers["Authorization"], "body": body.decode(),
                }).encode()
                self.send_response(200)
                self.send_header("Content-Length", str(len(result)))
                self.end_headers()
                self.wfile.write(result)

            def log_message(self, *args):
                pass

        upstream = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Upstream)
        worker = threading.Thread(target=upstream.serve_forever, daemon=True)
        worker.start()
        self.addCleanup(worker.join)
        self.addCleanup(upstream.server_close)
        self.addCleanup(upstream.shutdown)
        upstream_host = f"127.0.0.1:{upstream.server_port}"
        with socket.socket() as reserve:
            reserve.bind(("127.0.0.1", 0))
            port = reserve.getsockname()[1]
        mapping = {"mcp.test": upstream_host}
        code, error = self.ensure(mapping, port)
        self.assertEqual(code, 0, error)
        pid = self.pid_file.read_text()
        request = urllib.request.Request(
            f"http://127.0.0.1:{port}/mcp/runs/test?scope=artifact",
            data=b'{"method":"tools/list"}',
            headers={"Host": "mcp.test", "Authorization": "Bearer test-only"},
        )
        with urllib.request.urlopen(request, timeout=3) as response:
            self.assertEqual(json.load(response), {
                "path": "/mcp/runs/test?scope=artifact", "host": upstream_host,
                "auth": "Bearer test-only", "body": '{"method":"tools/list"}',
            })
        code, error = self.ensure(mapping, port)
        self.assertEqual(code, 0, error)
        self.assertEqual(self.pid_file.read_text(), pid, "--ensure started a second daemon")
        self.assertEqual(self.hosts.read_text().count("127.0.0.1 mcp.test"), 1)

    def test_bind_failure_is_reported(self):
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            listener.listen()
            code, error = self.ensure({"mcp.test": "127.0.0.1:1"}, listener.getsockname()[1])
        self.assertNotEqual(code, 0)
        self.assertIn(b"did not become ready", error)
        self.assertFalse(self.pid_file.exists())


if __name__ == "__main__":
    unittest.main()
