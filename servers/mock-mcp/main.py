"""原生 MCP Streamable HTTP 示例服务（纯标准库）。

实现了 MCP 所需的 initialize / tools/list / tools/call 等 JSON-RPC 方法，
用于演示「Agent 通过网关调用一个真正的 MCP Server」的路径。
"""

import json
import os
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


HTTP_PORT = int(os.environ.get("HTTP_PORT", "9003"))
PROTOCOL_VERSION = "2025-03-26"


def build_tool(name, desc, schema):
    return {"name": name, "description": desc, "inputSchema": schema}


TOOLS = [
    build_tool(
        "mcp_echo",
        "回显传入的文本，用于测试工具调用链路。",
        {"type": "object", "properties": {"text": {"type": "string"}}, "required": ["text"]},
    ),
    build_tool(
        "mcp_time",
        "返回服务器当前的 UTC 时间。",
        {"type": "object", "properties": {}},
    ),
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
        return self._json(405, {"jsonrpc": "2.0", "error": {"code": -32600, "message": "use POST"}})

    def do_POST(self):
        length = int(self.headers.get("Content-Length", "0"))
        raw = self.rfile.read(length) if length else b"{}"
        try:
            req = json.loads(raw)
        except Exception:
            return self._json(400, {"jsonrpc": "2.0", "id": None, "error": {"code": -32700, "message": "parse error"}})

        method = req.get("method")
        rid = req.get("id")

        if method == "initialize":
            return self._json(200, {
                "jsonrpc": "2.0", "id": rid,
                "result": {
                    "protocolVersion": PROTOCOL_VERSION,
                    "capabilities": {"tools": {"listChanged": False}},
                    "serverInfo": {"name": "mock-weather-mcp", "version": "1.0.0"},
                },
            })
        if method == "notifications/initialized":
            return self._json(202, {})
        if method == "ping":
            return self._json(200, {"jsonrpc": "2.0", "id": rid, "result": {}})
        if method == "tools/list":
            return self._json(200, {"jsonrpc": "2.0", "id": rid, "result": {"tools": TOOLS}})
        if method == "tools/call":
            params = req.get("params", {})
            name = params.get("name")
            args = params.get("arguments", {}) or {}
            if name == "mcp_echo":
                return self._json(200, {
                    "jsonrpc": "2.0", "id": rid,
                    "result": {"content": [{"type": "text", "text": json.dumps({"echo": args.get("text", "")}, ensure_ascii=False, indent=2)}]},
                })
            if name == "mcp_time":
                return self._json(200, {
                    "jsonrpc": "2.0", "id": rid,
                    "result": {"content": [{"type": "text", "text": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())}]},
                })
            return self._json(200, {
                "jsonrpc": "2.0", "id": rid,
                "result": {"content": [{"type": "text", "text": f"tool {name} not found"}], "isError": True},
            })
        return self._json(200, {
            "jsonrpc": "2.0", "id": rid,
            "error": {"code": -32601, "message": f"method not found: {method}"},
        })

    def log_message(self, *args):
        pass


def main():
    server = ThreadingHTTPServer(("0.0.0.0", HTTP_PORT), Handler)
    print(f"mock-mcp-server listening on :{HTTP_PORT}", flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()

