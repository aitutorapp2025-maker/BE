// Package database wires up the PostgreSQL connection via GORM and exposes the
// *gorm.DB handle used by repositories.
package database

import (
	"fmt"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Connect opens the PostgreSQL connection pool via GORM and verifies it with a ping.
func Connect(cfg config.Config) (*gorm.DB, error) {
	gormLog := logger.Default.LogMode(logger.Warn)
	if !cfg.IsProduction() {
		gormLog = logger.Default.LogMode(logger.Info)
	}

	db, err := gorm.Open(postgres.Open(cfg.DB.DSN()), &gorm.Config{
		Logger:                                   gormLog,
		DisableForeignKeyConstraintWhenMigrating: false,
	})
	if err != nil {
		return nil, fmt.Errorf("gorm open: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("gorm db handle: %w", err)
	}

	// Sensible pool defaults.
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(time.Hour)

	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("postgres ping: %w", err)
	}

	return db, nil
}

// Close closes the underlying sql.DB connection pool.
func Close(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
