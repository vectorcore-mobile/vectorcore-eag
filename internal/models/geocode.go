package models

import "time"

// GeoCode is a curated reference entry for a SAME or UGC geocode — a small,
// hand-maintained subset (not the full NWS/FCC code universe), selectable
// when composing a CBE alert to attach CAP <geocode> entries.
type GeoCode struct {
	ID          uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Type        string    `gorm:"uniqueIndex:idx_geocode_type_code;index" json:"type"` // SAME or UGC
	Code        string    `gorm:"uniqueIndex:idx_geocode_type_code" json:"code"`
	Description string    `json:"description"`
	Enabled     bool      `gorm:"default:true" json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
