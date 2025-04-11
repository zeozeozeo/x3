package db

import (
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/disgoorg/snowflake/v2"
)

var (
	errInvalidUser = errors.New("invalid user ID provided for getting memories")
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
func GetMemories(userID snowflake.ID, limit int) []string {
	var memories []string
	if userID == 0 {
		slog.Warn("attempted to get memories for invalid user ID (0)")
		return memories
	}
	if limit == 0 {
		// default value
		limit = 35
	}

	query := "SELECT memory FROM memories WHERE user_id = ? ORDER BY created_at DESC"
	args := []any{userID.String()}
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := DB.Query(query, args...)
	if err != nil {
		slog.Error("failed to get memories",
			slog.Any("err", err),
			slog.String("user_id", userID.String()),
			slog.Int("limit_requested", limit),
		)
		return memories
	}
	defer rows.Close()

	for rows.Next() {
		var memory string
		if err := rows.Scan(&memory); err != nil {
			slog.Error("failed to scan memory", slog.Any("err", err), slog.String("user_id", userID.String()))
			continue
		}
		memories = append(memories, memory)
	}

	if err := rows.Err(); err != nil {
		slog.Error("rows iteration error while getting memories",
			slog.Any("err", err),
			slog.String("user_id", userID.String()),
		)
	}

	// Reverse the slice so older memories come first (as expected by LLM context)
	slices.Reverse(memories)

	return memories
}

// DeleteMemories removes all memories for a given user.
func DeleteMemories(userID snowflake.ID) error {
	if userID == 0 {
		return errInvalidUser
	}
	_, err := DB.Exec("DELETE FROM memories WHERE user_id = ?", userID.String())
	if err != nil {
		slog.Error("failed to delete memories", slog.Any("err", err), slog.String("user_id", userID.String()))
	}
	return err
}

// DeleteMemory removes a specific memory for a given user by index.
func DeleteMemory(userID snowflake.ID, idx int) error {
	if userID == 0 {
		return errInvalidUser
	}
	if idx < 0 {
		return fmt.Errorf("invalid index: index %d cannot be negative", idx)
	}

	query := `
		DELETE FROM memories
		WHERE rowid IN (
			SELECT rowid
			FROM memories
			WHERE user_id = ?
			ORDER BY created_at ASC, rowid ASC
			LIMIT 1 OFFSET ?
		)`

	result, err := DB.Exec(query, userID.String(), idx)
	if err != nil {
		slog.Error("failed to execute delete memory query",
			slog.Any("err", err),
			slog.String("user_id", userID.String()),
			slog.Int("index", idx),
		)
		return fmt.Errorf("failed to delete memory at index %d for user %s: %w", idx, userID, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		slog.Warn("failed to get rows affected after deleting memory",
			slog.Any("err", err),
			slog.String("user_id", userID.String()),
			slog.Int("index", idx),
		)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("memory not found: user %s, index %d", userID, idx)
	}

	slog.Debug("successfully deleted memory", slog.String("user_id", userID.String()), slog.Int("index", idx))
	return nil
}

func AddMemory(userID snowflake.ID, memory string) error {
	if userID == 0 {
		return errInvalidUser
	}
	memory = strings.TrimSpace(memory)
	if memory == "" {
		return nil
	}
	_, err := DB.Exec("INSERT INTO memories (user_id, memory) VALUES (?, ?)", userID.String(), memory)
	if err != nil {
		slog.Error("failed to add memory",
			slog.Any("err", err),
			slog.String("user_id", userID.String()),
			slog.String("memory", memory),
		)
	}
	return err
}
