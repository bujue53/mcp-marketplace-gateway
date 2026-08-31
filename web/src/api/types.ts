export interface User {
  id: number;
  username: string;
  email: string;
  role: string;
}

export interface MCPServer {
  id: number;
  name: string;
  description: string;
  version: string;
  endpoint_url: string;
  transport: string;
  auth_type: string;
  status: string;
  owner_id: number;
  tags: string[];
  created_at: string;
}

export interface MCPTool {
  id: number;
  server_id: number;
  name: string;
  description: string;
  category: string;
  tags: string[];
  input_schema: Record<string, unknown>;
  output_schema: Record<string, unknown>;
  cost_model: Record<string, unknown>;
  is_generated: boolean;
  version: string;
  rating: number;
  rating_count: number;
  call_count: number;
  created_at?: string;
}

export interface ContentBlock {
  type: string;
  text: string;
}

export interface ToolCallResult {
  content: ContentBlock[];
  isError?: boolean;
}

export interface AuthResponse {
  token: string;
  expires_at: string;
  user: User;
}

