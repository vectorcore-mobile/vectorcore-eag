package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/vectorcore/eag/internal/feeds"
	"github.com/vectorcore/eag/internal/models"
)

// registerCBEHandlers wires up the CBE (Cell Broadcast Entity) alert
// origination endpoint — lets an operator compose and submit a CAP alert
// from the web UI, feeding it into the same insert/broadcast pipeline used
// by polled feeds. Listing reuses GET /api/v1/alerts?feed_source=CBE.
func registerCBEHandlers(api huma.API, db *gorm.DB, manager *feeds.Manager) {
	huma.Register(api, huma.Operation{
		OperationID:   "create-cbe-alert",
		Method:        http.MethodPost,
		Path:          "/api/v1/cbe/alerts",
		Summary:       "Originate a CAP alert from the console",
		Tags:          []string{"CBE"},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, input *CBEAlertInput) (*CBEAlertOutput, error) {
		return createCBEAlert(db, manager, input)
	})
}

// --- Input/Output types ---

type CBEAlertBody struct {
	Sender      string       `json:"sender"                 doc:"CAP <sender> — identifier of the originating authority"`
	SenderName  string       `json:"sender_name,omitempty"  doc:"CAP <senderName> — human-readable authority name"`
	Category    string       `json:"category,omitempty"     doc:"Geo,Met,Safety,Security,Rescue,Fire,Health,Env,Transport,Infra,CBRNE,Other"`
	Event       string       `json:"event"                  doc:"CAP <event> — required"`
	Headline    string       `json:"headline,omitempty"`
	Description string       `json:"description,omitempty"`
	Instruction string       `json:"instruction,omitempty"`
	Severity    string       `json:"severity,omitempty"     doc:"Extreme,Severe,Moderate,Minor,Unknown"`
	Urgency     string       `json:"urgency,omitempty"      doc:"Immediate,Expected,Future,Past,Unknown"`
	Certainty   string       `json:"certainty,omitempty"    doc:"Observed,Likely,Possible,Unlikely,Unknown"`
	Status      string       `json:"status,omitempty"       doc:"Actual,Exercise,System,Test,Draft"`
	MsgType     string       `json:"msg_type,omitempty"     doc:"Alert,Update,Cancel"`
	Scope       string       `json:"scope,omitempty"        doc:"Public,Restricted,Private"`
	References  string       `json:"references,omitempty"   doc:"CAP <references> — 'sender,id,sent' of a prior CBE alert being updated/cancelled"`
	Effective   string       `json:"effective,omitempty"    doc:"ISO 8601 — defaults to now"`
	Onset       string       `json:"onset,omitempty"        doc:"ISO 8601"`
	Expires     string       `json:"expires"                doc:"ISO 8601 — required"`
	AreaDesc    string       `json:"area_desc"               doc:"Required"`
	Geometry    string       `json:"geometry,omitempty"      doc:"GeoJSON FeatureCollection of drawn polygons — required unless at least one geocode is given"`
	GeoCodes    []GeoCodeRef `json:"geocodes,omitempty" doc:"SAME/UGC codes from the reference list — required unless a polygon is drawn, additive to it otherwise"`
}

// GeoCodeRef identifies a reference GeoCode by its CAP fields directly
// (type + code), so the client doesn't need to round-trip a database ID.
type GeoCodeRef struct {
	Type string `json:"type" doc:"SAME or UGC"`
	Code string `json:"code"`
}

type CBEAlertInput struct {
	Body CBEAlertBody
}

type CBEAlertOutput struct {
	Body models.Alert
}

// --- Handler ---

