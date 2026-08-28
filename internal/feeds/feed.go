package feeds

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
	extensions "github.com/mmcdole/gofeed/extensions"
	"github.com/vectorcore/eag/internal/models"
)

const capNamespace = "urn:oasis:names:tc:emergency:cap:1.2"

// capAlert represents a parsed CAP XML alert document.
// The XMLName accepts both bare "alert" and the CAP 1.2 namespaced form
// used by FEMA IPAWS and other CAP 1.2 feeds.
type capAlert struct {
	XMLName    xml.Name  `xml:"urn:oasis:names:tc:emergency:cap:1.2 alert"`
	Identifier string    `xml:"identifier"`
	Sender     string    `xml:"sender"`
	Sent       string    `xml:"sent"`
	Status     string    `xml:"status"`
	MsgType    string    `xml:"msgType"`
	Scope      string    `xml:"scope"`
	References string    `xml:"references"`
	Infos      []capInfo `xml:"info"`
}

type capInfo struct {
	Event       string    `xml:"event"`
	Urgency     string    `xml:"urgency"`
	Severity    string    `xml:"severity"`
	Certainty   string    `xml:"certainty"`
	Effective   string    `xml:"effective"`
	Onset       string    `xml:"onset"`
	Expires     string    `xml:"expires"`
	Headline    string    `xml:"headline"`
	Description string    `xml:"description"`
	Areas       []capArea `xml:"area"`
}

// capArea mirrors a CAP 1.2 <area> block, including the shape and geocode
// data buildCBECAPXML/buildNWSCAPXML emit on the way out — parsed here so
// alerts ingested from signed/unsigned CAP XML feeds (e.g. IPAWS-OPEN) get
// the same map + geocode display CBE- and NWS-sourced alerts already do.
type capArea struct {
	AreaDesc string   `xml:"areaDesc"`
	Polygons []string `xml:"polygon"`
	Circles  []string `xml:"circle"`
	GeoCodes []struct {
		ValueName string `xml:"valueName"`
		Value     string `xml:"value"`
	} `xml:"geocode"`
}

func pollFeed(ctx context.Context, client *http.Client, sourceURL, userAgent, feedName string) ([]models.Alert, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("feed returned status %d", resp.StatusCode)
	}

	fp := gofeed.NewParser()
	feed, err := fp.Parse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parse feed: %w", err)
	}

	var alerts []models.Alert
	for _, item := range feed.Items {
		alert, err := itemToAlert(ctx, client, userAgent, feedName, item)
		if err != nil {
			slog.Warn("feed: skipping item", "feed", feedName, "error", err)
			continue
		}
		if alert != nil {
			alerts = append(alerts, *alert)
		}
	}
	return alerts, nil
}

func itemToAlert(ctx context.Context, client *http.Client, userAgent, feedName string, item *gofeed.Item) (*models.Alert, error) {
	// Try CAP XML from extensions
	if cap, rawXML := extractCAPFromExtensions(item); cap != nil {
		return capToAlert(cap, rawXML, feedName), nil
	}

	// Try linked CAP XML URL
	if link := findCAPLink(item); link != "" {
		cap, rawBody, err := fetchCAP(ctx, client, userAgent, link)
		if err != nil {
			slog.Warn("feed: could not fetch CAP link", "url", link, "error", err)
		} else {
			return capToAlert(cap, string(rawBody), feedName), nil
		}
	}

	// Fall back to raw feed item fields — synthesise minimal CAP XML
	slog.Warn("feed: item has no CAP data, using feed fields", "feed", feedName, "title", item.Title)

	id := item.GUID
	if id == "" {
		id = item.Link
	}
	if id == "" {
		return nil, fmt.Errorf("item has no usable ID")
	}

	sent := item.PublishedParsed
	if sent == nil {
		sent = item.UpdatedParsed
	}

	var sentTime time.Time
	if sent != nil {
		sentTime = *sent
	}

	a := &models.Alert{
		ID:          id,
		FeedSource:  feedName,
		Headline:    item.Title,
		Description: item.Description,
		MsgType:     "Alert",
		Status:      "Actual",
		Severity:    "Unknown",
		Urgency:     "Unknown",
		Certainty:   "Unknown",
		Sent:        sentTime,
	}
	a.RawCAP = buildFallbackCAPXML(a)
	return a, nil
}

