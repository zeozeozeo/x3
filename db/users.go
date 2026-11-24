package db

import (
	"database/sql"
	"log/slog"
	"time"

	"github.com/disgoorg/snowflake/v2"
)

func SetInteractionTime(id snowflake.ID, time time.Time) error {
	return SetInteractionTimeKey(id.String(), time)
}

// SetInteractionTimeKey records the last interaction time under an arbitrary
// key, so platforms that are not Discord can prefix their user IDs.
func SetInteractionTimeKey(key string, time time.Time) error {
	_, err := DB.Exec(
		"INSERT OR REPLACE INTO users (user_id, last_interaction_time) VALUES (?, ?)",
		key, time,
	)
	if err != nil {
		slog.Error("failed to set last interaction time", "err", err, "key", key)
	}
	return err
}

func GetInteractionTime(id snowflake.ID) time.Time {
	return GetInteractionTimeKey(id.String())
}

// GetInteractionTimeKey reads the last interaction time stored under an
// arbitrary key, returning the zero time when there is none.
func GetInteractionTimeKey(key string) time.Time {
	var t time.Time
	err := DB.QueryRow("SELECT last_interaction_time FROM users WHERE user_id = ?", key).Scan(&t)
	if err != nil {
		if err != sql.ErrNoRows {
			slog.Error("failed to get last interaction time", "err", err, "key", key)
		}
		return time.Time{}
	}
	return t
}
