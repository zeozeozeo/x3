package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	_ "modernc.org/sqlite"
)

// DB is the global database connection pool for the commands package.
var DB *sql.DB

// InitDB initializes the database connection and ensures tables are created.
func InitDB(dataSourceName string) error {
	var err error
	DB, err = sql.Open("sqlite", dataSourceName)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// Ping DB to ensure connection is valid
	if err = DB.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}
	slog.Info("Database connection established", slog.String("dataSource", dataSourceName))

	pragmas := []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA synchronous=NORMAL`,
		`PRAGMA busy_timeout=5000`,
	}
	for i, pragma := range pragmas {
		if _, err = DB.Exec(pragma); err != nil {
			return fmt.Errorf("failed to execute pragma %d: %w", i+1, err)
		}
	}

	// Run migrations/table creations
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS whitelist ( user_id TEXT PRIMARY KEY )`,
		`CREATE TABLE IF NOT EXISTS channel_cache ( channel_id TEXT PRIMARY KEY, cache BLOB )`,
		`CREATE TABLE IF NOT EXISTS message_interaction_cache ( message_id TEXT PRIMARY KEY, prompt TEXT )`,
		`CREATE TABLE IF NOT EXISTS message_render_cache ( message_id TEXT PRIMARY KEY, content TEXT )`,
		`CREATE TABLE IF NOT EXISTS global_stats ( stats BLOB )`,
		`CREATE TABLE IF NOT EXISTS server_stats ( server_id TEXT PRIMARY KEY, stats BLOB )`,
		`CREATE TABLE IF NOT EXISTS blacklist ( channel_id TEXT PRIMARY KEY )`,
		`CREATE TABLE IF NOT EXISTS image_blacklist ( channel_id TEXT PRIMARY KEY )`,
		`CREATE TABLE IF NOT EXISTS users ( user_id TEXT PRIMARY KEY, last_interaction_time TIMESTAMP DEFAULT CURRENT_TIMESTAMP )`,
		`CREATE TABLE IF NOT EXISTS antiscam_servers ( server_id TEXT PRIMARY KEY )`,
		`CREATE TABLE IF NOT EXISTS antiscam_channels ( guild_id TEXT NOT NULL, parent_id TEXT NOT NULL, channel_id TEXT PRIMARY KEY, prompt_message_id TEXT, UNIQUE(guild_id, parent_id) )`,
		`CREATE TABLE IF NOT EXISTS image_descriptions ( image_url TEXT PRIMARY KEY, description TEXT, created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP )`,
		`CREATE TABLE IF NOT EXISTS user_cache ( user_id TEXT PRIMARY KEY, cache BLOB )`,
		`CREATE TABLE IF NOT EXISTS link_metadata ( url TEXT PRIMARY KEY, metadata BLOB, fetched_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP )`,
		`CREATE TABLE IF NOT EXISTS site_sessions ( site_id TEXT PRIMARY KEY, creator_id TEXT NOT NULL, discord_message_id TEXT, expires_at TEXT NOT NULL, data BLOB NOT NULL )`,
	}

	for i, migration := range migrations {
		_, err = DB.Exec(migration)
		if err != nil {
			return fmt.Errorf("failed to execute migration %d: %w", i+1, err)
		}
	}
	slog.Info("Database tables ensured")

	if err := migrateSiteSessionsSchema(); err != nil {
		return fmt.Errorf("failed to migrate site_sessions schema: %w", err)
	}

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

func migrateSiteSessionsSchema() error {
	rows, err := DB.Query(`PRAGMA table_info(site_sessions)`)
	if err != nil {
		return err
	}
	defer rows.Close()

	hasLegacyColumn := false
	for rows.Next() {
		var cid int
		var name string
		var colType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if strings.EqualFold(name, "interactive_until") {
			hasLegacyColumn = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !hasLegacyColumn {
		return nil
	}

	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	statements := []string{
		`ALTER TABLE site_sessions RENAME TO site_sessions_old`,
		`CREATE TABLE site_sessions ( site_id TEXT PRIMARY KEY, creator_id TEXT NOT NULL, discord_message_id TEXT, expires_at TEXT NOT NULL, data BLOB NOT NULL )`,
		`INSERT INTO site_sessions (site_id, creator_id, discord_message_id, expires_at, data)
		 SELECT site_id, creator_id, discord_message_id, expires_at, data FROM site_sessions_old`,
		`DROP TABLE site_sessions_old`,
	}
	for _, stmt := range statements {
		if _, err = tx.Exec(stmt); err != nil {
			return err
		}
	}
	err = tx.Commit()
	return err
}
