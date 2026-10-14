import { useEffect, useState } from 'react';
import { api } from '../api/client';
import type { MCPTool } from '../api/types';
import { SearchBar } from '../components/SearchBar';
import { ToolCard } from '../components/ToolCard';
import { useAuth } from '../context/AuthContext';

export function Marketplace() {
  const { token } = useAuth();
  const [tools, setTools] = useState<MCPTool[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');

  const load = async (q = '', category = '', sort = 'popular') => {
    setLoading(true);
    setError('');
    try {
      const res = await api.listTools(q, category, 60, sort);
      setTools(res.items);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    load();
  }, []);

  const connect = async (tool: MCPTool) => {
    if (!token) {
      setNotice('请先登录后再接入工具');
      return;
    }
    try {
      const res = await api.connectTool(tool.id, token);
      setNotice(`已接入：${res.tool}，可经 ${res.endpoint} 调用（权限已授予）`);
    } catch (err) {
      setNotice(`接入失败：${(err as Error).message}`);
    }
  };

  return (
    <div>
      <div className="mb-8">
        <h2 className="text-2xl font-bold text-white">工具市场</h2>
        <p className="mt-1 text-sm text-slate-400">浏览、搜索、评分并一键接入企业 MCP 工具</p>
      </div>

      <SearchBar onSearch={(q, cat, sort) => load(q, cat, sort)} />

      {notice && (
        <div className="mt-4 rounded-xl border border-brand-500/30 bg-brand-500/10 px-4 py-3 text-sm text-brand-500">
          {notice}
        </div>
      )}
      {error && <div className="mt-4 text-sm text-red-400">加载失败：{error}</div>}

      {loading ? (
        <div className="mt-10 text-center text-slate-400">加载中…</div>
      ) : tools.length === 0 ? (
        <div className="mt-10 text-center text-slate-500">没有匹配的工具，请尝试其他关键词。</div>
      ) : (
        <div className="mt-8 grid grid-cols-1 gap-5 sm:grid-cols-2 lg:grid-cols-3">
          {tools.map((tool) => (
            <ToolCard key={tool.id} tool={tool} onConnect={connect} />
          ))}
        </div>
      )}
    </div>
  );
}

