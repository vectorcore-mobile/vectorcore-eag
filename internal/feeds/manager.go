package feeds

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/vectorcore/eag/internal/config"
	"github.com/vectorcore/eag/internal/models"
	"github.com/vectorcore/eag/internal/xmpp"
)

// Publisher is the interface the manager uses to forward alerts.
type Publisher interface {
	Broadcast(alert *models.Alert, db *gorm.DB)
}

type Manager struct {
	db        *gorm.DB
	cfg       *config.FeedsConfig
	publisher Publisher
	client    *http.Client

	ctx     context.Context // root context; used for background polls
	mu      sync.Mutex
	pollers map[uint]context.CancelFunc
}

func NewManager(db *gorm.DB, cfg *config.FeedsConfig, pub Publisher) *Manager {
	return &Manager{
		db:        db,
		cfg:       cfg,
		publisher: pub,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		pollers: make(map[uint]context.CancelFunc),
	}
}

// Start launches pollers for all enabled input sources from the database.
func (m *Manager) Start(ctx context.Context) {
	m.ctx = ctx
	var sources []models.FeedSource
	m.db.Where("enabled = ?", true).Find(&sources)
	for _, src := range sources {
		m.startPoller(ctx, src)
	}
}

func (m *Manager) startPoller(ctx context.Context, src models.FeedSource) {
	pollerCtx, cancel := context.WithCancel(ctx)
	m.mu.Lock()
	m.pollers[src.ID] = cancel
	m.mu.Unlock()

	go m.runPoller(pollerCtx, src)
}

func (m *Manager) runPoller(ctx context.Context, src models.FeedSource) {
	interval := time.Duration(src.PollInterval) * time.Second
	if interval <= 0 {
		interval = time.Duration(m.cfg.PollInterval) * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Poll immediately on start
	m.poll(ctx, src.ID)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.poll(ctx, src.ID)
		}
	}
}

func (m *Manager) poll(ctx context.Context, sourceID uint) {
	// Re-read from DB to get latest config
	var src models.FeedSource
	if err := m.db.First(&src, sourceID).Error; err != nil {
		slog.Error("feeds: failed to load source", "id", sourceID, "error", err)
		return
	}
	if !src.Enabled {
		return
	}

	var params map[string]string
	if src.Params != "" {
		json.Unmarshal([]byte(src.Params), &params) //nolint:errcheck
	}

	pollCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	var alerts []models.Alert
	var pollErr error

	switch src.Type {
	case "nws":
		alerts, pollErr = pollNWS(pollCtx, m.client, src.URL, m.cfg.UserAgent, src.Name, params)
	case "atom", "rss":
		alerts, pollErr = pollFeed(pollCtx, m.client, src.URL, m.cfg.UserAgent, src.Name)
	default:
		slog.Warn("feeds: unknown source type", "type", src.Type, "name", src.Name)
		return
	}

	now := time.Now()
	if pollErr != nil {
		slog.Error("feeds: poll failed", "name", src.Name, "error", pollErr)
		m.db.Model(&src).Updates(map[string]interface{}{
			"last_status": "error: " + pollErr.Error(),
			"last_polled": now,
		})
		return
	}

	count := 0
	for i := range alerts {
		if m.upsertAlert(&alerts[i]) {
			count++
		}
	}

	m.db.Model(&src).Updates(map[string]interface{}{
		"last_polled": now,
		"last_status": "ok",
		"alert_count": gorm.Expr("alert_count + ?", count),
	})

	slog.Debug("feeds: poll complete", "name", src.Name, "new_or_updated", count, "total", len(alerts))
}

