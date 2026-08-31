"""遗留 REST 天气服务（纯标准库）。

体现「工具孤岛」问题：一个无法被 Agent 直接调用的普通 REST API，
但暴露了 OpenAPI 3.x 文档（/openapi.json），供网关自动包装为 MCP 工具。
"""

import json
import os
import random
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlparse


HTTP_PORT = int(os.environ.get("HTTP_PORT", "9002"))


def load_spec():
    here = os.path.dirname(os.path.abspath(__file__))
    repo_root = os.path.dirname(os.path.dirname(here))
    candidates = [
        os.environ.get("OPENAPI_SPEC"),
        os.path.join(repo_root, "specs", "weather-v1.json"),
        os.path.join(os.getcwd(), "specs", "weather-v1.json"),
        os.path.join(here, "openapi.json"),
    ]
    for path in candidates:
        if path and os.path.exists(path):
            with open(path, encoding="utf-8") as f:
                return json.load(f)
    raise RuntimeError("openapi spec not found")


class Handler(BaseHTTPRequestHandler):
    server_version = "LegacyWeatherAPI/1.0"

    def _send(self, code, data):
        body = json.dumps(data, ensure_ascii=False).encode("utf-8")
        self.send_response(code)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        parsed = urlparse(self.path)
        query = parse_qs(parsed.query)
        if parsed.path == "/openapi.json":
            return self._send(200, self.server.spec)
        if parsed.path == "/health":
            return self._send(200, {"status": "ok"})

        if parsed.path == "/weather/current":
            city = (query.get("city") or ["东莞"])[0]
            return self._send(200, {
                "city": city,
                "temperature_c": round(random.uniform(18, 34), 1),
                "condition": random.choice(["晴", "多云", "小雨", "阴"]),
                "humidity": random.randint(40, 95),
                "updated_at": time.strftime("%Y-%m-%dT%H:%M:%S"),
            })
        if parsed.path == "/weather/forecast":
            city = (query.get("city") or ["东莞"])[0]
            days = min(int((query.get("days") or ["3"])[0]), 7)
            forecast = []
            for i in range(days):
                forecast.append({
                    "date": time.strftime("%Y-%m-%d", time.localtime(time.time() + i * 86400)),
                    "temp_low": round(random.uniform(15, 24), 1),
                    "temp_high": round(random.uniform(25, 35), 1),
                    "condition": random.choice(["晴", "多云", "小雨", "雷阵雨"]),
                })
            return self._send(200, {"city": city, "days": forecast})

        return self._send(404, {"error": "not found"})

    def do_POST(self):
        return self._send(405, {"error": "method not allowed"})

    def log_message(self, *args):
        pass


def main():
    server = ThreadingHTTPServer(("0.0.0.0", HTTP_PORT), Handler)
    server.spec = load_spec()
    print(f"legacy-weather-api listening on :{HTTP_PORT}", flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
