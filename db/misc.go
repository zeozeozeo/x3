package db

import (
	"database/sql"
	"log/slog"

	"github.com/disgoorg/snowflake/v2"
)

// WriteMessageInteractionPrompt caches the prompt used for a slash command interaction,
// associating it with the ID of the bot's response message.
func WriteMessageInteractionPrompt(messageID snowflake.ID, prompt string) error {
	return WriteMessageInteractionPromptKey(messageID.String(), prompt)
}

func WriteMessageInteractionPromptKey(messageKey string, prompt string) error {
	_, err := DB.Exec("INSERT OR REPLACE INTO message_interaction_cache (message_id, prompt) VALUES (?, ?)", messageKey, prompt)
	if err != nil {
		slog.Error("failed to write message interaction prompt cache", "err", err, slog.String("message_id", messageKey))
	}
	return err // Return error even if logged
}

// GetMessageInteractionPrompt retrieves the original prompt associated with a message ID from the cache.
func GetMessageInteractionPrompt(id snowflake.ID) (string, error) {
	return GetMessageInteractionPromptKey(id.String())
}

func GetMessageInteractionPromptKey(key string) (string, error) {
	var prompt string
	err := DB.QueryRow("SELECT prompt FROM message_interaction_cache WHERE message_id = ?", key).Scan(&prompt)
	// Errors (like sql.ErrNoRows) are expected and handled by the caller
	return prompt, err
}

// WriteMessageRenderedContent caches the raw assistant response when the visible
// Discord message differs because generated HTML was rendered to an attachment.
func WriteMessageRenderedContent(messageID snowflake.ID, content string) error {
	return WriteMessageRenderedContentKey(messageID.String(), content)
}

func WriteMessageRenderedContentKey(messageKey string, content string) error {
	if DB == nil {
		return nil
	}
	_, err := DB.Exec("INSERT OR REPLACE INTO message_render_cache (message_id, content) VALUES (?, ?)", messageKey, content)
	if err != nil {
		slog.Error("failed to write rendered message cache", "err", err, slog.String("message_id", messageKey))
	}
	return err
}

// GetMessageRenderedContent retrieves the raw assistant response for a rendered message.
func GetMessageRenderedContent(id snowflake.ID) (string, error) {
	return GetMessageRenderedContentKey(id.String())
}

func GetMessageRenderedContentKey(key string) (string, error) {
	if DB == nil {
		return "", sql.ErrConnDone
	}
	var content string
	err := DB.QueryRow("SELECT content FROM message_render_cache WHERE message_id = ?", key).Scan(&content)
	return content, err
}

// IsAntiscamEnabled checks if a server has the antiscam feature enabled.
func IsAntiscamEnabled(serverID snowflake.ID) bool {
	var id string
	err := DB.QueryRow("SELECT server_id FROM antiscam_servers WHERE server_id = ?", serverID.String()).Scan(&id)
	return err == nil
}

// SetAntiscamEnabled toggles the antiscam feature for a server.
func SetAntiscamEnabled(serverID snowflake.ID, enabled bool) error {
	if enabled {
		_, err := DB.Exec("INSERT OR IGNORE INTO antiscam_servers (server_id) VALUES (?)", serverID.String())
		return err
	}
	_, err := DB.Exec("DELETE FROM antiscam_servers WHERE server_id = ?", serverID.String())
	return err
}

type AntiscamChannel struct {
	GuildID         snowflake.ID
	ParentID        snowflake.ID
	ChannelID       snowflake.ID
	PromptMessageID snowflake.ID
}

func UpsertAntiscamChannel(guildID, parentID, channelID snowflake.ID, promptMessageID *snowflake.ID) error {
	prompt := ""
	if promptMessageID != nil {
		prompt = promptMessageID.String()
	}
	_, err := DB.Exec(
		`INSERT INTO antiscam_channels (guild_id, parent_id, channel_id, prompt_message_id)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(guild_id, parent_id) DO UPDATE SET channel_id = excluded.channel_id, prompt_message_id = excluded.prompt_message_id`,
		guildID.String(),
		parentID.String(),
		channelID.String(),
		prompt,
	)
	return err
}

func ReplaceAntiscamChannels(guildID, channelID snowflake.ID, promptMessageID *snowflake.ID) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM antiscam_channels WHERE guild_id = ?", guildID.String()); err != nil {
		return err
	}

	prompt := ""
	if promptMessageID != nil {
		prompt = promptMessageID.String()
	}
	if _, err := tx.Exec(
		`INSERT INTO antiscam_channels (guild_id, parent_id, channel_id, prompt_message_id) VALUES (?, ?, ?, ?)`,
		guildID.String(),
		"0",
		channelID.String(),
		prompt,
	); err != nil {
		return err
	}

	return tx.Commit()
}

func SetAntiscamPromptMessage(channelID, messageID snowflake.ID) error {
	_, err := DB.Exec("UPDATE antiscam_channels SET prompt_message_id = ? WHERE channel_id = ?", messageID.String(), channelID.String())
	return err
}

func GetAntiscamChannels(guildID snowflake.ID) ([]AntiscamChannel, error) {
	rows, err := DB.Query("SELECT guild_id, parent_id, channel_id, COALESCE(prompt_message_id, '') FROM antiscam_channels WHERE guild_id = ?", guildID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var channels []AntiscamChannel
	for rows.Next() {
		var guildKey, parentKey, channelKey, promptKey string
		if err := rows.Scan(&guildKey, &parentKey, &channelKey, &promptKey); err != nil {
			return nil, err
		}
		guild, err := snowflake.Parse(guildKey)
		if err != nil {
			continue
		}
		parent, err := snowflake.Parse(parentKey)
		if err != nil {
			continue
		}
		channel, err := snowflake.Parse(channelKey)
		if err != nil {
			continue
		}
		var prompt snowflake.ID
		if promptKey != "" {
			prompt, _ = snowflake.Parse(promptKey)
		}
		channels = append(channels, AntiscamChannel{
			GuildID:         guild,
			ParentID:        parent,
			ChannelID:       channel,
			PromptMessageID: prompt,
		})
	}
	return channels, rows.Err()
}

func IsAntiscamChannel(channelID snowflake.ID) bool {
	var id string
	err := DB.QueryRow("SELECT channel_id FROM antiscam_channels WHERE channel_id = ?", channelID.String()).Scan(&id)
	return err == nil
}

func DeleteAntiscamChannel(channelID snowflake.ID) error {
	_, err := DB.Exec("DELETE FROM antiscam_channels WHERE channel_id = ?", channelID.String())
	return err
}