func createCBEAlert(db *gorm.DB, manager *feeds.Manager, input *CBEAlertInput) (*CBEAlertOutput, error) {
	b := input.Body

	var missing []string
	if strings.TrimSpace(b.Sender) == "" {
		missing = append(missing, "sender")
	}
	if strings.TrimSpace(b.Event) == "" {
		missing = append(missing, "event")
	}
	if strings.TrimSpace(b.Expires) == "" {
		missing = append(missing, "expires")
	}
	if strings.TrimSpace(b.AreaDesc) == "" {
		missing = append(missing, "area_desc")
	}
	hasGeometry := strings.TrimSpace(b.Geometry) != ""
	hasGeoCodes := len(b.GeoCodes) > 0
	if !hasGeometry && !hasGeoCodes {
		missing = append(missing, "geometry or geocodes (at least one is required)")
	}
	if len(missing) > 0 {
		return nil, huma.Error422UnprocessableEntity("missing required field(s): " + strings.Join(missing, ", "))
	}

	if hasGeometry {
		var geomCheck interface{}
		if err := json.Unmarshal([]byte(b.Geometry), &geomCheck); err != nil {
			return nil, huma.Error422UnprocessableEntity("geometry is not valid JSON: " + err.Error())
		}
	}

	// Normalized to UTC so stored rows stay comparable with the expiry
	// sweep's "expires < ?" query — see parseTime's doc comment in
	// internal/feeds/nws.go for why mixed-offset TEXT timestamps break that
	// comparison in SQLite.
	expires, err := time.Parse(time.RFC3339, b.Expires)
	if err != nil {
		return nil, huma.Error422UnprocessableEntity("invalid 'expires' date: " + err.Error())
	}
	expires = expires.UTC()

	var effective time.Time
	if strings.TrimSpace(b.Effective) != "" {
		effective, err = time.Parse(time.RFC3339, b.Effective)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("invalid 'effective' date: " + err.Error())
		}
		effective = effective.UTC()
	}

	var onset time.Time
	if strings.TrimSpace(b.Onset) != "" {
		onset, err = time.Parse(time.RFC3339, b.Onset)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("invalid 'onset' date: " + err.Error())
		}
		onset = onset.UTC()
	}

	now := time.Now().UTC()
	if effective.IsZero() {
		effective = now
	}

	status := defaultStr(b.Status, "Actual")
	msgType := defaultStr(b.MsgType, "Alert")
	scope := defaultStr(b.Scope, "Public")
	severity := defaultStr(b.Severity, "Severe")
	urgency := defaultStr(b.Urgency, "Immediate")
	certainty := defaultStr(b.Certainty, "Likely")
	category := defaultStr(b.Category, "Other")

	alert := models.Alert{
		ID:          "CBE-" + uuid.New().String(),
		Sender:      b.Sender,
		Sent:        now,
		Status:      status,
		MsgType:     msgType,
		Scope:       scope,
		References:  b.References,
		Event:       b.Event,
		Headline:    b.Headline,
		Description: b.Description,
		Severity:    severity,
		Urgency:     urgency,
		Certainty:   certainty,
		Effective:   effective,
		Onset:       onset,
		Expires:     expires,
		AreaDesc:    b.AreaDesc,
		FeedSource:  "CBE",
		Geometry:    b.Geometry,
	}

	polygons := geometryToCAPPolygons(b.Geometry)
	alert.RawCAP = buildCBECAPXML(&alert, category, b.Instruction, b.SenderName, polygons, b.GeoCodes)

	manager.UpsertAlertDirect(&alert)

	return &CBEAlertOutput{Body: alert}, nil
}

func defaultStr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

// --- Geometry → CAP polygon conversion ---

// geometryToCAPPolygons extracts CAP 1.2 <polygon> ring strings ("lat,lon
// lat,lon ...") from a GeoJSON document. Accepts FeatureCollection, Feature,
// Polygon, or MultiPolygon at the top level.
func geometryToCAPPolygons(geomJSON string) []string {
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(geomJSON), &raw); err != nil {
		return nil
	}
	return extractPolygons(raw)
}

func extractPolygons(node map[string]interface{}) []string {
	t, _ := node["type"].(string)
	switch t {
	case "FeatureCollection":
		var out []string
		features, _ := node["features"].([]interface{})
		for _, f := range features {
			if fm, ok := f.(map[string]interface{}); ok {
				out = append(out, extractPolygons(fm)...)
			}
		}
		return out
	case "Feature":
		if geom, ok := node["geometry"].(map[string]interface{}); ok {
			return extractPolygons(geom)
		}
		return nil
	case "Polygon":
		coords, _ := node["coordinates"].([]interface{})
		if ring := capRingFromPolygonCoords(coords); ring != "" {
			return []string{ring}
		}
		return nil
	case "MultiPolygon":
		var out []string
		polys, _ := node["coordinates"].([]interface{})
		for _, p := range polys {
			if pc, ok := p.([]interface{}); ok {
				if ring := capRingFromPolygonCoords(pc); ring != "" {
					out = append(out, ring)
				}
			}
		}
		return out
	default:
		return nil
	}
}

// capRingFromPolygonCoords converts a GeoJSON Polygon's exterior ring
// (coordinates[0], [lon,lat] pairs) into a CAP polygon string of "lat,lon"
// pairs separated by spaces.
func capRingFromPolygonCoords(polyCoords []interface{}) string {
	if len(polyCoords) == 0 {
		return ""
	}
	exterior, ok := polyCoords[0].([]interface{})
	if !ok || len(exterior) == 0 {
		return ""
	}
	points := make([]string, 0, len(exterior))
	for _, pt := range exterior {
		coord, ok := pt.([]interface{})
		if !ok || len(coord) < 2 {
			continue
		}
		lon, lonOk := coord[0].(float64)
		lat, latOk := coord[1].(float64)
		if !lonOk || !latOk {
			continue
		}
		points = append(points, fmt.Sprintf("%g,%g", lat, lon))
	}
	if len(points) == 0 {
		return ""
	}
	return strings.Join(points, " ")
}

