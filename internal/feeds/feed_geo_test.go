package feeds

import (
	"encoding/json"
	"testing"
)

func TestAreasToGeometryAndGeoCodes(t *testing.T) {
	areas := []capArea{
		{
			AreaDesc: "Zone A",
			Polygons: []string{"40.1,-90.1 40.2,-90.1 40.2,-90.2 40.1,-90.2"},
			Circles:  []string{"39.5,-89.5 12.5"},
			GeoCodes: []struct {
				ValueName string `xml:"valueName"`
				Value     string `xml:"value"`
			}{
				{ValueName: "SAME", Value: "017001"},
			},
		},
		{
			AreaDesc: "Zone B",
			Polygons: []string{"41.1,-91.1 41.2,-91.1 41.2,-91.2"},
		},
	}

	geomJSON := areasToGeometry(areas)
	if geomJSON == "" {
		t.Fatal("expected non-empty geometry")
	}
	var geom map[string]interface{}
	if err := json.Unmarshal([]byte(geomJSON), &geom); err != nil {
		t.Fatalf("invalid geometry JSON: %v", err)
	}
	feats := geom["features"].([]interface{})
	if len(feats) != 3 {
		t.Fatalf("expected 3 features (2 polygons + 1 circle), got %d: %s", len(feats), geomJSON)
	}

	var sawPolygon, sawCircle int
	for _, f := range feats {
		fm := f.(map[string]interface{})
		g := fm["geometry"].(map[string]interface{})
		switch g["type"] {
		case "Polygon":
			sawPolygon++
			ring := g["coordinates"].([]interface{})[0].([]interface{})
			first := ring[0].([]interface{})
			last := ring[len(ring)-1].([]interface{})
			if first[0] != last[0] || first[1] != last[1] {
				t.Errorf("ring not closed: %v vs %v", first, last)
			}
		case "Point":
			sawCircle++
			props := fm["properties"].(map[string]interface{})
			radius := props["radius"].(float64)
			if radius != 12500 {
				t.Errorf("expected radius 12500m (12.5km), got %v", radius)
			}
			coords := g["coordinates"].([]interface{})
			lon, lat := coords[0].(float64), coords[1].(float64)
			if lon != -89.5 || lat != 39.5 {
				t.Errorf("expected lon=-89.5 lat=39.5, got lon=%v lat=%v", lon, lat)
			}
		}
	}
	if sawPolygon != 2 || sawCircle != 1 {
		t.Errorf("expected 2 polygons + 1 circle, got %d polygons + %d circles", sawPolygon, sawCircle)
	}

	entries := areasToGeoCodes(areas)
	if len(entries) != 1 || entries[0].Type != "SAME" || entries[0].Value != "017001" {
		t.Errorf("unexpected geocode entries: %+v", entries)
	}
}
