package store

import (
	"context"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/model"
)

// 调用热路径读取工具配置，避免每次调用扫描历史调用数与评分。
func (s *Store) InvocationTool(ctx context.Context, id int64, name string) (*model.MCPTool, error) {
	return s.tools.get(ctx, itoa64(id)+":"+name, func(ctx context.Context) (*model.MCPTool, error) {
		return s.queryTool(ctx, `SELECT id,server_id,name,description,category,tags,input_schema,output_schema,cost_model,is_generated,sensitive,version,created_at,0::float8,0::bigint,0::bigint FROM mcp_tools WHERE ($1>0 AND id=$1) OR ($1=0 AND name=$2) ORDER BY id LIMIT 1`, id, name)
	})
}

func itoa64(value int64) string { return strconv.FormatInt(value, 10) }
func (s *Store) EffectivePermission(ctx context.Context, typ, subject, role, resource string, id int64, action string) (bool, bool, error) {
	var allowed bool
	err := s.P.QueryRow(ctx, `SELECT allowed FROM permissions WHERE resource_type=$1 AND resource_id=$2 AND action=$3 AND ((subject_type=$4 AND subject_id=$5) OR (subject_type='role' AND subject_id=$6)) ORDER BY CASE WHEN subject_type=$4 AND subject_id=$5 THEN 0 ELSE 1 END LIMIT 1`, resource, id, action, typ, subject, role).Scan(&allowed)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, nil
	}
	return allowed, err == nil, err
}
