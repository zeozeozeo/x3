package db

import (
	"database/sql"
	"log/slog"

	"github.com/disgoorg/snowflake/v2"
)

// WriteMessageInteractionPrompt caches the prompt used for a slash command interaction,
// associating it with the ID of the bot's response message.
func WriteMessageInteractionPrompt(messageID snowflake.ID, prompt string) error {
	_, err := DB.Exec("INSERT OR REPLACE INTO message_interaction_cache (message_id, prompt) VALUES (?, ?)", messageID.String(), prompt)
	if err != nil {
		slog.Error("failed to write message interaction prompt cache", "err", err, slog.String("message_id", messageID.String()))
	}
	return err // Return error even if logged
}

// GetMessageInteractionPrompt retrieves the original prompt associated with a message ID from the cache.
func GetMessageInteractionPrompt(id snowflake.ID) (string, error) {
	var prompt string
	err := DB.QueryRow("SELECT prompt FROM message_interaction_cache WHERE message_id = ?", id.String()).Scan(&prompt)
	// Errors (like sql.ErrNoRows) are expected and handled by the caller
	return prompt, err
}

// WriteMessageRenderedContent caches the raw assistant response when the visible
// Discord message differs because generated HTML was rendered to an attachment.
func WriteMessageRenderedContent(messageID snowflake.ID, content string) error {
	if DB == nil {
		return nil
	}
	_, err := DB.Exec("INSERT OR REPLACE INTO message_render_cache (message_id, content) VALUES (?, ?)", messageID.String(), content)
	if err != nil {
		slog.Error("failed to write rendered message cache", "err", err, slog.String("message_id", messageID.String()))
	}
	return err
}

// GetMessageRenderedContent retrieves the raw assistant response for a rendered message.
func GetMessageRenderedContent(id snowflake.ID) (string, error) {
	if DB == nil {
		return "", sql.ErrConnDone
	}
	var content string
	err := DB.QueryRow("SELECT content FROM message_render_cache WHERE message_id = ?", id.String()).Scan(&content)
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
