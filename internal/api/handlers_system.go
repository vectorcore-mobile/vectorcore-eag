package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"gorm.io/gorm"

	"github.com/vectorcore/eag/internal/config"
	"github.com/vectorcore/eag/internal/expiry"
	"github.com/vectorcore/eag/internal/models"
	"github.com/vectorcore/eag/internal/xmpp"
)

func registerSystemHandlers(api huma.API, db *gorm.DB, srv *xmpp.Server, exp *expiry.Worker, xmppCfg *config.XMPPServerConfig, mapCfg *config.MapConfig, startAt int64, version string) {
	huma.Register(api, huma.Operation{
		OperationID: "get-status",
		Method:      http.MethodGet,
		Path:        "/api/v1/system/status",
		Summary:     "System health snapshot",
		Tags:        []string{"System"},
	}, func(ctx context.Context, input *struct{}) (*StatusOutput, error) {
		return getStatus(db, srv, xmppCfg, startAt, version)
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-map-config",
		Method:      http.MethodGet,
		Path:        "/api/v1/system/map-config",
		Summary:     "Basemap tile configuration for the web UI",
		Tags:        []string{"System"},
	}, func(ctx context.Context, input *struct{}) (*MapConfigOutput, error) {
		out := &MapConfigOutput{}
		out.Body = MapConfigBody{CartoAPIKey: mapCfg.CartoAPIKey}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "run-expiry",
		Method:        http.MethodPost,
		Path:          "/api/v1/system/expiry",
		Summary:       "Trigger immediate expiry sweep",
		Tags:          []string{"System"},
		DefaultStatus: http.StatusAccepted,
	}, func(ctx context.Context, input *struct{}) (*struct{}, error) {
		exp.RunNow()
		return nil, nil
	})
}

type StatusOutput struct {
	Body StatusBody
}

type MapConfigOutput struct {
	Body MapConfigBody
}

// MapConfigBody is safe to expose unauthenticated: CARTO basemap keys are
// meant for client-side/browser use (see https://carto.com/basemaps/apikey/),
// not a server secret.
type MapConfigBody struct {
	CartoAPIKey string `json:"carto_api_key,omitempty"`
}

type StatusBody struct {
	Version       string           `json:"version"`
	UptimeSeconds int64            `json:"uptime_seconds"`
	Database      DBStatus         `json:"database"`
	Feeds         []FeedStatus     `json:"feeds"`
	XMPPServer    XMPPServerStatus `json:"xmpp_server"`
}

type DBStatus struct {
	Driver                   string `json:"driver"`
	AlertCount               int64  `json:"alert_count"`
	ExpiredPendingHardDelete int64  `json:"expired_pending_hard_delete"`
}

type FeedStatus struct {
	ID         uint        `json:"id"`
	Name       string      `json:"name"`
	Enabled    bool        `json:"enabled"`
	LastPolled interface{} `json:"last_polled"`
	LastStatus string      `json:"last_status"`
	AlertCount int         `json:"alert_count"`
}

type XMPPServerStatus struct {
	C2S       ListenerStatus    `json:"c2s"`
	C2STLS    ListenerStatus    `json:"c2s_tls"`
	PeerCount int               `json:"peer_count"`
	Connected []xmpp.ConnStatus `json:"connected"`
}

type ListenerStatus struct {
	Enabled  bool `json:"enabled"`
	Port     int  `json:"port"`
	STARTTLS bool `json:"starttls,omitempty"`
}

func getStatus(db *gorm.DB, srv *xmpp.Server, xmppCfg *config.XMPPServerConfig, startAt int64, version string) (*StatusOutput, error) {
	uptime := time.Now().Unix() - startAt

	var alertCount int64
	db.Model(&models.Alert{}).Count(&alertCount) //nolint:errcheck

	var expiredCount int64
	db.Model(&models.Alert{}).Unscoped().
		Where("deleted_at IS NOT NULL").
		Count(&expiredCount) //nolint:errcheck

	driver := "sqlite"
	if db.Dialector != nil {
		driver = db.Dialector.Name()
	}

	var feeds []models.FeedSource
	db.Find(&feeds)
	feedStatuses := make([]FeedStatus, len(feeds))
	for i, f := range feeds {
		feedStatuses[i] = FeedStatus{
			ID:         f.ID,
			Name:       f.Name,
			Enabled:    f.Enabled,
			LastPolled: f.LastPolled,
			LastStatus: f.LastStatus,
			AlertCount: f.AlertCount,
		}
	}

	connected := srv.Status()

	out := &StatusOutput{}
	out.Body = StatusBody{
		Version:       version,
		UptimeSeconds: uptime,
		Database: DBStatus{
			Driver:                   driver,
			AlertCount:               alertCount,
			ExpiredPendingHardDelete: expiredCount,
		},
		Feeds: feedStatuses,
		XMPPServer: XMPPServerStatus{
			C2S: ListenerStatus{
				Enabled:  xmppCfg.C2S.Enabled,
				Port:     xmppCfg.C2S.Port,
				STARTTLS: xmppCfg.C2S.STARTTLS,
			},
			C2STLS: ListenerStatus{
				Enabled: xmppCfg.C2STLS.Enabled,
				Port:    xmppCfg.C2STLS.Port,
			},
			PeerCount: len(connected),
			Connected: connected,
		},
	}
	return out, nil
}
