package db

import (
	"log/slog"

	"github.com/disgoorg/snowflake/v2"
)

// WriteMessageInteractionPrompt caches the prompt used for a slash command interaction,
// associating it with the ID of the bot's response message.
func WriteMessageInteractionPrompt(messageID snowflake.ID, prompt string) error {
	_, err := DB.Exec("INSERT OR REPLACE INTO message_interaction_cache (message_id, prompt) VALUES (?, ?)", messageID.String(), prompt)
	if err != nil {
		slog.Error("failed to write message interaction prompt cache", slog.Any("err", err), slog.String("message_id", messageID.String()))
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
