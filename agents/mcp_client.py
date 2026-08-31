"""Agent 测试客户端：通过网关调用 MCP 工具（端到端演示）。

用法示例:
    # 登录并列出市场工具
    python agents/mcp_client.py --list

    # 通过 MCP /mcp 端点调用自动生成的天气工具
    python agents/mcp_client.py --mcp \
        --tool weather_current --args '{"city":"东莞"}'

    # 通过 REST 网关入口调用
    python agents/mcp_client.py --rest --tool weather_forecast --args '{"city":"深圳","days":2}'
"""

import argparse
import json
import sys
import urllib.request
import urllib.error


def post(base, path, payload, token=None, content_type="application/json"):
    url = base.rstrip("/") + path
    data = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(url, data=data, method="POST")
    req.add_header("Content-Type", content_type)
    req.add_header("Accept", "application/json, text/event-stream")
    if token:
        req.add_header("Authorization", f"Bearer {token}")
    try:
        with urllib.request.urlopen(req, timeout=20) as resp:
            return resp.status, resp.read().decode("utf-8")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8")


def login(base, user, password):
    code, body = post(base, "/api/auth/login", {"username": user, "password": password})
    if code != 200:
        print(f"[login 失败] {code} {body}", file=sys.stderr)
        sys.exit(2)
    return json.loads(body)["token"]


def mcp_flow(base, token, tool=None, args=None):
    # initialize
    code, body = post(base, "/mcp", {"jsonrpc": "2.0", "id": 1, "method": "initialize"}, token)
    if code != 200:
        print(f"[initialize 失败] {code} {body}", file=sys.stderr)
        sys.exit(2)
    print("== initialize ==")
    print(body)

    # tools/list
    code, body = post(base, "/mcp", {"jsonrpc": "2.0", "id": 2, "method": "tools/list"}, token)
    data = json.loads(body)
    tools = data.get("result", {}).get("tools", [])
    print(f"\n== tools/list (共 {len(tools)} 个)==")
    for t in tools:
        print(f"  - {t['name']}: {t.get('description', '')[:60]}")

    if tool:
        code, body = post(
            base,
            "/mcp",
            {"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": {"name": tool, "arguments": args or {}}},
            token,
        )
        print(f"\n== tools/call {tool} ==")
        obj = json.loads(body)
        result = obj.get("result", {})
        for c in result.get("content", []):
            print(c.get("text", ""))


def rest_flow(base, token, tool, args):
    code, body = post(base, "/api/gateway/invoke", {"tool_name": tool, "arguments": args or {}}, token)
    obj = json.loads(body)
    print(f"== REST invoke {tool} ==")
    print(f"trace_id   : {obj.get('trace_id')}")
    print(f"cost_micros: {obj.get('cost_micros')}")
    print(f"latency_ms : {obj.get('latency_ms')}")
    for c in obj.get("result", {}).get("content", []):
        print(c.get("text", ""))


def main():
    p = argparse.ArgumentParser(description="MCP Gateway Agent 客户端")
    p.add_argument("--base", default="http://localhost:8080")
    p.add_argument("--user", default="admin")
    p.add_argument("--password", default="admin123")
    p.add_argument("--mcp", action="store_true", help="走 MCP /mcp 端点")
    p.add_argument("--rest", action="store_true", help="走 REST /api/gateway/invoke 端点")
    p.add_argument("--list", action="store_true", help="列出工具")
    p.add_argument("--tool", default="")
    p.add_argument("--args", default="{}", help="JSON 参数")
    args = p.parse_args()

    token = login(args.base, args.user, args.password)
    print(f"[登录成功] token = {token[:24]}...\n")
    try:
        payload = json.loads(args.args)
    except json.JSONDecodeError:
        payload = {}

    if args.list or args.mcp:
        mcp_flow(args.base, token, args.tool or None, payload)
    elif args.rest or args.tool:
        rest_flow(args.base, token, args.tool, payload)


if __name__ == "__main__":
    main()

