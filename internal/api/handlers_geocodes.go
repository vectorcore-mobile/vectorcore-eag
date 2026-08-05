package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"gorm.io/gorm"

	"github.com/vectorcore/eag/internal/models"
)

// registerGeoCodeHandlers wires up CRUD for the curated geocode reference
// list — a hand-maintained subset (not the full universe of any one coding
// system) that the CBE compose form reads from to attach CAP <geocode>
// entries to an alert. Type is an open string (CAP's <valueName> can name
// any coding system — SAME/UGC in the US, but e.g. SGC, JMA_AREA, or
// ISO3166-2 elsewhere), normalized to uppercase for consistent grouping.
func registerGeoCodeHandlers(api huma.API, db *gorm.DB) {
	huma.Register(api, huma.Operation{
		OperationID: "list-geocodes",
		Method:      http.MethodGet,
		Path:        "/api/v1/geocodes",
		Summary:     "List reference SAME/UGC geocodes",
		Tags:        []string{"GeoCodes"},
	}, func(ctx context.Context, input *struct{}) (*GeoCodeListOutput, error) {
		var codes []models.GeoCode
		db.Order("type, code").Find(&codes)
		out := &GeoCodeListOutput{}
		out.Body = codes
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "create-geocode",
		Method:        http.MethodPost,
		Path:          "/api/v1/geocodes",
		Summary:       "Create a reference geocode",
		Tags:          []string{"GeoCodes"},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, input *GeoCodeCreateInput) (*GeoCodeOutput, error) {
		gc := models.GeoCode{
			Type:        strings.ToUpper(strings.TrimSpace(input.Body.Type)),
			Code:        input.Body.Code,
			Description: input.Body.Description,
			Enabled:     input.Body.Enabled,
		}
		if gc.Type == "" {
			return nil, huma.Error422UnprocessableEntity("type is required")
		}
		if gc.Code == "" {
			return nil, huma.Error422UnprocessableEntity("code is required")
		}
		if err := db.Create(&gc).Error; err != nil {
			return nil, huma.Error422UnprocessableEntity("could not create geocode: " + err.Error())
		}
		out := &GeoCodeOutput{}
		out.Body = gc
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-geocode",
		Method:      http.MethodPut,
		Path:        "/api/v1/geocodes/{id}",
		Summary:     "Update a reference geocode",
		Tags:        []string{"GeoCodes"},
	}, func(ctx context.Context, input *GeoCodeUpdateInput) (*GeoCodeOutput, error) {
		var gc models.GeoCode
		if err := db.First(&gc, input.ID).Error; err != nil {
			return nil, huma.Error404NotFound("geocode not found")
		}
		normType := strings.ToUpper(strings.TrimSpace(input.Body.Type))
		if normType == "" {
			return nil, huma.Error422UnprocessableEntity("type is required")
		}
		if input.Body.Code == "" {
			return nil, huma.Error422UnprocessableEntity("code is required")
		}
		gc.Type = normType
		gc.Code = input.Body.Code
		gc.Description = input.Body.Description
		gc.Enabled = input.Body.Enabled
		if err := db.Save(&gc).Error; err != nil {
			return nil, huma.Error422UnprocessableEntity("could not update geocode: " + err.Error())
		}
		out := &GeoCodeOutput{}
		out.Body = gc
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "delete-geocode",
		Method:        http.MethodDelete,
		Path:          "/api/v1/geocodes/{id}",
		Summary:       "Delete a reference geocode",
		Tags:          []string{"GeoCodes"},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, input *GeoCodeIDInput) (*struct{}, error) {
		if err := db.Delete(&models.GeoCode{}, input.ID).Error; err != nil {
			return nil, huma.Error404NotFound("geocode not found")
		}
		return nil, nil
	})
}

// --- Types ---

type GeoCodeListOutput struct {
	Body []models.GeoCode
}

type GeoCodeOutput struct {
	Body models.GeoCode
}

type GeoCodeIDInput struct {
	ID uint `path:"id"`
}

type GeoCodeBody struct {
	Type        string `json:"type"        doc:"Geocode scheme, e.g. SAME, UGC — normalized to uppercase"`
	Code        string `json:"code"`
	Description string `json:"description,omitempty"`
	Enabled     bool   `json:"enabled"`
}

type GeoCodeCreateInput struct {
	Body GeoCodeBody
}

type GeoCodeUpdateInput struct {
	ID   uint `path:"id"`
	Body GeoCodeBody
}
