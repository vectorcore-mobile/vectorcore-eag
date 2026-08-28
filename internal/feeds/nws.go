package feeds

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/vectorcore/eag/internal/models"
)

type nwsResponse struct {
	Features   []nwsFeature `json:"features"`
	Pagination *struct {
		Next string `json:"next"`
	} `json:"pagination"`
}

type nwsFeature struct {
	Geometry   json.RawMessage `json:"geometry"`
	Properties nwsProperties   `json:"properties"`
}

type nwsProperties struct {
	ID            string              `json:"id"`
	AreaDesc      string              `json:"areaDesc"`
	Geocode       map[string][]string `json:"geocode"`
	AffectedZones []string            `json:"affectedZones"`
	Sent          string              `json:"sent"`
	Effective     string              `json:"effective"`
	Onset         string              `json:"onset"`
	Expires       string              `json:"expires"`
	Ends          string              `json:"ends"`
	Status        string              `json:"status"`
	MessageType   string              `json:"messageType"`
	Category      string              `json:"category"`
	Severity      string              `json:"severity"`
	Certainty     string              `json:"certainty"`
	Urgency       string              `json:"urgency"`
	Event         string              `json:"event"`
	Sender        string              `json:"sender"`
	SenderName    string              `json:"senderName"`
	Headline      string              `json:"headline"`
	Description   string              `json:"description"`
	Instruction   string              `json:"instruction"`
	Response      string              `json:"response"`
	References    []struct {
		ID     string `json:"identifier"`
		Sender string `json:"sender"`
		Sent   string `json:"sent"`
	} `json:"references"`
	Scope string `json:"scope"`
}

// buildNWSCAPXML converts NWS GeoJSON properties to a CAP 1.2 XML document.
// Standard CAP fields are mapped directly; NWS-specific fields (affectedZones)
// are carried as <parameter> elements per the CAP 1.2 extension mechanism.
// SAME and UGC geocodes are mapped to <geocode> elements inside <area>.
func buildNWSCAPXML(p nwsProperties) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	b.WriteString(`<alert xmlns="urn:oasis:names:tc:emergency:cap:1.2">`)

	capXMLWriteTag(&b, "identifier", p.ID)
	capXMLWriteTag(&b, "sender", p.Sender)
	capXMLWriteTag(&b, "sent", p.Sent)
	capXMLWriteTag(&b, "status", p.Status)
	capXMLWriteTag(&b, "msgType", p.MessageType)
	capXMLWriteTag(&b, "scope", p.Scope)

	if len(p.References) > 0 {
		refs := make([]string, 0, len(p.References))
		for _, r := range p.References {
			refs = append(refs, r.Sender+","+r.ID+","+r.Sent)
		}
		capXMLWriteTag(&b, "references", strings.Join(refs, " "))
	}

	b.WriteString("<info>")
	capXMLWriteTag(&b, "language", "en-US")

	cat := p.Category
	if cat == "" {
		cat = "Met"
	}
	capXMLWriteTag(&b, "category", cat)
	capXMLWriteTag(&b, "event", p.Event)
	if p.Response != "" {
		capXMLWriteTag(&b, "responseType", p.Response)
	}
	capXMLWriteTag(&b, "urgency", p.Urgency)
	capXMLWriteTag(&b, "severity", p.Severity)
	capXMLWriteTag(&b, "certainty", p.Certainty)
	if p.Effective != "" {
		capXMLWriteTag(&b, "effective", p.Effective)
	}
	if p.Onset != "" {
		capXMLWriteTag(&b, "onset", p.Onset)
	}
	expires := p.Expires
	if expires == "" {
		expires = p.Ends
	}
	if expires != "" {
		capXMLWriteTag(&b, "expires", expires)
	}
	if p.SenderName != "" {
		capXMLWriteTag(&b, "senderName", p.SenderName)
	}
	if p.Headline != "" {
		capXMLWriteTag(&b, "headline", p.Headline)
	}
	if p.Description != "" {
		capXMLWriteTag(&b, "description", p.Description)
	}
	if p.Instruction != "" {
		capXMLWriteTag(&b, "instruction", p.Instruction)
	}

	for _, zone := range p.AffectedZones {
		b.WriteString("<parameter>")
		capXMLWriteTag(&b, "valueName", "affectedZones")
		capXMLWriteTag(&b, "value", zone)
		b.WriteString("</parameter>")
	}

	b.WriteString("<area>")
	capXMLWriteTag(&b, "areaDesc", p.AreaDesc)
	for _, code := range p.Geocode["SAME"] {
		b.WriteString("<geocode>")
		capXMLWriteTag(&b, "valueName", "SAME")
		capXMLWriteTag(&b, "value", code)
		b.WriteString("</geocode>")
	}
	for _, code := range p.Geocode["UGC"] {
		b.WriteString("<geocode>")
		capXMLWriteTag(&b, "valueName", "UGC")
		capXMLWriteTag(&b, "value", code)
		b.WriteString("</geocode>")
	}
	b.WriteString("</area>")

	b.WriteString("</info>")
	b.WriteString("</alert>")
	return b.String()
}

