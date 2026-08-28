package models

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// GeoCodeEntry is one CAP <geocode> (valueName/value pair, e.g. SAME "001000"
// or UGC "ALZ001") attached to an alert's area(s).
type GeoCodeEntry struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// EncodeGeoCodes JSON-encodes geocode entries for storage in Alert.GeoCodes,
// matching the raw-JSON-text convention already used for Geometry/Params.
// Returns "" for an empty list so the column stays empty rather than "[]".
func EncodeGeoCodes(entries []GeoCodeEntry) string {
	if len(entries) == 0 {
		return ""
	}
	b, err := json.Marshal(entries)
	if err != nil {
		return ""
	}
	return string(b)
}

type Alert struct {
	// Primary key is the CAP <identifier> field — globally unique per CAP spec.
	ID          string    `gorm:"primaryKey" json:"id"`
	Sender      string    `gorm:"index" json:"sender"`
	Sent        time.Time `gorm:"index" json:"sent"`
	Status      string    `gorm:"index" json:"status"`
	MsgType     string    `gorm:"index" json:"msg_type"`
	Scope       string    `json:"scope"`
	References  string    `gorm:"type:text" json:"references"`
	Event       string    `gorm:"index" json:"event"`
	Headline    string    `json:"headline"`
	Description string    `gorm:"type:text" json:"description"`
	Severity    string    `gorm:"index" json:"severity"`
	Urgency     string    `gorm:"index" json:"urgency"`
	Certainty   string    `json:"certainty"`
	Effective   time.Time `json:"effective"`
	Onset       time.Time `json:"onset"`
	Expires     time.Time `gorm:"index" json:"expires"`
	AreaDesc    string    `gorm:"index" json:"area_desc"`
	FeedSource  string    `gorm:"index" json:"feed_source"`
	Geometry    string    `gorm:"type:text" json:"geometry,omitempty"` // GeoJSON geometry object
	// GeoCodes is a JSON-encoded []GeoCodeEntry of the alert area(s)' CAP
	// <geocode> entries (e.g. SAME, UGC), if any.
	GeoCodes string `gorm:"type:text" json:"geocodes,omitempty"`
	RawCAP   string `gorm:"type:text" json:"raw_cap,omitempty"`
	// SignatureStatus reflects capverify.Result.Status() when the owning
	// FeedSource has VerifySignature enabled: "verified", "invalid",
	// "revoked", or "unsigned". Empty when verification wasn't performed.
	SignatureStatus string         `json:"signature_status,omitempty"`
	Forwarded       bool           `gorm:"default:false;index" json:"forwarded"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	DeletedAt       gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}
