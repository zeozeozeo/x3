package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
)

// DB is the global database connection pool for the commands package.
var DB *sql.DB

// InitDB initializes the database connection and ensures tables are created.
func InitDB(dataSourceName string) error {
	var err error
	DB, err = sql.Open("sqlite3", dataSourceName)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// Ping DB to ensure connection is valid
	if err = DB.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}
	slog.Info("Database connection established", slog.String("dataSource", dataSourceName))

	// Run migrations/table creations
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS whitelist ( user_id TEXT PRIMARY KEY )`,
		`CREATE TABLE IF NOT EXISTS channel_cache ( channel_id TEXT PRIMARY KEY, cache BLOB )`,
		`CREATE TABLE IF NOT EXISTS message_interaction_cache ( message_id TEXT PRIMARY KEY, prompt TEXT )`,
		`CREATE TABLE IF NOT EXISTS global_stats ( stats BLOB )`,
		`CREATE TABLE IF NOT EXISTS server_stats ( server_id TEXT PRIMARY KEY, stats BLOB )`,
		`CREATE TABLE IF NOT EXISTS blacklist ( channel_id TEXT PRIMARY KEY )`,
		`CREATE TABLE IF NOT EXISTS image_blacklist ( channel_id TEXT PRIMARY KEY )`,
		`CREATE TABLE IF NOT EXISTS users ( user_id TEXT PRIMARY KEY, last_interaction_time TIMESTAMP DEFAULT CURRENT_TIMESTAMP )`,
		`CREATE TABLE IF NOT EXISTS antiscam_servers ( server_id TEXT PRIMARY KEY )`,
		`CREATE TABLE IF NOT EXISTS image_descriptions ( image_url TEXT PRIMARY KEY, description TEXT, created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP )`,
		`CREATE TABLE IF NOT EXISTS user_cache ( user_id TEXT PRIMARY KEY, cache BLOB )`,
	}

	for i, migration := range migrations {
		_, err = DB.Exec(migration)
		if err != nil {
			return fmt.Errorf("failed to execute migration %d: %w", i+1, err)
		}
	}
	slog.Info("Database tables ensured")

	// Ensure global_stats has one row
	var count int
	err = DB.QueryRow("SELECT COUNT(*) FROM global_stats").Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to query global_stats count: %w", err)
	}
	if count == 0 {
		slog.Info("Initializing global_stats row")
		data, err := json.Marshal(GlobalStats{})
		if err != nil {
			return fmt.Errorf("failed to marshal initial global stats: %w", err)
		}
		_, err = DB.Exec("INSERT INTO global_stats (stats) VALUES (?)", data)
		if err != nil {
			return fmt.Errorf("failed to insert initial global stats: %w", err)
		}
	}

	// Add default whitelist entry
	AddToWhitelist(890686470556356619)

	return nil
}
