package db

import (
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/vectorcore/eag/internal/models"
)

func Init(driver, dsn string) (*gorm.DB, error) {
	var db *gorm.DB
	var err error

	cfg := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	}

	switch driver {
	case "postgres":
		db, err = initPostgres(dsn, cfg)
	case "sqlite", "":
		db, err = initSQLite(dsn, cfg)
	default:
		return nil, fmt.Errorf("unsupported database driver: %q", driver)
	}
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if err := db.AutoMigrate(
		&models.Alert{},
		&models.FeedSource{},
		&models.XMPPPeer{},
		&models.GeoCode{},
	); err != nil {
		return nil, fmt.Errorf("auto-migrate: %w", err)
	}

	// Composite indexes not expressible via struct tags
	db.Exec("CREATE INDEX IF NOT EXISTS idx_alerts_expires_deleted ON alerts(expires, deleted_at)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_alerts_severity_status ON alerts(severity, status, deleted_at)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_alerts_sent_desc ON alerts(sent DESC)")

	return db, nil
}
