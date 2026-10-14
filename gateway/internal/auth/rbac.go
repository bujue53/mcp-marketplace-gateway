package auth

import (
	"context"
	"strconv"

	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/model"
	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/store"
)

// rbac 实现「角色默认权限 + 细粒度权限覆盖」的授权判定。
// 角色默认值：
//   - admin     -> 全部
//   - developer -> invoke + read + manage
//   - viewer    -> read
type rbac struct {
	st *store.Store
}

func NewRBAC(st *store.Store) *rbac { return &rbac{st: st} }

// Can 判定 subject（user 的角色）对某个资源执行 action 是否被允许。
// subjectType = role | user；resourceType = server | tool。
func (r *rbac) Can(ctx context.Context, user *model.User, subjectType, resourceType string, resourceID int64, action string) bool {
	// 1) 先查细粒度覆盖（用户 / 角色维度）
	id := user.Role
	if subjectType == "user" {
		id = strconv.FormatInt(user.ID, 10)
	}
	override, found, err := r.st.EffectivePermission(ctx, subjectType, id, user.Role, resourceType, resourceID, action)
	if err != nil {
		return false
	}
	if found {
		return override
	}
	// 2) 回退到角色默认策略
	return defaultRoleAllows(user.Role, action)
}

func defaultRoleAllows(role, action string) bool {
	switch role {
	case model.RoleAdmin:
		return true
	case model.RoleDeveloper:
		return action == model.ActionInvoke || action == model.ActionRead || action == model.ActionManage
	default: // viewer
		return action == model.ActionRead
	}
}
