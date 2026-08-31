package middleware

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// bucketScript 实现 Redis 令牌桶：容量 = burst，每窗口补充 rps 个令牌。
var bucketScript = redis.NewScript(`
local key    = KEYS[1]
local cap    = tonumber(ARGV[1])
local rate   = tonumber(ARGV[2])   -- tokens per second
local now    = tonumber(ARGV[3])
local state  = redis.call('HMGET', key, 'ts', 'tokens')
local ts     = tonumber(state[1]) or now
local tokens = tonumber(state[2]) or cap
local elapsed = now - ts
tokens = math.min(cap, tokens + elapsed * rate)
local allowed = 0
if tokens >= 1 then
   tokens = tokens - 1
   allowed = 1
end
redis.call('HMSET', key, 'ts', now, 'tokens', tokens)
redis.call('PEXPIRE', key, 15000)
return allowed
`)

// RateLimiter 基于 Redis 的全局/按用户令牌桶限流。
type RateLimiter struct {
	rdb  *redis.Client
	rps  int
	burst int
}

func NewRateLimiter(rdb *redis.Client, rps, burst int) *RateLimiter {
	return &RateLimiter{rdb: rdb, rps: rps, burst: burst}
}

// Allow 若桶内有余量返回 true，否则 false。
func (r *RateLimiter) Allow(ctx context.Context, key string) (bool, error) {
	now := float64(time.Now().UnixMilli()) / 1000.0
	res, err := bucketScript.Run(ctx, r.rdb, []string{"rl:" + key}, r.burst, r.rps, now).Int()
	if err != nil {
		return false, err
	}
	return res == 1, nil
}

