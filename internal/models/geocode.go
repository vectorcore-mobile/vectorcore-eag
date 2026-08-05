package models

import "time"

// GeoCode is a curated reference entry for a CAP geocode — a small,
// hand-maintained subset (not the full universe of any one coding system),
// selectable when composing a CBE alert to attach CAP <geocode> entries.
// Type names the coding system (CAP <valueName>) — e.g. SAME/UGC in the US,
// or another scheme elsewhere — and is stored uppercased.
type GeoCode struct {
	ID          uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Type        string    `gorm:"uniqueIndex:idx_geocode_type_code;index" json:"type"`
	Code        string    `gorm:"uniqueIndex:idx_geocode_type_code" json:"code"`
	Description string    `json:"description"`
	Enabled     bool      `gorm:"default:true" json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
