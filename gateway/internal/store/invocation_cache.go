package store

import (
	"context"
	"sync"
	"time"

	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/model"
)

// 只缓存身份与工具/服务元数据 500ms；权限覆盖每次从数据库读取。
// 锁合并同一时刻的刷新，避免缓存过期时并发重复查询。
type cacheEntry[T any] struct {
	value   *T
	expires time.Time
}
type invocationCache[T any] struct {
	mu      sync.Mutex
	entries map[string]cacheEntry[T]
}

func (c *invocationCache[T]) get(ctx context.Context, key string, load func(context.Context) (*T, error)) (*T, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.entries[key]; ok && time.Now().Before(entry.expires) {
		return entry.value, nil
	}
	value, err := load(ctx)
	if err != nil {
		return nil, err
	}
	if c.entries == nil || len(c.entries) >= 1024 {
		c.entries = map[string]cacheEntry[T]{}
	}
	c.entries[key] = cacheEntry[T]{value: value, expires: time.Now().Add(500 * time.Millisecond)}
	return value, nil
}
func (c *invocationCache[T]) clear() { c.mu.Lock(); c.entries = nil; c.mu.Unlock() }
func (s *Store) InvocationUser(ctx context.Context, id int64) (*model.User, error) {
	return s.users.get(ctx, itoa64(id), func(ctx context.Context) (*model.User, error) { return s.FindUserByID(ctx, id) })
}
func (s *Store) InvocationServer(ctx context.Context, id int64) (*model.MCPServer, error) {
	return s.servers.get(ctx, itoa64(id), func(ctx context.Context) (*model.MCPServer, error) { return s.GetServerByID(ctx, id) })
}
