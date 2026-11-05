#!/usr/bin/env python3
"""Private E2B transport adapter; no dependency on the Grasp runtime bundle."""

import base64
import hashlib
import hmac
import http.client
import json
import os
import re
import socket
import struct
import sys
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlsplit

MAX_FRAME = 2 * 1024 * 1024
DENIED_PORTS = {8081, 49983}


def valid_port(port):
    return type(port) is int and 1 <= port <= 65535 and port not in DENIED_PORTS


def exact(stream, length):
    chunks = []
    while length:
        part = stream.read(length)
        if not part:
            raise EOFError()
        chunks.append(part)
        length -= len(part)
    return b"".join(chunks)


def rewrite_cdp(value, target):
    if isinstance(value, dict):
        for key, entry in list(value.items()):
            if key == "webSocketDebuggerUrl" and isinstance(entry, str):
                url = urlsplit(entry)
                value[key] = "ws://" + target + url.path + ("?" + url.query if url.query else "")
            else:
                rewrite_cdp(entry, target)
    elif isinstance(value, list):
        for entry in value:
            rewrite_cdp(entry, target)


class RelayServer(ThreadingHTTPServer):
    daemon_threads = True

    def __init__(self, address, root, config):
        if not isinstance(config.get("secret"), str) or not config["secret"]:
            raise ValueError("relay secret is required")
        ports = config.get("ports", [])
        if not isinstance(ports, list) or any(not valid_port(p) for p in ports):
            raise ValueError("invalid port allowlist")
        self.root, self.config = root, config
        self.allowed, self.lock = set(ports), threading.Lock()
        super().__init__(address, Handler)


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *_):
        pass

    def authenticated(self):
        supplied = self.headers.get("X-Grasp-Relay-Key", "").encode()
        return hmac.compare_digest(supplied, self.server.config["secret"].encode())

    def reply(self, status, value):
        data = json.dumps(value).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.send_header("Connection", "close")
        self.end_headers()
        self.wfile.write(data)
        self.close_connection = True

    def do_POST(self):
        if not self.authenticated():
            self.reply(403, {"error": "forbidden"})
            return
        if self.path != "/_bridge/ports":
            self.reply(404, {"error": "not found"})
            return
        try:
            length = int(self.headers.get("Content-Length", "0"))
            if not 0 < length <= 8192 or self.headers.get("Transfer-Encoding"):
                raise ValueError()
            ports = json.loads(exact(self.rfile, length))["ports"]
            if not isinstance(ports, list) or any(not valid_port(p) for p in ports):
                raise ValueError()
            with self.server.lock:
                self.server.allowed.update(ports)
            self.reply(200, {"ok": True})
        except (ValueError, KeyError, TypeError, EOFError):
            self.reply(400, {"error": "invalid ports"})

    def do_GET(self):
        if not self.authenticated():
            self.reply(403, {"error": "forbidden"})
            return
        parsed = urlsplit(self.path)
        if parsed.path == "/_bridge/health":
            ports = {}
            for port in (22, 8765, 8744):
                try:
                    with socket.create_connection(("127.0.0.1", port), .3):
                        pass
                    ports[str(port)] = True
                except OSError:
                    ports[str(port)] = False
            code = None
            try:
                with open(os.path.join(self.server.root, "bootstrap.exit")) as handle:
                    code = int(handle.read(32).strip())
            except (OSError, ValueError):
                pass
            workspace = None
            try:
                path = self.server.config.get("workspace", "/root/workspace")
                stats = os.statvfs(path)
                workspace = {"path": path,
                             "totalBytes": stats.f_blocks * stats.f_frsize,
                             "freeBytes": stats.f_bfree * stats.f_frsize,
                             "availableBytes": stats.f_bavail * stats.f_frsize}
            except (OSError, TypeError, ValueError):
                pass
            self.reply(200, {"marker": self.server.config.get("marker", ""),
                             "ports": ports, "bootstrapExit": code,
                             "workspace": workspace})
            return
        if parsed.path == "/_bridge/logs":
            try:
                with open(os.path.join(self.server.root, "bootstrap.log"), "rb") as handle:
                    handle.seek(0, 2)
                    handle.seek(max(0, handle.tell() - 1048576))
                    content = handle.read(1048576).decode(errors="replace")
            except OSError:
                content = ""
            self.reply(200, {"content": content})
            return
        if parsed.path == "/_bridge/cdp":
            self.cdp()
            return
        query = parse_qs(parsed.query)
        try:
            port = int(query["port"][0])
        except (KeyError, ValueError, IndexError):
            port = 0
        if parsed.path != "/_bridge/tcp" or self.headers.get("Upgrade", "").lower() != "websocket":
            self.reply(404, {"error": "not found"})
            return
        with self.server.lock:
            allowed = valid_port(port) and port in self.server.allowed
        if not allowed:
            self.reply(403, {"error": "port not published"})
            return
        self.tunnel(port)

    def cdp(self):
        path = self.headers.get("X-Grasp-CDP-Path", "/json/version")
        target = self.headers.get("X-Grasp-CDP-Host", "")
        if path not in ("/json/version", "/json/list", "/json", "/json/") or not re.fullmatch(r"127\.0\.0\.1:[0-9]{1,5}", target):
            self.reply(400, {"error": "invalid CDP discovery"})
            return
        with self.server.lock:
            allowed = 9222 in self.server.allowed
        if not allowed:
            self.reply(403, {"error": "port not published"})
            return
        conn = http.client.HTTPConnection("127.0.0.1", 9222, timeout=10)
        try:
            conn.request("GET", path, headers={"Host": "127.0.0.1:9222"})
            response = conn.getresponse()
            data = response.read(4194305)
            if response.status != 200 or len(data) > 4194304:
                self.reply(502, {"error": "CDP unavailable"})
                return
            value = json.loads(data)
            rewrite_cdp(value, target)
            self.reply(200, value)
        except (OSError, ValueError, http.client.HTTPException):
            self.reply(502, {"error": "CDP unavailable"})
        finally:
            conn.close()

    def tunnel(self, port):
        key = self.headers.get("Sec-WebSocket-Key", "")
        try:
            if len(base64.b64decode(key, validate=True)) != 16:
                raise ValueError()
        except ValueError:
            self.reply(400, {"error": "invalid websocket key"})
            return
        protocols = [p.strip() for p in self.headers.get("Sec-WebSocket-Protocol", "").split(",")]
        if self.headers.get("Sec-WebSocket-Version") != "13" or "binary" not in protocols:
            self.reply(400, {"error": "invalid websocket protocol"})
            return
        try:
            upstream = socket.create_connection(("127.0.0.1", port), 10)
        except OSError:
            self.reply(502, {"error": "target unavailable"})
            return
        try:
            self.stream(upstream, key)
        finally:
            upstream.close()

    def stream(self, upstream, key):
        upstream.settimeout(None)  # Connect deadlines must not become idle deadlines.
        upstream.setsockopt(socket.IPPROTO_TCP, socket.TCP_NODELAY, 1)
        self.connection.setsockopt(socket.IPPROTO_TCP, socket.TCP_NODELAY, 1)
        accept = base64.b64encode(hashlib.sha1((key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").encode()).digest()).decode()
        self.send_response(101)
        self.send_header("Upgrade", "websocket")
        self.send_header("Connection", "Upgrade")
        self.send_header("Sec-WebSocket-Accept", accept)
        self.send_header("Sec-WebSocket-Protocol", "binary")
        self.end_headers()
        self.close_connection = True
        write_lock = threading.Lock()

        def send(data, opcode=2):
            length = len(data)
            header = bytes([128 | opcode, length]) if length < 126 else bytes([128 | opcode, 126]) + struct.pack("!H", length) if length < 65536 else bytes([128 | opcode, 127]) + struct.pack("!Q", length)
            with write_lock:
                self.wfile.write(header + data)
                self.wfile.flush()

        def output():
            try:
                while True:
                    data = upstream.recv(65536)
                    if not data:
                        break
                    send(data)
            except OSError:
                pass
            finally:
                try:
                    send(struct.pack("!H", 1000), 8)
                except OSError:
                    pass
                try:
                    self.connection.shutdown(socket.SHUT_RDWR)
                except OSError:
                    pass

        writer = threading.Thread(target=output, daemon=True)
        writer.start()
        try:
            fragmented = None
            message = bytearray()
            while True:
                header = exact(self.rfile, 2)
                final, opcode = bool(header[0] & 128), header[0] & 15
                length = header[1] & 127
                if header[0] & 112 or not header[1] & 128:
                    raise ValueError()
                if length == 126:
                    length = struct.unpack("!H", exact(self.rfile, 2))[0]
                elif length == 127:
                    length = struct.unpack("!Q", exact(self.rfile, 8))[0]
                if length > MAX_FRAME or (opcode >= 8 and (length > 125 or not final)):
                    raise ValueError()
                mask = exact(self.rfile, 4)
                data = exact(self.rfile, length)
                data = bytes(value ^ mask[index % 4] for index, value in enumerate(data))
                if opcode == 8:
                    break
                if opcode == 9:
                    send(data, 10)
                    continue
                if opcode == 10:
                    continue
                if opcode in (1, 2):
                    if fragmented is not None:
                        raise ValueError()
                    fragmented = opcode
                elif opcode != 0 or fragmented is None:
                    raise ValueError()
                message.extend(data)
                if len(message) > MAX_FRAME:
                    raise ValueError()
                if not final:
                    continue
                if fragmented == 1:
                    if message != b"EOF":
                        raise ValueError()
                    upstream.shutdown(socket.SHUT_WR)
                else:
                    upstream.sendall(message)
                fragmented, message = None, bytearray()
        except (OSError, EOFError, ValueError):
            pass
        finally:
            try:
                upstream.shutdown(socket.SHUT_RDWR)
            except OSError:
                pass
            upstream.close()
            writer.join(timeout=2)


def main():
    root = sys.argv[1]
    with open(os.path.join(root, "relay.json")) as handle:
        config = json.load(handle)
    RelayServer(("0.0.0.0", 8081), root, config).serve_forever()


if __name__ == "__main__":
    main()
