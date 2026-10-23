package verifications

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// RunWorker lapses reserved-name claims whose X verification was not renewed
// within the grace period, locking their IDs until the same X account re-verifies.
func RunWorker(ctx context.Context, svc Service, log *zap.Logger) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n, err := svc.Sweep(ctx); err != nil {
				log.Error("verification sweep", zap.Error(err))
			} else if n > 0 {
				log.Warn("reserved-name claims lapsed for missing re-verification", zap.Int("count", n))
			}
		}
	}
}
