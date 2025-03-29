package db

import (
	"errors"
	"log/slog"
	"strings"

	"github.com/disgoorg/snowflake/v2"
)

// HandleMemories saves memories for a user to the database.
func HandleMemories(userID snowflake.ID, memories []string) error {
	if len(memories) == 0 || userID == 0 {
		return nil
	}

	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	// Defer rollback in case of errors during exec or commit
	defer tx.Rollback() // Rollback is a no-op if Commit succeeds

	stmt, err := tx.Prepare("INSERT OR REPLACE INTO memories (user_id, memory) VALUES (?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, memory := range memories {
		memory = strings.TrimSpace(memory) // Ensure no leading/trailing whitespace
		if memory == "" {
			continue
		}
		_, err := stmt.Exec(userID.String(), memory)
		if err != nil {
			// Rollback is handled by defer
			return err
		}
	}

	// Commit the transaction
	return tx.Commit()
}

// GetMemories retrieves the latest memories for a user from the database.
func GetMemories(userID snowflake.ID) []string {
	var memories []string
	if userID == 0 {
		return memories // Return empty slice if userID is invalid
	}

	rows, err := DB.Query(
		"SELECT memory FROM memories WHERE user_id = ? ORDER BY created_at DESC LIMIT 35",
		userID.String(),
	)
	if err != nil {
		slog.Error("failed to get memories", slog.Any("err", err), slog.String("user_id", userID.String()))
		return memories // Return empty slice on error
	}
	defer rows.Close()

	for rows.Next() {
		var memory string
		if err := rows.Scan(&memory); err != nil {
			slog.Error("failed to scan memory", slog.Any("err", err))
			continue // Skip this row on scan error
		}
		memories = append(memories, memory)
	}

	if err := rows.Err(); err != nil {
		slog.Error("rows iteration error while getting memories", slog.Any("err", err))
	}

	// Reverse the slice so older memories come first (as expected by LLM context)
	// slices.Reverse(memories) // Assuming Go 1.21+, otherwise implement manually

	// Manual reverse for compatibility
	for i, j := 0, len(memories)-1; i < j; i, j = i+1, j-1 {
		memories[i], memories[j] = memories[j], memories[i]
	}

	return memories
}

// DeleteMemories removes all memories for a given user.
func DeleteMemories(userID snowflake.ID) error {
	if userID == 0 {
		return errors.New("invalid user ID provided for deleting memories")
	}
	_, err := DB.Exec("DELETE FROM memories WHERE user_id = ?", userID.String())
	if err != nil {
		slog.Error("failed to delete memories", slog.Any("err", err), slog.String("user_id", userID.String()))
	}
	return err
}
