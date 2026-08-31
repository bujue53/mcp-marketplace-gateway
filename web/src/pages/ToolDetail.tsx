import { useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';
import { api } from '../api/client';
import type { MCPTool } from '../api/types';
import { StarRating } from '../components/StarRating';
import { useAuth } from '../context/AuthContext';

export function ToolDetail() {
  const { id } = useParams();
  const { token } = useAuth();
  const [tool, setTool] = useState<MCPTool | null>(null);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');

  const [args, setArgs] = useState('{\n  "city": "东莞"\n}');
  const [result, setResult] = useState('');
  const [running, setRunning] = useState(false);

  useEffect(() => {
    api
      .getTool(Number(id))
      .then(setTool)
      .catch((e) => setError((e as Error).message));
  }, [id]);

  const rate = async (rating: number) => {
    if (!token || !tool) return;
    try {
      await api.rateTool(tool.id, rating, token);
      setNotice(`已评 ${rating} 星`);
      const updated = await api.getTool(tool.id);
      setTool(updated);
    } catch (e) {
      setNotice(`评分失败：${(e as Error).message}`);
    }
  };

  const invoke = async () => {
    if (!token || !tool) return;
    setRunning(true);
    setResult('');
    try {
      const payload = JSON.parse(args || '{}');
      const res = await api.invoke(token, tool.name, payload);
      const text = res.result?.content?.map((c) => c.text).join('\n') ?? '';
      setResult(`trace=${res.trace_id} cost=${res.cost_micros}us latency=${res.latency_ms}ms\n\n${text}`);
    } catch (e) {
      setResult(`调用失败：${(e as Error).message}`);
    } finally {
      setRunning(false);
    }
  };

  if (error) return <div className="text-red-400">{error}</div>;
  if (!tool) return <div className="text-slate-400">加载中…</div>;

  return (
    <div className="grid grid-cols-1 gap-8 lg:grid-cols-2">
      <div>
        <div className="flex items-center gap-2">
          <h2 className="text-2xl font-bold text-white">{tool.name}</h2>
          {tool.is_generated && (
            <span className="rounded-full bg-brand-500/15 px-2 py-0.5 text-xs text-brand-500">OpenAPI 生成</span>
          )}
        </div>
        <p className="mt-3 leading-relaxed text-slate-300">{tool.description}</p>

        <div className="mt-4 flex items-center gap-3">
          <StarRating value={tool.rating} count={tool.rating_count} onRate={token ? rate : undefined} />
          <span className="text-xs text-slate-500">调用 {tool.call_count} 次 · v{tool.version}</span>
        </div>

        <div className="mt-6 rounded-2xl border border-slate-800 bg-slate-900 p-5">
          <h3 className="text-sm font-semibold text-slate-200">输入参数（inputSchema）</h3>
          <pre className="mt-2 max-h-64 overflow-auto rounded-lg bg-slate-950 p-3 text-xs text-slate-300">
            {JSON.stringify(tool.input_schema, null, 2)}
          </pre>
        </div>

        <div className="mt-4 rounded-2xl border border-slate-800 bg-slate-900 p-5">
          <h3 className="text-sm font-semibold text-slate-200">输出（outputSchema）</h3>
          <pre className="mt-2 max-h-64 overflow-auto rounded-lg bg-slate-950 p-3 text-xs text-slate-300">
            {JSON.stringify(tool.output_schema, null, 2)}
          </pre>
        </div>
      </div>

      <div>
        <div className="rounded-2xl border border-slate-800 bg-slate-900 p-5">
          <h3 className="text-sm font-semibold text-slate-200">通过网关试调用</h3>
          <p className="mt-1 text-xs text-slate-500">携带 JWT，经认证 → 授权 → 限流 → 熔断 → 成本计量 → 审计</p>
          {!token ? (
            <div className="mt-4 text-sm text-amber-400">请先登录后再试调用。</div>
          ) : (
            <>
              <textarea
                value={args}
                onChange={(e) => setArgs(e.target.value)}
                rows={6}
                className="mt-3 w-full rounded-xl border border-slate-700 bg-slate-950 p-3 font-mono text-xs outline-none focus:border-brand-500"
              />
              <button
                onClick={invoke}
                disabled={running}
                className="mt-3 w-full rounded-xl bg-brand-600 py-2.5 text-sm font-medium text-white hover:bg-brand-700 disabled:opacity-50"
              >
                {running ? '调用中…' : '调用工具'}
              </button>
            </>
          )}
        </div>

        {notice && <div className="mt-4 rounded-xl bg-slate-800 px-4 py-3 text-sm text-slate-300">{notice}</div>}
        {result && (
          <pre className="mt-4 max-h-80 overflow-auto rounded-2xl border border-slate-800 bg-slate-950 p-4 text-xs text-slate-300">
            {result}
          </pre>
        )}
      </div>
    </div>
  );
}

