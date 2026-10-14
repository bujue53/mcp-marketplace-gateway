import { useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';
import { api } from '../api/client';
import type { MCPTool, ToolVersion } from '../api/types';
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
  const [comment, setComment] = useState('');
  const [config, setConfig] = useState('');
  const [comments, setComments] = useState<{ rating: number; comment: string; username: string }[]>([]);
  const [versions, setVersions] = useState<ToolVersion[]>([]);

  useEffect(() => {
    api
      .getTool(Number(id))
      .then(setTool)
      .catch((e) => setError((e as Error).message));
    api.toolComments(Number(id)).then((r) => setComments(r.items)).catch(() => setComments([]));
    api.toolVersions(Number(id)).then((r) => setVersions(r.items)).catch(() => setVersions([]));
  }, [id]);

  const rate = async (rating: number) => {
    if (!token || !tool) return;
    try {
      await api.rateTool(tool.id, rating, comment, token);
      setNotice(`已评 ${rating} 星${comment ? '，感谢评论' : ''}`);
      const updated = await api.getTool(tool.id);
      setTool(updated);
      const r = await api.toolComments(tool.id);
      setComments(r.items);
      setComment('');
    } catch (e) {
      setNotice(`评分失败：${(e as Error).message}`);
    }
  };

  const connect = async () => {
    if (!token || !tool) return;
    try {
      const res = await api.connectTool(tool.id, token);
      setConfig(JSON.stringify(res.config, null, 2));
      setNotice(`已接入 ${res.tool}，权限已授予`);
    } catch (e) {
      setNotice(`接入失败：${(e as Error).message}`);
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

        {token && (
          <div className="mt-4 flex flex-col gap-2 rounded-xl border border-slate-800 bg-slate-900 p-4">
            <div className="flex items-center gap-2">
              <span className="text-xs text-slate-400">补充评论</span>
              <input
                value={comment}
                onChange={(e) => setComment(e.target.value)}
                placeholder="一句话评价（可选）"
                className="flex-1 rounded-lg border border-slate-700 bg-slate-950 px-3 py-1.5 text-xs outline-none focus:border-brand-500"
              />
            </div>
            <button onClick={() => rate(Math.round(tool.rating) || 5)} className="w-fit rounded-lg bg-slate-800 px-3 py-1.5 text-xs text-slate-200 hover:bg-slate-700">
              提交评分/评论
            </button>
          </div>
        )}

        {comments.length > 0 && (
          <div className="mt-4 rounded-2xl border border-slate-800 bg-slate-900 p-4">
            <h3 className="text-sm font-semibold text-slate-200">用户评论</h3>
            <div className="mt-2 space-y-2">
              {comments.map((c, i) => (
                <div key={i} className="rounded-lg bg-slate-950 px-3 py-2 text-xs">
                  <span className="text-amber-400">{'★'.repeat(c.rating)}</span>
                  <span className="ml-2 text-slate-300">{c.comment}</span>
                  <span className="ml-2 text-slate-500">— {c.username}</span>
                </div>
              ))}
            </div>
          </div>
        )}

        <div className="mt-4 rounded-2xl border border-slate-800 bg-slate-900 p-4">
          <div className="flex items-center justify-between">
            <h3 className="text-sm font-semibold text-slate-200">版本快照</h3>
            <span className="text-xs text-slate-500">最近 {versions.length} 条</span>
          </div>
          {versions.length === 0 ? (
            <p className="mt-2 text-xs text-slate-500">暂无版本快照</p>
          ) : (
            <div className="mt-3 space-y-2">
              {versions.map((version, index) => (
                <details key={version.id} className="rounded-lg bg-slate-950 px-3 py-2">
                  <summary className="cursor-pointer text-xs text-slate-300">
                    <span className="font-medium">v{version.version}</span>
                    {index === 0 && <span className="ml-2 text-brand-400">最近快照</span>}
                    <span className="ml-2 text-slate-500">{new Date(version.created_at).toLocaleString()}</span>
                  </summary>
                  <pre className="mt-2 max-h-48 overflow-auto rounded bg-slate-900 p-2 text-[11px] text-slate-400">
                    {JSON.stringify(version.spec, null, 2)}
                  </pre>
                </details>
              ))}
            </div>
          )}
        </div>

        <div className="mt-4 rounded-2xl border border-slate-800 bg-slate-900 p-4">
          <h3 className="text-sm font-semibold text-slate-200">接入配置</h3>
          <button onClick={connect} className="mt-2 rounded-lg bg-brand-600 px-3 py-1.5 text-xs text-white hover:bg-brand-700">
            一键接入并生成配置
          </button>
          {config && (
            <pre className="mt-3 overflow-auto rounded-lg bg-slate-950 p-3 text-[11px] text-slate-300">{config}</pre>
          )}
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
