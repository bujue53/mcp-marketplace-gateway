package metrics

import (
	"context"
	"sync/atomic"
	"time"
)

type timingKey struct{}
type Timing struct{ DownstreamNS atomic.Int64 }

func WithTiming(ctx context.Context) (context.Context, *Timing) {
	timing := &Timing{}
	return context.WithValue(ctx, timingKey{}, timing), timing
}
func RecordDownstream(ctx context.Context, duration time.Duration) {
	if timing, ok := ctx.Value(timingKey{}).(*Timing); ok {
		timing.DownstreamNS.Add(int64(duration))
	}
}
