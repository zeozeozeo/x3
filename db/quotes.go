package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"slices"

	"github.com/disgoorg/snowflake/v2"
)

// Quote represents a saved quote.
type Quote struct {
	MessageID     snowflake.ID `json:"message_id"`
	Quoter        snowflake.ID `json:"quoter"`
	AuthorID      snowflake.ID `json:"author_id"`
	AuthorUser    string       `json:"author_user"`
	Channel       snowflake.ID `json:"channel"`
	Text          string       `json:"text"`
	AttachmentURL string       `json:"attachment_url"`
	Timestamp     time.Time    `json:"timestamp"`
}

// ServerStats holds server-specific data, currently just quotes.
type ServerStats struct {
	Quotes []Quote `json:"quotes"`
}

// QuoteExists checks if a quote already exists in the server stats.
func (s ServerStats) QuoteExists(quote Quote) (bool, int) {
	for i, q := range s.Quotes {
		if quote.MessageID == 0 { // For quotes created via /quote new
			if q.Channel == quote.Channel && q.AuthorID == quote.AuthorID && q.Text == quote.Text {
				return true, i
			}
		} else if q.MessageID == quote.MessageID || q.Timestamp.Equal(quote.Timestamp) { // For quotes created via reply
			return true, i
		}
	}
	return false, 0
}

// AddQuote adds a new quote to the server stats and returns the new quote number (1-based).
func (s *ServerStats) AddQuote(quote Quote) int {
	s.Quotes = append(s.Quotes, quote)
	return len(s.Quotes)
}

// RemoveQuote removes a quote by its index (0-based).
func (s *ServerStats) RemoveQuote(index int) {
	s.Quotes = slices.Delete(s.Quotes, index, index+1)
}

// unmarshalServerStats decodes JSON data into a ServerStats struct.
func unmarshalServerStats(data []byte) (ServerStats, error) {
	var stats ServerStats
	err := json.Unmarshal(data, &stats)
	return stats, err
}

// write saves the ServerStats to the database.
func (s ServerStats) Write(serverID snowflake.ID) error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	_, err = DB.Exec("INSERT OR REPLACE INTO server_stats (server_id, stats) VALUES (?, ?)", serverID.String(), data)
	return err
}

// getServerStats retrieves the ServerStats for a given server ID from the database.
func GetServerStats(serverID snowflake.ID) (ServerStats, error) {
	var data []byte
	err := DB.QueryRow("SELECT stats FROM server_stats WHERE server_id = ?", serverID.String()).Scan(&data)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// If no stats exist for the server, return empty stats.
			return ServerStats{}, nil
		}
		slog.Error("failed to get server stats from db", "err", err, slog.String("server_id", serverID.String()))
		return ServerStats{}, err
	}
	stats, err := unmarshalServerStats(data)
	if err != nil {
		slog.Error("failed to unmarshal server stats", "err", err, slog.String("server_id", serverID.String()))
		return ServerStats{}, err
	}
	return stats, nil
}
