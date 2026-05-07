package db

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"time"
)

type LinkMetadataCacheEntry struct {
	URL       string    `json:"url"`
	Metadata  string    `json:"metadata,omitempty"`
	Error     string    `json:"error,omitempty"`
	FetchedAt time.Time `json:"fetched_at"`
}

func GetLinkMetadata(url string) (LinkMetadataCacheEntry, error) {
	if DB == nil {
		return LinkMetadataCacheEntry{}, sql.ErrConnDone
	}
	var data []byte
	err := DB.QueryRow("SELECT metadata FROM link_metadata WHERE url = ?", url).Scan(&data)
	if err != nil {
		return LinkMetadataCacheEntry{}, err
	}
	var entry LinkMetadataCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return LinkMetadataCacheEntry{}, err
	}
	return entry, nil
}

func WriteLinkMetadata(entry LinkMetadataCacheEntry) error {
	if DB == nil {
		return nil
	}
	if entry.FetchedAt.IsZero() {
		entry.FetchedAt = time.Now().UTC()
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	_, err = DB.Exec(
		"INSERT OR REPLACE INTO link_metadata (url, metadata, fetched_at) VALUES (?, ?, ?)",
		entry.URL,
		data,
		entry.FetchedAt,
	)
	if err != nil {
		slog.Error("failed to write link metadata cache", "err", err, "url", entry.URL)
	}
	return err
}
