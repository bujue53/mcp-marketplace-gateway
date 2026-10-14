import { useState } from 'react';

const CATEGORIES = ['', '数据库', '办公', '研发', '营销', 'weather', 'general'];

export function SearchBar({
  onSearch,
}: {
  onSearch: (q: string, category: string, sort: string) => void;
}) {
  const [q, setQ] = useState('');
  const [cat, setCat] = useState('');
  const [sort, setSort] = useState('popular');

  return (
    <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
      <div className="relative flex-1">
        <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-slate-500">🔍</span>
        <input
          value={q}
          onChange={(e) => setQ(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && onSearch(q, cat, sort)}
          placeholder="搜索工具，例如 weather、用户、订单…"
          className="w-full rounded-xl border border-slate-700 bg-slate-900 py-2.5 pl-10 pr-4 text-sm text-slate-100 outline-none focus:border-brand-500"
        />
      </div>
      <select
        value={cat}
        onChange={(e) => {
          setCat(e.target.value);
          onSearch(q, e.target.value, sort);
        }}
        className="rounded-xl border border-slate-700 bg-slate-900 px-3 py-2.5 text-sm text-slate-100 outline-none focus:border-brand-500"
      >
        {CATEGORIES.map((c) => (
          <option key={c} value={c}>
            {c === '' ? '全部分类' : c}
          </option>
        ))}
      </select>
      <select
        value={sort}
        onChange={(e) => {
          setSort(e.target.value);
          onSearch(q, cat, e.target.value);
        }}
        aria-label="工具排序方式"
        className="rounded-xl border border-slate-700 bg-slate-900 px-3 py-2.5 text-sm text-slate-100 outline-none focus:border-brand-500"
      >
        <option value="popular">调用最多</option>
        <option value="default">默认顺序</option>
      </select>
      <button
        onClick={() => onSearch(q, cat, sort)}
        className="rounded-xl bg-brand-600 px-5 py-2.5 text-sm font-medium text-white hover:bg-brand-700"
      >
        搜索
      </button>
    </div>
  );
}
