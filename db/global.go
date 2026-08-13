package db

import (
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/disgoorg/snowflake/v2"
)

// GlobalSettings controls whether a guild is limited to one channel and who
// may edit its persona.
type GlobalSettings struct {
	MainChannelID    *snowflake.ID
	BlockPersonaEdit bool
}

// GetGlobalSettings returns the settings for a guild. A guild without a row
// has the default settings: global mode disabled and persona edits allowed.
func GetGlobalSettings(guildID snowflake.ID) (GlobalSettings, error) {
	var mainChannelKey sql.NullString
	var blockPersonaEdit bool
	err := DB.QueryRow(
		"SELECT main_channel_id, block_persona_edit FROM global_settings WHERE guild_id = ?",
		guildID.String(),
	).Scan(&mainChannelKey, &blockPersonaEdit)
	if err == sql.ErrNoRows {
		return GlobalSettings{}, nil
	}
	if err != nil {
		return GlobalSettings{}, fmt.Errorf("failed to get global settings: %w", err)
	}

	settings := GlobalSettings{BlockPersonaEdit: blockPersonaEdit}
	if mainChannelKey.Valid && mainChannelKey.String != "" {
		mainChannelID, err := snowflake.Parse(mainChannelKey.String)
		if err != nil {
			return GlobalSettings{}, fmt.Errorf("invalid main channel ID %q: %w", mainChannelKey.String, err)
		}
		settings.MainChannelID = &mainChannelID
	}
	return settings, nil
}

// SetGlobalSettings persists the settings for a guild.
func SetGlobalSettings(guildID snowflake.ID, settings GlobalSettings) error {
	mainChannelKey := ""
	if settings.MainChannelID != nil {
		mainChannelKey = settings.MainChannelID.String()
	}

	_, err := DB.Exec(
		`INSERT INTO global_settings (guild_id, main_channel_id, block_persona_edit)
		 VALUES (?, NULLIF(?, ''), ?)
		 ON CONFLICT(guild_id) DO UPDATE SET
		 main_channel_id = excluded.main_channel_id,
		 block_persona_edit = excluded.block_persona_edit`,
		guildID.String(), mainChannelKey, settings.BlockPersonaEdit,
	)
	if err != nil {
		slog.Error("failed to set global settings", "err", err, slog.String("guild_id", guildID.String()))
	}
	return err
}

// IsGlobalChannel reports whether a guild message is accepted by global mode.
// DMs are not passed here; a guild with global mode disabled accepts every
// channel, while an enabled guild accepts only its configured main channel.
func IsGlobalChannel(channelID, guildID snowflake.ID) bool {
	settings, err := GetGlobalSettings(guildID)
	if err != nil {
		slog.Error("failed to check global channel", "err", err, slog.String("guild_id", guildID.String()))
		return true
	}
	return settings.MainChannelID == nil || *settings.MainChannelID == channelID
}
