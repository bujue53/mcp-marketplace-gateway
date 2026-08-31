import { Link } from 'react-router-dom';
import type { MCPTool } from '../api/types';
import { StarRating } from './StarRating';

export function ToolCard({
  tool,
  onConnect,
}: {
  tool: MCPTool;
  onConnect?: (tool: MCPTool) => void;
}) {
  return (
    <div className="card-hover flex flex-col justify-between rounded-2xl border border-slate-800 bg-slate-900 p-5">
      <div>
        <div className="flex items-start justify-between gap-2">
          <h3 className="text-base font-semibold text-white">{tool.name}</h3>
          {tool.is_generated && (
            <span className="rounded-full bg-brand-500/15 px-2 py-0.5 text-[11px] font-medium text-brand-500">
              OpenAPI 生成
            </span>
          )}
        </div>
        <p className="mt-2 line-clamp-2 text-sm leading-relaxed text-slate-400">{tool.description || '暂无描述'}</p>
        <div className="mt-3 flex flex-wrap gap-1.5">
          <span className="rounded-md bg-slate-800 px-2 py-0.5 text-xs text-slate-300">{tool.category}</span>
          {tool.tags.slice(0, 3).map((tag) => (
            <span key={tag} className="rounded-md bg-slate-800/60 px-2 py-0.5 text-xs text-slate-400">
              #{tag}
            </span>
          ))}
        </div>
      </div>

      <div className="mt-4 flex items-center justify-between">
        <div className="space-y-1">
          <StarRating value={tool.rating} count={tool.rating_count} />
          <div className="text-xs text-slate-500">
            调用 <span className="text-slate-300">{tool.call_count}</span> 次 · v{tool.version}
          </div>
        </div>
        <div className="flex gap-2">
          <Link
            to={`/tools/${tool.id}`}
            className="rounded-lg border border-slate-700 px-3 py-1.5 text-xs text-slate-300 hover:bg-slate-800"
          >
            详情
          </Link>
          {onConnect && (
            <button
              onClick={() => onConnect(tool)}
              className="rounded-lg bg-brand-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-brand-700"
            >
              一键接入
            </button>
          )}
        </div>
      </div>
    </div>
  );
}

