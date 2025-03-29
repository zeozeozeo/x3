package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/zeozeozeo/x3/llm"
)

// GlobalStats holds the global usage statistics for the bot.
type GlobalStats struct {
	Usage llm.Usage `json:"usage"`
	// total number of messages processed
	MessageCount    uint      `json:"message_count"`
	LastMessageTime time.Time `json:"last_message_time"`
}

// UnmarshalGlobalStats decodes JSON data into a GlobalStats struct.
func unmarshalGlobalStats(data []byte) (GlobalStats, error) {
	var stats GlobalStats
	err := json.Unmarshal(data, &stats)
	return stats, err
}

// Write saves the GlobalStats to the database.
func (s GlobalStats) Write() error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	// Assumes the table exists and has exactly one row.
	_, err = DB.Exec("UPDATE global_stats SET stats = ? WHERE EXISTS (SELECT 1 FROM global_stats)", data)
	return err
}

// GetGlobalStats retrieves the GlobalStats from the database.
func GetGlobalStats() (GlobalStats, error) {
	var data []byte
	err := DB.QueryRow("SELECT stats FROM global_stats").Scan(&data)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Should not happen if DB is initialized correctly, but return empty stats just in case.
			slog.Error("global_stats table is empty")
			return GlobalStats{}, nil
		}
		return GlobalStats{}, err
	}
	return unmarshalGlobalStats(data)
}

// UpdateGlobalStats retrieves, updates, and writes back the global stats.
func UpdateGlobalStats(usage llm.Usage) error {
	stats, err := GetGlobalStats()
	if err != nil {
		slog.Error("updateGlobalStats: failed to get global stats", slog.Any("err", err))
		return err
	}
	stats.Usage = stats.Usage.Add(usage)
	stats.MessageCount++
	stats.LastMessageTime = time.Now()
	if err := stats.Write(); err != nil {
		slog.Error("updateGlobalStats: failed to write global stats", slog.Any("err", err))
		return err
	}
	return nil
}
