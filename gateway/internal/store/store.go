package store

import (
	"context"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yafeng/mcp-marketplace/gateway/internal/model"
)

// ErrNotFound 表示查询无结果。
var ErrNotFound = errors.New("not found")

// Store 是聚合的数据访问层，集中所有 SQL。
type Store struct {
	P *pgxpool.Pool
}

func New(p *pgxpool.Pool) *Store { return &Store{P: p} }

// ---- users ----

func (s *Store) CreateUser(ctx context.Context, u *model.User) error {
	return s.P.QueryRow(ctx,
		`INSERT INTO users(username,email,password_hash,role)
		 VALUES($1,$2,$3,$4) RETURNING id, created_at`,
		u.Username, u.Email, u.PasswordHash, u.Role,
	).Scan(&u.ID, &u.CreatedAt)
}

func (s *Store) FindUserByUsername(ctx context.Context, username string) (*model.User, error) {
	u := &model.User{}
	err := s.P.QueryRow(ctx,
		`SELECT id,username,email,password_hash,role,created_at
		 FROM users WHERE username=$1`, username,
	).Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

func (s *Store) FindUserByID(ctx context.Context, id int64) (*model.User, error) {
	u := &model.User{}
	err := s.P.QueryRow(ctx,
		`SELECT id,username,email,password_hash,role,created_at
		 FROM users WHERE id=$1`, id,
	).Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

// ---- mcp servers ----

func (s *Store) UpsertServer(ctx context.Context, sv *model.MCPServer) error {
	return s.P.QueryRow(ctx,
		`INSERT INTO mcp_servers(name,description,version,endpoint_url,transport,auth_type,status,owner_id,tags)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
		 ON CONFLICT(name) DO UPDATE
		   SET description=EXCLUDED.description, version=EXCLUDED.version,
		       endpoint_url=EXCLUDED.endpoint_url, transport=EXCLUDED.transport,
		       auth_type=EXCLUDED.auth_type, status=EXCLUDED.status,
		       owner_id=EXCLUDED.owner_id, tags=EXCLUDED.tags, updated_at=now()
		 RETURNING id, created_at`,
		sv.Name, sv.Description, sv.Version, sv.EndpointURL, sv.Transport,
		sv.AuthType, sv.Status, sv.OwnerID, sv.Tags,
	).Scan(&sv.ID, &sv.CreatedAt)
}

func (s *Store) GetServerByID(ctx context.Context, id int64) (*model.MCPServer, error) {
	sv := &model.MCPServer{}
	err := s.P.QueryRow(ctx,
		`SELECT id,name,description,version,endpoint_url,transport,auth_type,status,owner_id,tags,created_at,updated_at
		 FROM mcp_servers WHERE id=$1`, id,
	).Scan(&sv.ID, &sv.Name, &sv.Description, &sv.Version, &sv.EndpointURL, &sv.Transport,
		&sv.AuthType, &sv.Status, &sv.OwnerID, &sv.Tags, &sv.CreatedAt, &sv.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return sv, err
}

func (s *Store) ListServers(ctx context.Context, status string) ([]*model.MCPServer, error) {
	q := `SELECT id,name,description,version,endpoint_url,transport,auth_type,status,owner_id,tags,created_at,updated_at
	      FROM mcp_servers`
	args := []any{}
	if status != "" {
		q += ` WHERE status=$1`
		args = append(args, status)
	}
	q += ` ORDER BY id`
	rows, err := s.P.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.MCPServer
	for rows.Next() {
		sv := &model.MCPServer{}
		if err := rows.Scan(&sv.ID, &sv.Name, &sv.Description, &sv.Version, &sv.EndpointURL,
			&sv.Transport, &sv.AuthType, &sv.Status, &sv.OwnerID, &sv.Tags,
			&sv.CreatedAt, &sv.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, sv)
	}
	return out, nil
}

func (s *Store) DeleteServer(ctx context.Context, id int64) error {
	return s.P.QueryRow(ctx, `DELETE FROM mcp_servers WHERE id=$1 RETURNING id`, id).Scan(new(int64))
}

// ---- tools ----

func (s *Store) UpsertTool(ctx context.Context, t *model.MCPTool) error {
	err := s.P.QueryRow(ctx,
		`INSERT INTO mcp_tools(server_id,name,description,category,tags,input_schema,output_schema,cost_model,is_generated,version)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		 ON CONFLICT(server_id, name) DO UPDATE
		   SET description=EXCLUDED.description, category=EXCLUDED.category,
		       tags=EXCLUDED.tags, input_schema=EXCLUDED.input_schema,
		       output_schema=EXCLUDED.output_schema, cost_model=EXCLUDED.cost_model,
		       is_generated=EXCLUDED.is_generated, version=EXCLUDED.version
		 RETURNING id, created_at`,
		t.ServerID, t.Name, t.Description, t.Category, t.Tags, t.InputSchema,
		t.OutputSchema, t.CostModel, t.IsGenerated, t.Version,
	).Scan(&t.ID, &t.CreatedAt)
	if err != nil {
		return err
	}
	// 写版本快照
	_, err = s.P.Exec(ctx,
		`INSERT INTO tool_versions(tool_id,version,spec) VALUES($1,$2,$3)`,
		t.ID, t.Version, model.JSON{"name": t.Name, "input_schema": t.InputSchema, "output_schema": t.OutputSchema})
	return err
}

func (s *Store) GetToolByID(ctx context.Context, id int64) (*model.MCPTool, error) {
	return s.queryTool(ctx,
		`SELECT t.id,t.server_id,t.name,t.description,t.category,t.tags,
		        t.input_schema,t.output_schema,t.cost_model,t.is_generated,t.version,t.created_at,
		        COALESCE(AVG(r.rating),0)::float8, COUNT(DISTINCT r.id),
		        (SELECT COUNT(*) FROM tool_calls c WHERE c.tool_id=t.id)
		 FROM mcp_tools t
		 LEFT JOIN tool_ratings r ON r.tool_id=t.id
		 WHERE t.id=$1 GROUP BY t.id`, id)
}

func (s *Store) GetToolByName(ctx context.Context, serverID int64, name string) (*model.MCPTool, error) {
	return s.queryTool(ctx,
		`SELECT t.id,t.server_id,t.name,t.description,t.category,t.tags,
		        t.input_schema,t.output_schema,t.cost_model,t.is_generated,t.version,t.created_at,
		        COALESCE(AVG(r.rating),0)::float8, COUNT(DISTINCT r.id),
		        (SELECT COUNT(*) FROM tool_calls c WHERE c.tool_id=t.id)
		 FROM mcp_tools t
		 LEFT JOIN tool_ratings r ON r.tool_id=t.id
		 WHERE t.server_id=$1 AND t.name=$2 GROUP BY t.id`, serverID, name)
}

func (s *Store) FindToolByName(ctx context.Context, name string) (*model.MCPTool, error) {
	return s.queryTool(ctx,
		`SELECT t.id,t.server_id,t.name,t.description,t.category,t.tags,
		        t.input_schema,t.output_schema,t.cost_model,t.is_generated,t.version,t.created_at,
		        COALESCE(AVG(r.rating),0)::float8, COUNT(DISTINCT r.id),
		        (SELECT COUNT(*) FROM tool_calls c WHERE c.tool_id=t.id)
		 FROM mcp_tools t
		 LEFT JOIN tool_ratings r ON r.tool_id=t.id
		 WHERE t.name=$1 GROUP BY t.id ORDER BY t.id LIMIT 1`, name)
}

func (s *Store) ListTools(ctx context.Context, q, category string, limit int) ([]*model.MCPTool, error) {
	sql := `SELECT t.id,t.server_id,t.name,t.description,t.category,t.tags,
	               t.input_schema,t.output_schema,t.cost_model,t.is_generated,t.version,t.created_at,
	               COALESCE(AVG(r.rating),0)::float8, COUNT(DISTINCT r.id),
	               (SELECT COUNT(*) FROM tool_calls c WHERE c.tool_id=t.id)
	        FROM mcp_tools t
	        LEFT JOIN tool_ratings r ON r.tool_id=t.id`
	args := []any{}
	where := []string{}
	if q != "" {
		args = append(args, "%"+q+"%")
		where = append(where, `(t.name ILIKE $`+itoa(len(args))+` OR t.description ILIKE $`+itoa(len(args))+`)`)
	}
	if category != "" {
		args = append(args, category)
		where = append(where, `t.category=$`+itoa(len(args)))
	}
	if len(where) > 0 {
		sql += ` WHERE ` + joinAnd(where)
	}
	sql += ` GROUP BY t.id ORDER BY t.id LIMIT $` + itoa(len(args)+1)
	args = append(args, limit)

	rows, err := s.P.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.MCPTool
	for rows.Next() {
		t := &model.MCPTool{}
		if err := rows.Scan(&t.ID, &t.ServerID, &t.Name, &t.Description, &t.Category,
			&t.Tags, &t.InputSchema, &t.OutputSchema, &t.CostModel, &t.IsGenerated,
			&t.Version, &t.CreatedAt, &t.Rating, &t.RatingCount, &t.CallCount); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

func (s *Store) queryTool(ctx context.Context, sql string, args ...any) (*model.MCPTool, error) {
	t := &model.MCPTool{}
	err := s.P.QueryRow(ctx, sql, args...).Scan(
		&t.ID, &t.ServerID, &t.Name, &t.Description, &t.Category, &t.Tags,
		&t.InputSchema, &t.OutputSchema, &t.CostModel, &t.IsGenerated, &t.Version,
		&t.CreatedAt, &t.Rating, &t.RatingCount, &t.CallCount,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return t, err
}

// ---- ratings ----

func (s *Store) RateTool(ctx context.Context, toolID, userID int64, rating int) error {
	_, err := s.P.Exec(ctx,
		`INSERT INTO tool_ratings(tool_id,user_id,rating) VALUES($1,$2,$3)
		 ON CONFLICT(tool_id,user_id) DO UPDATE SET rating=EXCLUDED.rating, created_at=now()`,
		toolID, userID, rating)
	return err
}

// ---- calls & audit ----

func (s *Store) InsertToolCall(ctx context.Context, c *model.ToolCall) error {
	return s.P.QueryRow(ctx,
		`INSERT INTO tool_calls(trace_id,user_id,tool_id,status_code,latency_ms,tokens_in,tokens_out,cost_micros)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id, created_at`,
		c.TraceID, c.UserID, c.ToolID, c.StatusCode, c.LatencyMs,
		c.TokensIn, c.TokensOut, c.CostMicros,
	).Scan(&c.ID, &c.CreatedAt)
}

func (s *Store) InsertAuditLog(ctx context.Context, a *model.AuditLog) error {
	_, err := s.P.Exec(ctx,
		`INSERT INTO audit_logs(trace_id,user_id,action,resource,resource_id,detail,ip,user_agent)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
		a.TraceID, a.UserID, a.Action, a.Resource, a.ResourceID, a.Detail, a.IP, a.UserAgent)
	return err
}

// ---- stats ----

// DailyCalls 返回最近 N 天每天的调用次数（用于前端折线图）。
func (s *Store) DailyCalls(ctx context.Context, days int) (map[string]int64, error) {
	rows, err := s.P.Query(ctx,
		`SELECT to_char(created_at,'YYYY-MM-DD') d, COUNT(*) FROM tool_calls
		 WHERE created_at >= now() - make_interval(days => $1)
		 GROUP BY d ORDER BY d`, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var d string
		var c int64
		if err := rows.Scan(&d, &c); err != nil {
			return nil, err
		}
		out[d] = c
	}
	return out, nil
}

func (s *Store) TopTools(ctx context.Context, limit int) ([]*model.MCPTool, error) {
	return s.ListTools(ctx, "", "", limit)
}

// StatsSummary 平台汇总：服务数、工具数、调用数、累计成本。
func (s *Store) StatsSummary(ctx context.Context) (map[string]any, error) {
	out := map[string]any{}
	err := s.P.QueryRow(ctx, `
		SELECT (SELECT COUNT(*) FROM mcp_servers),
		       (SELECT COUNT(*) FROM mcp_tools),
		       (SELECT COUNT(*) FROM tool_calls),
		       (SELECT COALESCE(SUM(cost_micros),0) FROM tool_calls)
	`).Scan(&out["servers"], &out["tools"], &out["calls"], &out["cost_micros"])
	return out, err
}

// TopToolsByCalls 按调用次数排序的前 N 个工具。
func (s *Store) TopToolsByCalls(ctx context.Context, limit int) ([]*model.MCPTool, error) {
	rows, err := s.P.Query(ctx,
		`SELECT t.id,t.server_id,t.name,t.description,t.category,t.tags,
		        t.input_schema,t.output_schema,t.cost_model,t.is_generated,t.version,t.created_at,
		        COALESCE(AVG(r.rating),0)::float8, COUNT(DISTINCT r.id),
		        (SELECT COUNT(*) FROM tool_calls c WHERE c.tool_id=t.id) AS calls
		 FROM mcp_tools t
		 LEFT JOIN tool_ratings r ON r.tool_id=t.id
		 GROUP BY t.id
		 ORDER BY calls DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.MCPTool
	for rows.Next() {
		t := &model.MCPTool{}
		if err := rows.Scan(&t.ID, &t.ServerID, &t.Name, &t.Description, &t.Category,
			&t.Tags, &t.InputSchema, &t.OutputSchema, &t.CostModel, &t.IsGenerated,
			&t.Version, &t.CreatedAt, &t.Rating, &t.RatingCount, &t.CallCount); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// ---- permissions ----

func (s *Store) UpsertPermission(ctx context.Context, subjectType, subjectID, resourceType string, resourceID int64, action string, allowed bool) error {
	_, err := s.P.Exec(ctx,
		`INSERT INTO permissions(subject_type,subject_id,resource_type,resource_id,action,allowed)
		 VALUES($1,$2,$3,$4,$5,$6)
		 ON CONFLICT(subject_type,subject_id,resource_type,resource_id,action)
		 DO UPDATE SET allowed=EXCLUDED.allowed`,
		subjectType, subjectID, resourceType, resourceID, action, allowed)
	return err
}

func (s *Store) PermissionAllowed(ctx context.Context, subjectType, subjectID string, resourceType string, resourceID int64, action string) (bool, bool, error) {
	var allowed bool
	err := s.P.QueryRow(ctx,
		`SELECT allowed FROM permissions
		 WHERE subject_type=$1 AND subject_id=$2 AND resource_type=$3 AND resource_id=$4 AND action=$5`,
		subjectType, subjectID, resourceType, resourceID, action,
	).Scan(&allowed)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, nil
	}
	return allowed, true, err
}

func itoa(n int) string { return strconv.Itoa(n) }

func joinAnd(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " AND "
		}
		out += p
	}
	return out
}
