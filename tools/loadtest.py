"""网关调用压测脚本（Python 标准库，无第三方依赖）。

用法:
    python tools/loadtest.py --rps 50 --seconds 5 --tool weather_current

输出: 吞吐量、成功率、P50/P95/P99 延迟、成本汇总。
"""

import argparse
import json
import threading
import time
import urllib.request
import urllib.error


def login(base, user, password):
    req = urllib.request.Request(
        base + "/api/auth/login",
        data=json.dumps({"username": user, "password": password}).encode(),
        headers={"Content-Type": "application/json"},
    )
    with urllib.request.urlopen(req, timeout=15) as resp:
        return json.loads(resp.read())["token"]


def invoke(base, token, tool, args):
    req = urllib.request.Request(
        base + "/api/gateway/invoke",
        data=json.dumps({"tool_name": tool, "arguments": args}).encode(),
        headers={"Content-Type": "application/json", "Authorization": "Bearer " + token},
    )
    t0 = time.perf_counter()
    try:
        with urllib.request.urlopen(req, timeout=15) as resp:
            latency_ms = (time.perf_counter() - t0) * 1000
            return resp.status, latency_ms
    except urllib.error.HTTPError as e:
        return e.code, (time.perf_counter() - t0) * 1000
    except Exception:
        return -1, (time.perf_counter() - t0) * 1000


def main():
    p = argparse.ArgumentParser()
    p.add_argument("--base", default="http://localhost:8080")
    p.add_argument("--user", default="admin")
    p.add_argument("--password", default="admin123")
    p.add_argument("--tool", default="weather_current")
    p.add_argument("--args", default='{"city":"东莞"}')
    p.add_argument("--rps", type=int, default=50)
    p.add_argument("--seconds", type=int, default=5)
    args = p.parse_args()

    token = login(args.base, args.user, args.password)
    payload = json.loads(args.args)
    total = args.rps * args.seconds
    lat = []
    ok = 0
    start = time.perf_counter()
    lock = threading.Lock()

    def worker():
        nonlocal ok
        while time.perf_counter() - start < args.seconds:
            code, ms = invoke(args.base, token, args.tool, payload)
            with lock:
                lat.append(ms)
                if code == 200:
                    ok += 1

    workers = [threading.Thread(target=worker) for _ in range(min(max(args.rps, 1), 200))]
    for w in workers:
        w.start()
    for w in workers:
        w.join()

    elapsed = time.perf_counter() - start
    lat.sort()

    def pct(n):
        if not lat:
            return 0.0
        return lat[min(len(lat) - 1, int(len(lat) * n))]

    print(f"目标 RPS    : {args.rps}")
    print(f"实际调用数  : {len(lat)}")
    print(f"成功率      : {ok}/{len(lat)} = {ok/len(lat)*100:.1f}%")
    print(f"吞吐量(RPS) : {len(lat)/elapsed:.1f}")
    print(f"P50(ms)     : {pct(0.50):.2f}")
    print(f"P95(ms)     : {pct(0.95):.2f}")
    print(f"P99(ms)     : {pct(0.99):.2f}")


if __name__ == "__main__":
    main()
