package expiry

import (
	"context"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"github.com/vectorcore/eag/internal/models"
)

type Worker struct {
	db              *gorm.DB
	sweepInterval   time.Duration
	hardDeleteAfter time.Duration
	triggerCh       chan struct{}
}

func NewWorker(db *gorm.DB, sweepIntervalSecs, hardDeleteAfterHours int) *Worker {
	return &Worker{
		db:              db,
		sweepInterval:   time.Duration(sweepIntervalSecs) * time.Second,
		hardDeleteAfter: time.Duration(hardDeleteAfterHours) * time.Hour,
		triggerCh:       make(chan struct{}, 1),
	}
}

func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.sweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.sweep()
		case <-w.triggerCh:
			w.sweep()
		}
	}
}

// RunNow triggers an immediate sweep (non-blocking).
func (w *Worker) RunNow() {
	select {
	case w.triggerCh <- struct{}{}:
	default:
	}
}

func (w *Worker) sweep() {
	now := time.Now().UTC()

	// Soft-delete expired active alerts
	result := w.db.Where("expires < ? AND deleted_at IS NULL", now).
		Delete(&models.Alert{})
	if result.Error != nil {
		slog.Error("expiry: soft-delete sweep failed", "error", result.Error)
	} else if result.RowsAffected > 0 {
		slog.Info("expiry: soft-deleted expired alerts", "count", result.RowsAffected)
	}

	// Hard-delete stale soft-deleted alerts
	cutoff := now.Add(-w.hardDeleteAfter)
	result = w.db.Unscoped().
		Where("deleted_at IS NOT NULL AND deleted_at < ?", cutoff).
		Delete(&models.Alert{})
	if result.Error != nil {
		slog.Error("expiry: hard-delete sweep failed", "error", result.Error)
	} else if result.RowsAffected > 0 {
		slog.Info("expiry: hard-deleted stale alerts", "count", result.RowsAffected)
	}
}
