package auth

import (
	"context"

	"github.com/yafeng/mcp-marketplace/gateway/internal/model"
)

type ctxKey string

const userKey ctxKey = "mcp.user"

// WithUser 将当前用户挂到 context。
func WithUser(ctx context.Context, u *model.User) context.Context {
	return context.WithValue(ctx, userKey, u)
}

// UserFrom 从 context 取出当前用户。
func UserFrom(ctx context.Context) *model.User {
	if u, ok := ctx.Value(userKey).(*model.User); ok {
		return u
	}
	return nil
}

