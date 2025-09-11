package db

import (
	"log/slog"

	"github.com/disgoorg/snowflake/v2"
)

// AddToWhitelist adds a user ID to the whitelist table.
func AddToWhitelist(id snowflake.ID) {
	_, err := DB.Exec("INSERT OR IGNORE INTO whitelist (user_id) VALUES (?)", id.String())
	if err != nil {
		slog.Error("failed to add user to whitelist", "err", err, slog.String("user_id", id.String()))
	}
}

// RemoveFromWhitelist removes a user ID from the whitelist table.
func RemoveFromWhitelist(id snowflake.ID) {
	_, err := DB.Exec("DELETE FROM whitelist WHERE user_id = ?", id.String())
	if err != nil {
		slog.Error("failed to remove user from whitelist", "err", err, slog.String("user_id", id.String()))
	}
}

// IsInWhitelist checks if a user ID exists in the whitelist table.
func IsInWhitelist(id snowflake.ID) bool {
	var count int
	err := DB.QueryRow("SELECT COUNT(*) FROM whitelist WHERE user_id = ?", id.String()).Scan(&count)
	if err != nil {
		slog.Error("failed to check if user is in whitelist", "err", err, slog.String("user_id", id.String()))
		// Return false on error, as per original logic
	}
	return count > 0
}
