package payments

import (
	"context"
	"time"

	"go.uber.org/zap"
)

type Worker struct {
	svc      Service
	repo     Repository
	log      *zap.Logger
	interval time.Duration
	batch    int32
	lease    time.Duration
}

func NewWorker(svc Service, repo Repository, log *zap.Logger) *Worker {
	return &Worker{svc: svc, repo: repo, log: log, interval: 2 * time.Second, batch: 25, lease: 30 * time.Second}
}

// Run polls until ctx is cancelled. Leasing makes it safe to run on several
// instances at once.
func (w *Worker) Run(ctx context.Context) {
	w.log.Info("payment worker started")
	t := time.NewTicker(w.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			w.log.Info("payment worker stopped")
			return
		case <-t.C:
			w.tick(ctx)
		}
	}
}

func (w *Worker) tick(ctx context.Context) {
	if n, err := w.svc.ExpireStale(ctx); err != nil {
		w.log.Error("expire payments", zap.Error(err))
	} else if n > 0 {
		w.log.Info("expired payment intents", zap.Int("count", n))
	}

	due, err := w.repo.LeaseDue(ctx, w.batch, w.lease)
	if err != nil {
		w.log.Error("lease due payments", zap.Error(err))
		return
	}
	for _, p := range due {
		pctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		if err := w.svc.Process(pctx, p); err != nil {
			w.log.Error("process payment", zap.String("payment_id", p.ID.String()), zap.Error(err))
		}
		cancel()
	}
}
