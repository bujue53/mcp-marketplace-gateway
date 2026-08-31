"""示例 MCP Server：数据库工具（纯标准库，示例数据）。"""

import json
import os
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


HTTP_PORT = int(os.environ.get("HTTP_PORT", "9005"))
PROTOCOL_VERSION = "2025-03-26"

TOOLS = [
    {
        "name": "db_query",
        "description": "执行只读 SQL 查询（示例实现）。",
        "inputSchema": {"type": "object", "properties": {"sql": {"type": "string"}}, "required": ["sql"]},
    },
    {
        "name": "db_schema",
        "description": "列出数据库中的表。",
        "inputSchema": {"type": "object", "properties": {}},
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
                "serverInfo": {"name": "mock-db-server", "version": "1.0.0"},
            }})
        if method == "tools/list":
            return self._json(200, {"jsonrpc": "2.0", "id": rid, "result": {"tools": TOOLS}})
        if method == "tools/call":
            params = req.get("params", {})
            name = params.get("name")
            if name == "db_schema":
                text = json.dumps({"tables": ["orders", "users", "products"]}, ensure_ascii=False)
            else:
                text = json.dumps({"rows": [{"id": 1, "status": "OK"}], "note": "示例查询结果"}, ensure_ascii=False)
            return self._json(200, {"jsonrpc": "2.0", "id": rid, "result": {"content": [{"type": "text", "text": text}]}})
        return self._json(200, {"jsonrpc": "2.0", "id": rid, "error": {"code": -32601, "message": "method not found"}})

    def log_message(self, *args):
        pass


def main():
    ThreadingHTTPServer(("0.0.0.0", HTTP_PORT), Handler).serve_forever()


if __name__ == "__main__":
    main()

