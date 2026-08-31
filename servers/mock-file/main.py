"""示例 MCP Server：文件类工具（纯标准库）。"""

import json
import os
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


HTTP_PORT = int(os.environ.get("HTTP_PORT", "9004"))
PROTOCOL_VERSION = "2025-03-26"

TOOLS = [
    {
        "name": "file_read",
        "description": "读取指定文本文件的内容片段。",
        "inputSchema": {"type": "object", "properties": {"path": {"type": "string"}}, "required": ["path"]},
    },
    {
        "name": "file_list",
        "description": "列出目录下的文件名。",
        "inputSchema": {"type": "object", "properties": {"dir": {"type": "string"}}},
    },
]


class Handler(BaseHTTPRequestHandler):
    def _json(self, code, obj):
        body = json.dumps(obj, ensure_ascii=False).encode("utf-8")
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path.startswith("/health"):
            return self._json(200, {"status": "ok"})
        return self._json(405, {"error": "use POST"})

    def do_POST(self):
        raw = self.rfile.read(int(self.headers.get("Content-Length", "0")))
        req = json.loads(raw or b"{}")
        method, rid = req.get("method"), req.get("id")
        if method == "initialize":
            return self._json(200, {"jsonrpc": "2.0", "id": rid, "result": {
                "protocolVersion": PROTOCOL_VERSION,
                "capabilities": {"tools": {"listChanged": False}},
                "serverInfo": {"name": "mock-file-server", "version": "1.0.0"},
            }})
        if method in ("notifications/initialized",):
            return self._json(202, {})
        if method == "tools/list":
            return self._json(200, {"jsonrpc": "2.0", "id": rid, "result": {"tools": TOOLS}})
        if method == "tools/call":
            params = req.get("params", {})
            name = params.get("name")
            if name == "file_list":
                return self._json(200, {"jsonrpc": "2.0", "id": rid, "result": {"content": [
                    {"type": "text", "text": json.dumps(["/data/report.pdf", "/data/2026-08-31.sql"], ensure_ascii=False)}
                ]}})
            return self._json(200, {"jsonrpc": "2.0", "id": rid, "result": {"content": [
                {"type": "text", "text": "（示例）读取到文件内容：这是一份伪造的报表内容。"}
            ]}})
        return self._json(200, {"jsonrpc": "2.0", "id": rid, "error": {"code": -32601, "message": "method not found"}})

    def log_message(self, *args):
        pass


def main():
    ThreadingHTTPServer(("0.0.0.0", HTTP_PORT), Handler).serve_forever()


if __name__ == "__main__":
    main()

