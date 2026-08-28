package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"gorm.io/gorm"

	"github.com/vectorcore/eag/internal/feeds"
	"github.com/vectorcore/eag/internal/models"
)

func registerFeedHandlers(api huma.API, db *gorm.DB, manager *feeds.Manager) {
	huma.Register(api, huma.Operation{
		OperationID: "list-feeds",
		Method:      http.MethodGet,
		Path:        "/api/v1/feeds",
		Summary:     "List all feed sources",
		Tags:        []string{"Feeds"},
	}, func(ctx context.Context, input *struct{}) (*FeedListOutput, error) {
		var sources []models.FeedSource
		db.Find(&sources)
		out := &FeedListOutput{}
		out.Body = feedsToAPI(sources)
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "create-feed",
		Method:        http.MethodPost,
		Path:          "/api/v1/feeds",
		Summary:       "Create feed source",
		Tags:          []string{"Feeds"},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, input *FeedCreateInput) (*FeedOutput, error) {
		paramsJSON, _ := json.Marshal(input.Body.Params)
		src := models.FeedSource{
			Name:            input.Body.Name,
			URL:             input.Body.URL,
			Type:            input.Body.Type,
			Enabled:         input.Body.Enabled,
			PollInterval:    input.Body.PollInterval,
			Params:          string(paramsJSON),
			VerifySignature: input.Body.VerifySignature,
		}
		if src.PollInterval <= 0 {
			src.PollInterval = 60
		}
		if err := db.Create(&src).Error; err != nil {
			return nil, huma.Error422UnprocessableEntity("could not create feed: " + err.Error())
		}
		if src.Enabled {
			manager.AddOrRestart(src.ID)
		}
		out := &FeedOutput{}
		out.Body = feedToAPI(src)
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-feed",
		Method:      http.MethodPut,
		Path:        "/api/v1/feeds/{id}",
		Summary:     "Update feed source",
		Tags:        []string{"Feeds"},
	}, func(ctx context.Context, input *FeedUpdateInput) (*FeedOutput, error) {
		var src models.FeedSource
		if err := db.First(&src, input.ID).Error; err != nil {
			return nil, huma.Error404NotFound("feed not found")
		}
		paramsJSON, _ := json.Marshal(input.Body.Params)
		src.Name = input.Body.Name
		src.URL = input.Body.URL
		src.Type = input.Body.Type
		src.Enabled = input.Body.Enabled
		src.PollInterval = input.Body.PollInterval
		src.Params = string(paramsJSON)
		src.VerifySignature = input.Body.VerifySignature
		if err := db.Save(&src).Error; err != nil {
			return nil, huma.Error422UnprocessableEntity("could not update feed: " + err.Error())
		}
		manager.AddOrRestart(src.ID)
		out := &FeedOutput{}
		out.Body = feedToAPI(src)
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "delete-feed",
		Method:        http.MethodDelete,
		Path:          "/api/v1/feeds/{id}",
		Summary:       "Delete feed source",
		Tags:          []string{"Feeds"},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, input *FeedIDInput) (*struct{}, error) {
		if err := db.Delete(&models.FeedSource{}, input.ID).Error; err != nil {
			return nil, huma.Error404NotFound("feed not found")
		}
		manager.Remove(uint(input.ID))
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "poll-feed",
		Method:        http.MethodPost,
		Path:          "/api/v1/feeds/{id}/poll",
		Summary:       "Trigger immediate poll",
		Tags:          []string{"Feeds"},
		DefaultStatus: http.StatusAccepted,
	}, func(ctx context.Context, input *FeedIDInput) (*struct{}, error) {
		if err := manager.PollNow(uint(input.ID)); err != nil {
			return nil, huma.Error404NotFound("feed not found")
		}
		return nil, nil
	})
}

// --- Types ---

type FeedAPIModel struct {
	ID              uint              `json:"id"`
	Name            string            `json:"name"`
	URL             string            `json:"url"`
	Type            string            `json:"type"`
	Enabled         bool              `json:"enabled"`
	PollInterval    int               `json:"poll_interval"`
	Params          map[string]string `json:"params"`
	VerifySignature bool              `json:"verify_signature"`
	LastPolled      interface{}       `json:"last_polled"`
	LastStatus      string            `json:"last_status"`
	AlertCount      int               `json:"alert_count"`
}

type FeedListOutput struct {
	Body []FeedAPIModel
}

type FeedOutput struct {
	Body FeedAPIModel
}

type FeedIDInput struct {
	ID uint `path:"id"`
}

type FeedBody struct {
	Name            string            `json:"name"`
	URL             string            `json:"url"`
	Type            string            `json:"type"`
	Enabled         bool              `json:"enabled"`
	PollInterval    int               `json:"poll_interval"`
	Params          map[string]string `json:"params"`
	VerifySignature bool              `json:"verify_signature"`
}

type FeedCreateInput struct {
	Body FeedBody
}

type FeedUpdateInput struct {
	ID   uint `path:"id"`
	Body FeedBody
}

func feedToAPI(src models.FeedSource) FeedAPIModel {
	var params map[string]string
	if src.Params != "" {
		json.Unmarshal([]byte(src.Params), &params) //nolint:errcheck
	}
	if params == nil {
		params = map[string]string{}
	}
	return FeedAPIModel{
		ID:              src.ID,
		Name:            src.Name,
		URL:             src.URL,
		Type:            src.Type,
		Enabled:         src.Enabled,
		PollInterval:    src.PollInterval,
		Params:          params,
		VerifySignature: src.VerifySignature,
		LastPolled:      src.LastPolled,
		LastStatus:      src.LastStatus,
		AlertCount:      src.AlertCount,
	}
}

func feedsToAPI(sources []models.FeedSource) []FeedAPIModel {
	out := make([]FeedAPIModel, len(sources))
	for i, s := range sources {
		out[i] = feedToAPI(s)
	}
	return out
}