func extractCAPFromExtensions(item *gofeed.Item) (*capAlert, string) {
	if item.Extensions == nil {
		return nil, ""
	}
	// gofeed Extensions type is map[string]map[string][]extensions.Extension
	// outer key = namespace alias, inner key = element name
	for ns, fields := range item.Extensions {
		if !strings.Contains(ns, "cap") && ns != capNamespace {
			continue
		}
		rawXML, err := buildCAPXMLFromExtensions(fields)
		if err != nil {
			continue
		}
		var cap capAlert
		if err := xml.Unmarshal([]byte(rawXML), &cap); err == nil && cap.Identifier != "" {
			return &cap, rawXML
		}
	}
	return nil, ""
}

func buildCAPXMLFromExtensions(exts map[string][]extensions.Extension) (string, error) {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	sb.WriteString(`<alert xmlns="` + capNamespace + `">`)
	for _, f := range []string{"identifier", "sender", "sent", "status", "msgType", "scope", "references"} {
		if vals, ok := exts[f]; ok && len(vals) > 0 {
			capXMLWriteTag(&sb, f, vals[0].Value)
		}
	}
	sb.WriteString("<info>")
	capXMLWriteTag(&sb, "language", "en-US")
	for _, f := range []string{"category", "event", "responseType", "urgency", "severity", "certainty", "effective", "onset", "expires", "senderName", "headline", "description", "instruction"} {
		if vals, ok := exts[f]; ok && len(vals) > 0 {
			capXMLWriteTag(&sb, f, vals[0].Value)
		}
	}
	if vals, ok := exts["areaDesc"]; ok && len(vals) > 0 {
		sb.WriteString("<area>")
		capXMLWriteTag(&sb, "areaDesc", vals[0].Value)
		sb.WriteString("</area>")
	}
	sb.WriteString("</info>")
	sb.WriteString("</alert>")
	return sb.String(), nil
}

// buildFallbackCAPXML synthesises a minimal CAP 1.2 XML document from plain
// feed item fields when no embedded or linked CAP XML is available.
func buildFallbackCAPXML(a *models.Alert) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	b.WriteString(`<alert xmlns="urn:oasis:names:tc:emergency:cap:1.2">`)
	capXMLWriteTag(&b, "identifier", a.ID)
	if a.Sender != "" {
		capXMLWriteTag(&b, "sender", a.Sender)
	}
	capXMLWriteTag(&b, "sent", capDateTime(a.Sent))
	capXMLWriteTag(&b, "status", a.Status)
	capXMLWriteTag(&b, "msgType", a.MsgType)
	capXMLWriteTag(&b, "scope", "Public")
	b.WriteString("<info>")
	capXMLWriteTag(&b, "language", "en-US")
	capXMLWriteTag(&b, "category", "Other")
	if a.Event != "" {
		capXMLWriteTag(&b, "event", a.Event)
	} else {
		capXMLWriteTag(&b, "event", a.Headline)
	}
	capXMLWriteTag(&b, "urgency", a.Urgency)
	capXMLWriteTag(&b, "severity", a.Severity)
	capXMLWriteTag(&b, "certainty", a.Certainty)
	if a.Headline != "" {
		capXMLWriteTag(&b, "headline", a.Headline)
	}
	if a.Description != "" {
		capXMLWriteTag(&b, "description", a.Description)
	}
	if a.AreaDesc != "" {
		b.WriteString("<area>")
		capXMLWriteTag(&b, "areaDesc", a.AreaDesc)
		b.WriteString("</area>")
	}
	b.WriteString("</info>")
	b.WriteString("</alert>")
	return b.String()
}

// capXMLWriteTag writes a single XML element with an escaped text value.
func capXMLWriteTag(b *strings.Builder, name, value string) {
	fmt.Fprintf(b, "<%s>%s</%s>", name, capXMLEscape(value), name)
}

// capXMLEscape escapes the five XML predefined entities.
func capXMLEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}

func findCAPLink(item *gofeed.Item) string {
	// Check enclosures first (standard CAP-in-Atom pattern)
	for _, enc := range item.Enclosures {
		if strings.Contains(enc.Type, "cap") || strings.HasSuffix(enc.URL, ".cap") || strings.HasSuffix(enc.URL, ".xml") {
			return enc.URL
		}
	}
	// Fall back to the entry's main link (FEMA IPAWS pattern: each Atom entry
	// links directly to a CAP XML document with no extension or MIME hint).
	if item.Link != "" {
		return item.Link
	}
	return ""
}

func fetchCAP(ctx context.Context, client *http.Client, userAgent, link string) (*capAlert, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}

	var cap capAlert
	if err := xml.Unmarshal(body, &cap); err != nil {
		return nil, nil, err
	}
	return &cap, body, nil
}