// nwsGeoCodes flattens NWS's geocode map (keyed "SAME"/"UGC") into entries,
// SAME first then UGC, matching the order buildNWSCAPXML emits them in.
func nwsGeoCodes(geocode map[string][]string) []models.GeoCodeEntry {
	var out []models.GeoCodeEntry
	for _, code := range geocode["SAME"] {
		out = append(out, models.GeoCodeEntry{Type: "SAME", Value: code})
	}
	for _, code := range geocode["UGC"] {
		out = append(out, models.GeoCodeEntry{Type: "UGC", Value: code})
	}
	return out
}

// parseTime parses a CAP/NWS timestamp and normalizes it to UTC.
//
// Source feeds report times with their local offset (e.g. "-04:00" for
// NWS eastern-zone alerts). SQLite has no native timestamp type — these
// columns are stored as TEXT, and comparisons like the expiry sweep's
// "expires < ?" are lexicographic, not chronological. Storing values with
// mixed offsets breaks that ordering (e.g. "20:00:00-04:00" sorts before
// "21:14:00+00:00" even though it's chronologically later), which was
// causing not-yet-expired alerts to be soft-deleted prematurely. Normalizing
// every parsed time to UTC before it's stored keeps all rows on the same
// offset so lexicographic and chronological order match.
// capDateTime formats a time as CAP 1.2's required dateTime form
// "YYYY-MM-DDThh:mm:ssXzh:zm". The spec explicitly prohibits the "Z"
// designator time.RFC3339 produces for UTC — "Alphabetic timezone
// designators such as 'Z' MUST NOT be used. The timezone for UTC MUST be
// represented as '-00:00'" (CAP-v1.2-os §3.3.2) — so the "+00:00" Go's
// numeric offset format emits for UTC needs rewriting to "-00:00". Used
// only for locally-synthesized CAP XML (buildFallbackCAPXML) — real feed
// timestamps (buildNWSCAPXML) pass the source's own already-compliant
// dateTime strings straight through.
func capDateTime(t time.Time) string {
	s := t.UTC().Format("2006-01-02T15:04:05-07:00")
	if strings.HasSuffix(s, "+00:00") {
		s = strings.TrimSuffix(s, "+00:00") + "-00:00"
	}
	return s
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05-07:00",
		"2006-01-02T15:04:05Z",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func pollNWS(ctx context.Context, client *http.Client, sourceURL, userAgent, feedName string, params map[string]string) ([]models.Alert, error) {
	// Build initial URL with params
	u, err := url.Parse(sourceURL)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}
	q := u.Query()
	for k, v := range params {
		if v != "" {
			q.Set(k, v)
		}
	}
	u.RawQuery = q.Encode()

	var allAlerts []models.Alert
	nextURL := u.String()

	for nextURL != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, nextURL, nil)
		if err != nil {
			return nil, fmt.Errorf("new request: %w", err)
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", "application/geo+json")

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("http get: %w", err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read body: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("nws returned status %d: %s", resp.StatusCode, string(body))
		}

		var nwsResp nwsResponse
		if err := json.Unmarshal(body, &nwsResp); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}

		now := time.Now()
		for _, f := range nwsResp.Features {
			p := f.Properties

			expires := parseTime(p.Expires)
			if expires.IsZero() {
				expires = parseTime(p.Ends)
			}
			// Skip already-expired alerts
			if !expires.IsZero() && expires.Before(now) {
				continue
			}

			// Build references string
			refs := ""
			for i, r := range p.References {
				if i > 0 {
					refs += " "
				}
				refs += r.Sender + "," + r.ID + "," + r.Sent
			}

			geom := ""
			if len(f.Geometry) > 0 && string(f.Geometry) != "null" {
				geom = string(f.Geometry)
			}

			alert := models.Alert{
				ID:          p.ID,
				Sender:      p.Sender,
				Sent:        parseTime(p.Sent),
				Status:      p.Status,
				MsgType:     p.MessageType,
				Scope:       p.Scope,
				References:  refs,
				Event:       p.Event,
				Headline:    p.Headline,
				Description: p.Description,
				Severity:    p.Severity,
				Urgency:     p.Urgency,
				Certainty:   p.Certainty,
				Effective:   parseTime(p.Effective),
				Onset:       parseTime(p.Onset),
				Expires:     expires,
				AreaDesc:    p.AreaDesc,
				FeedSource:  feedName,
				Geometry:    geom,
				GeoCodes:    models.EncodeGeoCodes(nwsGeoCodes(p.Geocode)),
				RawCAP:      buildNWSCAPXML(p),
			}

			allAlerts = append(allAlerts, alert)
		}

		nextURL = ""
		if nwsResp.Pagination != nil && nwsResp.Pagination.Next != "" {
			slog.Debug("nws: following pagination", "next", nwsResp.Pagination.Next)
			nextURL = nwsResp.Pagination.Next
		}
	}

	return allAlerts, nil
}
