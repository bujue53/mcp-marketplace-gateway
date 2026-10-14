import type { AuthResponse, MCPTool, ToolCallResult, ToolVersion, User } from './types';

const BASE = __API_BASE__;

async function request<T>(path: string, options: RequestInit = {}, token?: string): Promise<T> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(options.headers as Record<string, string>),
  };
  if (token) headers.Authorization = `Bearer ${token}`;
  const res = await fetch(`${BASE}${path}`, { ...options, headers });
  if (!res.ok) {
    const body = await res.text().catch(() => '');
    throw new Error(`[${res.status}] ${body}`);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

export const api = {
  login(username: string, password: string) {
    return request<AuthResponse>('/api/auth/login', { method: 'POST', body: JSON.stringify({ username, password }) });
  },

  listTools(q = '', category = '', limit = 50, sort = 'popular') {
    const params = new URLSearchParams({ limit: String(limit) });
    if (q) params.set('q', q);
    if (category) params.set('category', category);
    if (sort) params.set('sort', sort);
    return request<{ items: MCPTool[] }>(`/api/marketplace/tools?${params.toString()}`);
  },

  getTool(id: number) {
    return request<MCPTool>(`/api/tools/${id}`);
  },

  toolVersions(id: number, limit = 20) {
    return request<{ items: ToolVersion[] }>(`/api/tools/${id}/versions?limit=${limit}`);
  },

  rateTool(id: number, rating: number, comment: string, token: string) {
    return request<{ ok: boolean }>(`/api/tools/${id}/rate`, { method: 'POST', body: JSON.stringify({ rating, comment }) }, token);
  },

  toolComments(id: number) {
    return request<{ items: { rating: number; comment: string; username: string }[] }>(`/api/tools/${id}/comments`);
  },

  connectTool(id: number, token: string) {
    return request<{ connected: boolean; tool: string; endpoint: string; access: string; config: Record<string, unknown> }>(
      `/api/tools/${id}/connect`,
      { method: 'POST' },
      token,
    );
  },

  invoke(token: string, toolName: string, args: Record<string, unknown>) {
    return request<{ trace_id: string; tool: string; cost_micros: number; latency_ms: number; result: ToolCallResult }>(
      '/api/gateway/invoke',
      { method: 'POST', body: JSON.stringify({ tool_name: toolName, arguments: args }) },
      token,
    );
  },

  statsSummary(token: string) {
    return request<{ servers: number; tools: number; calls: number; cost_micros: number }>('/api/stats/summary', {}, token);
  },

  statsDaily(token: string, days = 7) {
    return request<{ daily: Record<string, number> }>(`/api/stats/daily?days=${days}`, {}, token);
  },

  statsTop(token: string, limit = 10) {
    return request<{ items: MCPTool[] }>(`/api/stats/top?limit=${limit}`, {}, token);
  },

  auditAlerts(token: string, limit = 20) {
    return request<{ items: { id: number; severity: string; rule: string; tool_id: number; detail: object; created_at: string }[] }>(
      `/api/audit/alerts?limit=${limit}`,
      {},
      token,
    );
  },

  auditReport(token: string) {
    return request<{ summary: Record<string, number>; sensitive_hot: { tool_id: number; name: string; calls: number }[] }>(
      '/api/audit/report',
      {},
      token,
    );
  },
};

export function me(): User | null {
  try {
    const raw = localStorage.getItem('mcp.user');
    return raw ? (JSON.parse(raw) as User) : null;
  } catch {
    return null;
  }
}