// --- CAP XML builder ---

// weaSAMEEventCode maps the CBE's WEA alert-class dropdown values to the
// SAME event code the CBC's classifier keys off of (see
// cbe-cap-classification-corrections.md). <event> is free text CAP doesn't
// constrain, so the CBC can't use it for automated Message ID selection —
// these machine-readable codes are what it actually checks. "Imminent
// Threat" and "Public Safety" are intentionally absent: Imminent Threat
// already classifies correctly from severity/urgency/certainty alone, and
// Public Safety has no confirmed standard Message ID (a CBC-side
// configuration decision, not something the CBE can encode).
var weaSAMEEventCode = map[string]string{
	"Presidential Alert": "EAN",
	"AMBER Alert":        "CAE",
	"Test Message":       "RMT",
}

func buildCBECAPXML(a *models.Alert, category, instruction, senderName string, polygons []string, geocodes []GeoCodeRef) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	b.WriteString(`<alert xmlns="urn:oasis:names:tc:emergency:cap:1.2">`)
	cbeXMLWriteTag(&b, "identifier", a.ID)
	cbeXMLWriteTag(&b, "sender", a.Sender)
	cbeXMLWriteTag(&b, "sent", cbeDateTime(a.Sent))
	cbeXMLWriteTag(&b, "status", a.Status)
	cbeXMLWriteTag(&b, "msgType", a.MsgType)
	cbeXMLWriteTag(&b, "scope", a.Scope)
	if a.References != "" {
		cbeXMLWriteTag(&b, "references", a.References)
	}
	b.WriteString("<info>")
	cbeXMLWriteTag(&b, "language", "en-US")
	cbeXMLWriteTag(&b, "category", category)
	cbeXMLWriteTag(&b, "event", a.Event)
	cbeXMLWriteTag(&b, "urgency", a.Urgency)
	cbeXMLWriteTag(&b, "severity", a.Severity)
	cbeXMLWriteTag(&b, "certainty", a.Certainty)
	if code, ok := weaSAMEEventCode[a.Event]; ok {
		b.WriteString("<eventCode>")
		cbeXMLWriteTag(&b, "valueName", "SAME")
		cbeXMLWriteTag(&b, "value", code)
		b.WriteString("</eventCode>")
	}
	if !a.Effective.IsZero() {
		cbeXMLWriteTag(&b, "effective", cbeDateTime(a.Effective))
	}
	if !a.Onset.IsZero() {
		cbeXMLWriteTag(&b, "onset", cbeDateTime(a.Onset))
	}
	if !a.Expires.IsZero() {
		cbeXMLWriteTag(&b, "expires", cbeDateTime(a.Expires))
	}
	if senderName != "" {
		cbeXMLWriteTag(&b, "senderName", senderName)
	}
	if a.Headline != "" {
		cbeXMLWriteTag(&b, "headline", a.Headline)
	}
	if a.Description != "" {
		cbeXMLWriteTag(&b, "description", a.Description)
	}
	if instruction != "" {
		cbeXMLWriteTag(&b, "instruction", instruction)
	}
	b.WriteString("<area>")
	cbeXMLWriteTag(&b, "areaDesc", a.AreaDesc)
	for _, poly := range polygons {
		cbeXMLWriteTag(&b, "polygon", poly)
	}
	for _, gc := range geocodes {
		b.WriteString("<geocode>")
		cbeXMLWriteTag(&b, "valueName", gc.Type)
		cbeXMLWriteTag(&b, "value", gc.Code)
		b.WriteString("</geocode>")
	}
	b.WriteString("</area>")
	b.WriteString("</info>")
	b.WriteString("</alert>")
	return b.String()
}

// cbeDateTime formats a time as CAP 1.2's required dateTime form
// "YYYY-MM-DDThh:mm:ssXzh:zm". The spec explicitly prohibits the "Z"
// designator time.RFC3339 produces for UTC — "Alphabetic timezone
// designators such as 'Z' MUST NOT be used. The timezone for UTC MUST be
// represented as '-00:00'" (CAP-v1.2-os §3.3.2) — so the "+00:00" Go's
// numeric offset format emits for UTC needs rewriting to "-00:00".
func cbeDateTime(t time.Time) string {
	s := t.UTC().Format("2006-01-02T15:04:05-07:00")
	if strings.HasSuffix(s, "+00:00") {
		s = strings.TrimSuffix(s, "+00:00") + "-00:00"
	}
	return s
}

func cbeXMLWriteTag(b *strings.Builder, name, value string) {
	fmt.Fprintf(b, "<%s>%s</%s>", name, cbeXMLEscape(value), name)
}

func cbeXMLEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}
