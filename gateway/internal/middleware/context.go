package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

type mdKey string

const traceKey mdKey = "mcp.trace"

// GenTraceID 生成 32 位十六进制 trace id。
func GenTraceID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "0000000000000000"
	}
	return hex.EncodeToString(b)
}

func WithTrace(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, traceKey, id)
}

func TraceFrom(ctx context.Context) string {
	if v, ok := ctx.Value(traceKey).(string); ok {
		return v
	}
	return ""
}

