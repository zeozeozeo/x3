package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/zeozeozeo/x3/imagedesc"
	_ "modernc.org/sqlite"
	_ "modernc.org/sqlite/vec"
)

// DB is the global database connection pool for the commands package.
var DB *sql.DB

// withConnectionPragmas appends _pragma parameters so every pooled
// connection gets the same settings (modernc.org/sqlite applies them at
// connect time). Plain file paths like "x3.db" and ":memory:" both work.
func withConnectionPragmas(dataSourceName string) string {
	pragmas := []string{
		"_pragma=busy_timeout%285000%29",
		"_pragma=journal_mode%28WAL%29",
		"_pragma=synchronous%28NORMAL%29",
	}
	sep := "?"
	if strings.Contains(dataSourceName, "?") {
		sep = "&"
	}
	return dataSourceName + sep + strings.Join(pragmas, "&")
}

// InitDB initializes the database connection and ensures tables are created.
func InitDB(dataSourceName string) error {
	var err error
	// NOTE: busy_timeout (and the other pragmas) are per-connection settings.
	// database/sql opens pooled connections lazily, so they must ride in the
	// DSN via _pragma to apply to every connection; otherwise a concurrently
	// opened connection has timeout 0 and fails instantly with SQLITE_BUSY
	// (this bit us with background worldmem writes racing the hot path).
	DB, err = sql.Open("sqlite", withConnectionPragmas(dataSourceName))
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
		`CREATE TABLE IF NOT EXISTS ab_test_stats ( default_model TEXT NOT NULL, ab_model TEXT NOT NULL, comparisons INTEGER NOT NULL DEFAULT 0, a_votes INTEGER NOT NULL DEFAULT 0, b_votes INTEGER NOT NULL DEFAULT 0, closed INTEGER NOT NULL DEFAULT 0, PRIMARY KEY (default_model, ab_model) )`,
		`CREATE TABLE IF NOT EXISTS server_stats ( server_id TEXT PRIMARY KEY, stats BLOB )`,
		`CREATE TABLE IF NOT EXISTS blacklist ( channel_id TEXT PRIMARY KEY )`,
		`CREATE TABLE IF NOT EXISTS global_settings ( guild_id TEXT PRIMARY KEY, main_channel_id TEXT, block_persona_edit INTEGER NOT NULL DEFAULT 0 )`,
		`CREATE TABLE IF NOT EXISTS image_blacklist ( channel_id TEXT PRIMARY KEY )`,
		`CREATE TABLE IF NOT EXISTS users ( user_id TEXT PRIMARY KEY, last_interaction_time TIMESTAMP DEFAULT CURRENT_TIMESTAMP )`,
		`CREATE TABLE IF NOT EXISTS antiscam_servers ( server_id TEXT PRIMARY KEY )`,
		`CREATE TABLE IF NOT EXISTS antiscam_channels ( guild_id TEXT NOT NULL, parent_id TEXT NOT NULL, channel_id TEXT PRIMARY KEY, prompt_message_id TEXT, UNIQUE(guild_id, parent_id) )`,
		`CREATE TABLE IF NOT EXISTS image_descriptions ( image_hash TEXT PRIMARY KEY, description TEXT, created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP )`,
		`CREATE TABLE IF NOT EXISTS user_cache ( user_id TEXT PRIMARY KEY, cache BLOB )`,
		`CREATE TABLE IF NOT EXISTS link_metadata ( url TEXT PRIMARY KEY, metadata BLOB, fetched_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP )`,
		`CREATE TABLE IF NOT EXISTS site_sessions ( site_id TEXT PRIMARY KEY, creator_id TEXT NOT NULL, discord_message_id TEXT, expires_at TEXT NOT NULL, data BLOB NOT NULL )`,
		`CREATE TABLE IF NOT EXISTS world_memories ( id INTEGER PRIMARY KEY AUTOINCREMENT, channel_key TEXT NOT NULL, text TEXT NOT NULL, tokens INTEGER NOT NULL DEFAULT 0, parent_id INTEGER REFERENCES world_memories(id), use_count INTEGER NOT NULL DEFAULT 0, last_used TIMESTAMP, created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP )`,
		`CREATE INDEX IF NOT EXISTS idx_world_memories_channel ON world_memories(channel_key, id)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS world_vec USING vec0(embedding float[768], channel TEXT PARTITION KEY)`,
		`CREATE TABLE IF NOT EXISTS world_index_meta ( key TEXT PRIMARY KEY, value TEXT NOT NULL )`,
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

	if err := migrateImageDescriptionsSchema(DB); err != nil {
		return fmt.Errorf("failed to migrate image_descriptions schema: %w", err)
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

// migrateImageDescriptionsSchema rekeys image_descriptions by a SHA-256 of
// the image URI (imagedesc.DescriptionKey) so multi-megabyte base64 data
// URIs never become row keys. Legacy rows keyed by a raw data: URI are
// dropped, not rehashed: they can only come from the Matrix image path
// before it could actually send images, so their descriptions were generated
// from a "failed to fetch" placeholder and are worthless. URL-keyed rows are
// rehashed in place. The database is vacuumed afterwards to reclaim the
// freed space.
func migrateImageDescriptionsSchema(conn *sql.DB) error {
	hasLegacyColumn := false
	rows, err := conn.Query(`PRAGMA table_info(image_descriptions)`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var cid int
		var name string
		var colType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultValue, &pk); err != nil {
			rows.Close()
			return err
		}
		if strings.EqualFold(name, "image_hash") {
			rows.Close()
			return nil // already migrated
		}
		if strings.EqualFold(name, "image_url") {
			hasLegacyColumn = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()
	if !hasLegacyColumn {
		return nil
	}

	// URLs (Discord CDN links etc.) are short, so collecting their rowids and
	// hashes up front is cheap; data: URIs are never read into memory at all.
	type rekey struct {
		rowid int64
		hash  string
	}
	var rekeys []rekey
	rows, err = conn.Query(`SELECT rowid, image_url FROM image_descriptions WHERE image_url NOT LIKE 'data:%'`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var rowid int64
		var imageURL string
		if err := rows.Scan(&rowid, &imageURL); err != nil {
			rows.Close()
			return err
		}
		rekeys = append(rekeys, rekey{rowid: rowid, hash: imagedesc.DescriptionKey(imageURL)})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var dropped int64
	result, err := tx.Exec(`DELETE FROM image_descriptions WHERE image_url LIKE 'data:%'`)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err == nil {
		dropped = n
	}
	for _, rk := range rekeys {
		if _, err = tx.Exec(`UPDATE image_descriptions SET image_url = ? WHERE rowid = ?`, rk.hash, rk.rowid); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(`ALTER TABLE image_descriptions RENAME COLUMN image_url TO image_hash`); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}

	slog.Info("migrated image_descriptions to hashed keys", "rekeyed", len(rekeys), "dropped", dropped)

	// compaction is best-effort: the schema is already correct without it
	if _, err := conn.Exec(`VACUUM`); err != nil {
		slog.Warn("failed to vacuum database after image_descriptions migration", "err", err)
	}
	return nil
}