// upsertAlert inserts or updates an alert; returns true if it was new or changed.
func (m *Manager) upsertAlert(alert *models.Alert) bool {
	// Drop alerts that have no usable CAP data — failed parses produce zero
	// timestamps and empty event fields. Sent and Event are the minimum required
	// fields for a meaningful alert record.
	if alert.Sent.IsZero() || alert.Event == "" {
		slog.Debug("feeds: dropping alert with missing CAP data",
			"id", alert.ID, "sent_zero", alert.Sent.IsZero(), "event_empty", alert.Event == "")
		return false
	}

	var existing models.Alert
	err := m.db.Unscoped().First(&existing, "id = ?", alert.ID).Error

	if err != nil {
		// New alert
		if err := m.db.Create(alert).Error; err != nil {
			slog.Error("feeds: insert alert failed", "id", alert.ID, "error", err)
			return false
		}
		m.handleMsgType(alert)
		m.publisher.Broadcast(alert, m.db)
		return true
	}

	// Existing alert — check if meaningful fields changed. A prior soft-delete
	// (the expiry sweep, or a source re-reporting an alert it had previously
	// stopped serving) counts as a change too: the source is actively
	// reporting this alert again right now, so it belongs back in the active
	// view rather than staying invisible forever.
	wasDeleted := existing.DeletedAt.Valid
	changed := wasDeleted ||
		existing.Expires != alert.Expires ||
		existing.Severity != alert.Severity ||
		existing.Urgency != alert.Urgency ||
		existing.Headline != alert.Headline

	updates := map[string]interface{}{
		"sender":      alert.Sender,
		"sent":        alert.Sent,
		"status":      alert.Status,
		"msg_type":    alert.MsgType,
		"scope":       alert.Scope,
		"references":  alert.References,
		"event":       alert.Event,
		"headline":    alert.Headline,
		"description": alert.Description,
		"severity":    alert.Severity,
		"urgency":     alert.Urgency,
		"certainty":   alert.Certainty,
		"effective":   alert.Effective,
		"onset":       alert.Onset,
		"expires":     alert.Expires,
		"area_desc":   alert.AreaDesc,
		"raw_cap":     alert.RawCAP,
	}
	if changed {
		updates["forwarded"] = false
	}
	if wasDeleted {
		updates["deleted_at"] = nil
	}

	if err := m.db.Model(&models.Alert{}).Unscoped().Where("id = ?", alert.ID).Updates(updates).Error; err != nil {
		slog.Error("feeds: update alert failed", "id", alert.ID, "error", err)
		return false
	}

	if changed {
		// Re-read updated record
		m.db.Unscoped().First(alert, "id = ?", alert.ID)
		m.handleMsgType(alert)
		m.publisher.Broadcast(alert, m.db)
		return true
	}
	return false
}

// handleMsgType processes UPDATE and CANCEL msg types by soft-deleting referenced alerts.
func (m *Manager) handleMsgType(alert *models.Alert) {
	if alert.MsgType != "Update" && alert.MsgType != "Cancel" {
		return
	}
	if alert.References == "" {
		return
	}
	// References format: "sender,id,sent sender,id,sent ..."
	ids := parseReferenceIDs(alert.References)
	if len(ids) == 0 {
		return
	}
	result := m.db.Where("id IN ?", ids).Delete(&models.Alert{})
	if result.Error != nil {
		slog.Error("feeds: soft-delete referenced alerts failed", "error", result.Error)
	} else if result.RowsAffected > 0 {
		slog.Info("feeds: soft-deleted referenced alerts",
			"msg_type", alert.MsgType, "count", result.RowsAffected)
	}
}

func parseReferenceIDs(refs string) []string {
	parts := strings.Fields(refs)
	ids := make([]string, 0, len(parts))
	for _, p := range parts {
		segments := strings.Split(p, ",")
		if len(segments) >= 2 {
			ids = append(ids, segments[1])
		}
	}
	return ids
}

// PollNow triggers an immediate poll for the given source ID.
// Uses the manager's root context so the poll survives beyond the HTTP request.
func (m *Manager) PollNow(sourceID uint) error {
	var src models.FeedSource
	if err := m.db.First(&src, sourceID).Error; err != nil {
		return err
	}
	go m.poll(m.ctx, src.ID)
	return nil
}

// AddOrRestart starts or restarts the poller for a source.
func (m *Manager) AddOrRestart(sourceID uint) {
	m.mu.Lock()
	if cancel, ok := m.pollers[sourceID]; ok {
		cancel()
		delete(m.pollers, sourceID)
	}
	m.mu.Unlock()

	var src models.FeedSource
	if err := m.db.First(&src, sourceID).Error; err != nil {
		return
	}
	if src.Enabled {
		m.startPoller(m.ctx, src)
	}
}

// Remove stops the poller for a source.
func (m *Manager) Remove(sourceID uint) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cancel, ok := m.pollers[sourceID]; ok {
		cancel()
		delete(m.pollers, sourceID)
	}
}

// UpsertAlertDirect is used by the API poll-now handler to process polled alerts directly.
func (m *Manager) UpsertAlertDirect(alert *models.Alert) {
	m.upsertAlert(alert)
}

// ensure *xmpp.Server satisfies Publisher interface at compile time.
var _ Publisher = (*xmpp.Server)(nil)

// Ensure clause import is used (for future use)
var _ = clause.OnConflict{}
