import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { api } from '../api/client';
import type { MCPTool } from '../api/types';
import { useAuth } from '../context/AuthContext';

interface Summary {
  servers: number;
  tools: number;
  calls: number;
  cost_micros: number;
}

export function Dashboard() {
  const { token } = useAuth();
  const [summary, setSummary] = useState<Summary | null>(null);
  const [daily, setDaily] = useState<Record<string, number>>({});
  const [top, setTop] = useState<MCPTool[]>([]);
  const [report, setReport] = useState<{ summary: Record<string, number>; sensitive_hot: { name: string; calls: number }[] } | null>(null);
  const [alerts, setAlerts] = useState<{ severity: string; rule: string; created_at: string }[]>([]);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!token) return;
    Promise.all([api.statsSummary(token), api.statsDaily(token, 7), api.statsTop(token, 10), api.auditReport(token), api.auditAlerts(token)])
      .then(([s, d, t, r, a]) => {
        setSummary(s);
        setDaily(d.daily);
        setTop(t.items);
        setReport(r);
        setAlerts(a.items);
      })
      .catch((e) => setError((e as Error).message));
  }, [token]);

  const dates = Object.keys(daily).sort();
  const maxCalls = Math.max(1, ...dates.map((d) => daily[d]));

  return (
    <div>
      <h2 className="text-2xl font-bold text-white">调用统计</h2>
      <p className="mt-1 text-sm text-slate-400">全链路审计与成本计量概览</p>
      {error && <div className="mt-4 text-sm text-red-400">{error}</div>}

      <div className="mt-6 grid grid-cols-2 gap-4 sm:grid-cols-4">
        <StatCard label="MCP Server" value={summary?.servers ?? '--'} />
        <StatCard label="工具数" value={summary?.tools ?? '--'} />
        <StatCard label="调用次数" value={summary?.calls ?? '--'} />
        <StatCard label="累计成本" value={formatCost(summary?.cost_micros ?? 0)} />
      </div>

      <div className="mt-8 grid grid-cols-1 gap-6 lg:grid-cols-2">
        <div className="rounded-2xl border border-slate-800 bg-slate-900 p-5">
          <h3 className="text-sm font-semibold text-slate-200">最近 7 天调用趋势</h3>
          <div className="mt-4 flex h-48 items-end gap-2">
            {dates.length === 0 ? (
              <div className="text-sm text-slate-500">暂无调用数据</div>
            ) : (
              dates.map((d) => (
                <div key={d} className="flex flex-1 flex-col items-center gap-1">
                  <div className="text-[11px] text-slate-400">{daily[d]}</div>
                  <div
                    className="w-full rounded-t-md bg-brand-600/80"
                    style={{ height: `${Math.max(6, (daily[d] / maxCalls) * 100)}%` }}
                  />
                  <div className="text-[10px] text-slate-500">{d.slice(5)}</div>
                </div>
              ))
            )}
          </div>
        </div>

        <div className="rounded-2xl border border-slate-800 bg-slate-900 p-5">
          <h3 className="text-sm font-semibold text-slate-200">调用最多工具 Top 10</h3>
          <div className="mt-4 space-y-2">
            {top.length === 0 ? (
              <div className="text-sm text-slate-500">暂无数据</div>
            ) : (
              top.map((tool) => (
                <Link
                  key={tool.id}
                  to={`/tools/${tool.id}`}
                  className="flex items-center justify-between rounded-lg bg-slate-950 px-3 py-2 text-sm hover:bg-slate-800"
                >
                  <span className="text-slate-200">{tool.name}</span>
                  <span className="text-xs text-slate-500">{tool.call_count} 次</span>
                </Link>
              ))
            )}
          </div>
        </div>
      </div>

      <div className="mt-8 grid grid-cols-1 gap-6 lg:grid-cols-2">
        <div className="rounded-2xl border border-slate-800 bg-slate-900 p-5">
          <h3 className="text-sm font-semibold text-slate-200">治理报告</h3>
          <div className="mt-3 grid grid-cols-3 gap-3">
            <MiniStat label="敏感工具" value={String(report?.summary?.sensitive_tools ?? '--')} />
            <MiniStat label="近7天告警" value={String(report?.summary?.alerts_7d ?? '--')} />
            <MiniStat label="累计成本" value={formatCost(report?.summary?.cost_micros ?? 0)} />
          </div>
          {report && report.sensitive_hot.length > 0 && (
            <div className="mt-3 text-xs text-amber-400">
              敏感工具高频调用：{report.sensitive_hot.map((h) => `${h.name}(${h.calls})`).join('、')}
            </div>
          )}
        </div>

        <div className="rounded-2xl border border-slate-800 bg-slate-900 p-5">
          <h3 className="text-sm font-semibold text-slate-200">异常调用告警</h3>
          <div className="mt-3 max-h-48 space-y-2 overflow-auto">
            {alerts.length === 0 ? (
              <div className="text-sm text-slate-500">暂无告警</div>
            ) : (
              alerts.map((a, i) => (
                <div key={i} className="rounded-lg bg-slate-950 px-3 py-2 text-xs">
                  <span className={a.severity === 'critical' ? 'text-red-400' : 'text-amber-400'}>{a.severity}</span>
                  <span className="ml-2 text-slate-300">{a.rule}</span>
                  <span className="ml-2 text-slate-500">{a.created_at}</span>
                </div>
              ))
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

function StatCard({ label, value }: { label: string; value: string | number }) {
  return (
    <div className="rounded-2xl border border-slate-800 bg-slate-900 p-5">
      <div className="text-xs text-slate-400">{label}</div>
      <div className="mt-2 text-2xl font-bold text-white">{value}</div>
    </div>
  );
}

function formatCost(micros: number) {
  const usd = micros / 1_000_000;
  return `$${usd.toFixed(4)}`;
}

function MiniStat({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg bg-slate-950 p-3">
      <div className="text-[11px] text-slate-500">{label}</div>
      <div className="mt-1 text-lg font-semibold text-slate-100">{value}</div>
    </div>
  );
}
