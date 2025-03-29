package db

import (
	"log/slog"

	"github.com/disgoorg/snowflake/v2"
)

// IsChannelInBlacklist checks if a channel ID exists in the blacklist table.
func IsChannelInBlacklist(id snowflake.ID) bool {
	var count int
	err := DB.QueryRow("SELECT COUNT(*) FROM blacklist WHERE channel_id = ?", id.String()).Scan(&count)
	if err != nil {
		slog.Error("failed to check if channel is in blacklist", slog.Any("err", err), slog.String("channel_id", id.String()))
		// Assume not blacklisted on error? Or return error? Original code assumed false.
	}
	return count > 0
}

// AddChannelToBlacklist adds a channel ID to the blacklist table.
func AddChannelToBlacklist(id snowflake.ID) error {
	_, err := DB.Exec("INSERT OR IGNORE INTO blacklist (channel_id) VALUES (?)", id.String())
	if err != nil {
		slog.Error("failed to add channel to blacklist", slog.Any("err", err), slog.String("channel_id", id.String()))
	}
	return err
}

// RemoveChannelFromBlacklist removes a channel ID from the blacklist table.
func RemoveChannelFromBlacklist(id snowflake.ID) error {
	_, err := DB.Exec("DELETE FROM blacklist WHERE channel_id = ?", id.String())
	if err != nil {
		slog.Error("failed to remove channel from blacklist", slog.Any("err", err), slog.String("channel_id", id.String()))
	}
	return err
}
