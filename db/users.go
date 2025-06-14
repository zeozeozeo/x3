package db

import (
	"log/slog"
	"time"

	"github.com/disgoorg/snowflake/v2"
)

func SetInteractionTime(id snowflake.ID, time time.Time) error {
	_, err := DB.Exec(
		"INSERT OR REPLACE INTO users (user_id, last_interaction_time) VALUES (?, ?)",
		id, time,
	)
	if err != nil {
		slog.Error("failed to set last interaction time", "err", err, "snowflake", id)
	}
	return err
}

func GetInteractionTime(id snowflake.ID) time.Time {
	var t time.Time
	err := DB.QueryRow("SELECT last_interaction_time FROM users WHERE user_id = ?", id).Scan(&t)
	if err != nil {
		slog.Error("failed to get last interaction time", "err", err, "snowflake", id)
		return time.Time{}
	}
	return t
}
