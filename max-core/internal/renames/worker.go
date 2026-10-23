package renames

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// RunWorker reconciles pending renames with their fee payments until ctx ends.
// The payments worker verifies fees on-chain; this loop only reacts to the result.
func RunWorker(ctx context.Context, svc Service, log *zap.Logger) {
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n, err := svc.Sweep(ctx); err != nil {
				log.Error("rename sweep", zap.Error(err))
			} else if n > 0 {
				log.Info("renames reconciled", zap.Int("count", n))
			}
		}
	}
}