func capToAlert(cap *capAlert, rawXML string, feedName string) *models.Alert {
	a := &models.Alert{
		ID:         cap.Identifier,
		Sender:     cap.Sender,
		Sent:       parseTime(cap.Sent),
		Status:     cap.Status,
		MsgType:    cap.MsgType,
		Scope:      cap.Scope,
		References: cap.References,
		FeedSource: feedName,
		RawCAP:     rawXML,
	}

	if len(cap.Infos) > 0 {
		info := cap.Infos[0]
		a.Event = info.Event
		a.Urgency = info.Urgency
		a.Severity = info.Severity
		a.Certainty = info.Certainty
		a.Effective = parseTime(info.Effective)
		a.Onset = parseTime(info.Onset)
		a.Expires = parseTime(info.Expires)
		a.Headline = info.Headline
		a.Description = info.Description
		if len(info.Areas) > 0 {
			areas := make([]string, len(info.Areas))
			for i, ar := range info.Areas {
				areas[i] = ar.AreaDesc
			}
			a.AreaDesc = strings.Join(areas, "; ")
			a.Geometry = areasToGeometry(info.Areas)
			a.GeoCodes = models.EncodeGeoCodes(areasToGeoCodes(info.Areas))
		}
	}

	return a
}

// areasToGeometry converts every <polygon>/<circle> across all of an alert's
// <area> blocks into one GeoJSON FeatureCollection — a Polygon feature per
// CAP polygon, a Point feature with a "radius" property (meters) per CAP
// circle, matching the convention the CBE compose UI already uses so
// AlertMap can render both the same way regardless of source. Returns ""
// when there are no shapes (geocode-only areas are common and fine).
func areasToGeometry(areas []capArea) string {
	var features []map[string]interface{}
	for _, ar := range areas {
		for _, poly := range ar.Polygons {
			coords := capRingToGeoJSON(poly)
			if coords == nil {
				continue
			}
			features = append(features, map[string]interface{}{
				"type":       "Feature",
				"properties": map[string]interface{}{},
				"geometry": map[string]interface{}{
					"type":        "Polygon",
					"coordinates": [][][2]float64{coords},
				},
			})
		}
		for _, c := range ar.Circles {
			lon, lat, radiusM, ok := capCircleToPoint(c)
			if !ok {
				continue
			}
			features = append(features, map[string]interface{}{
				"type":       "Feature",
				"properties": map[string]interface{}{"radius": radiusM},
				"geometry": map[string]interface{}{
					"type":        "Point",
					"coordinates": [2]float64{lon, lat},
				},
			})
		}
	}
	if len(features) == 0 {
		return ""
	}
	b, err := json.Marshal(map[string]interface{}{
		"type":     "FeatureCollection",
		"features": features,
	})
	if err != nil {
		return ""
	}
	return string(b)
}

// capRingToGeoJSON parses a CAP polygon string ("lat,lon lat,lon ...") into
// a GeoJSON exterior ring ([lon,lat] pairs), closing it if the source didn't
// repeat the first point as required by the GeoJSON spec.
func capRingToGeoJSON(poly string) [][2]float64 {
	fields := strings.Fields(poly)
	if len(fields) < 3 {
		return nil
	}
	ring := make([][2]float64, 0, len(fields)+1)
	for _, pair := range fields {
		lat, lon, ok := parseLatLon(pair)
		if !ok {
			return nil
		}
		ring = append(ring, [2]float64{lon, lat})
	}
	if len(ring) < 3 {
		return nil
	}
	if ring[0] != ring[len(ring)-1] {
		ring = append(ring, ring[0])
	}
	return ring
}

// capCircleToPoint parses a CAP circle string ("lat,lon radius_km") into a
// (lon, lat, radius_meters) triple.
func capCircleToPoint(circle string) (lon, lat, radiusM float64, ok bool) {
	fields := strings.Fields(circle)
	if len(fields) != 2 {
		return 0, 0, 0, false
	}
	lat, lon, ok = parseLatLon(fields[0])
	if !ok {
		return 0, 0, 0, false
	}
	radiusKm, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return 0, 0, 0, false
	}
	return lon, lat, radiusKm * 1000, true
}

func parseLatLon(pair string) (lat, lon float64, ok bool) {
	parts := strings.SplitN(pair, ",", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	lat, err1 := strconv.ParseFloat(parts[0], 64)
	lon, err2 := strconv.ParseFloat(parts[1], 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return lat, lon, true
}

func areasToGeoCodes(areas []capArea) []models.GeoCodeEntry {
	var out []models.GeoCodeEntry
	for _, ar := range areas {
		for _, gc := range ar.GeoCodes {
			if gc.ValueName == "" || gc.Value == "" {
				continue
			}
			out = append(out, models.GeoCodeEntry{Type: gc.ValueName, Value: gc.Value})
		}
	}
	return out
}
