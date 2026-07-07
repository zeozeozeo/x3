package db

import (
	"database/sql"
	"log/slog"
	"time"
)

type SiteSessionRecord struct {
	SiteID           string
	CreatorID        string
	DiscordMessageID string
	ExpiresAt        time.Time
	Data             []byte
}

func WriteSiteSession(record SiteSessionRecord) error {
	if DB == nil {
		return nil
	}
	_, err := DB.Exec(
		`INSERT OR REPLACE INTO site_sessions
		(site_id, creator_id, discord_message_id, expires_at, data)
		VALUES (?, ?, ?, ?, ?)`,
		record.SiteID,
		record.CreatorID,
		record.DiscordMessageID,
		record.ExpiresAt.UTC().Format(time.RFC3339Nano),
		record.Data,
	)
	if err != nil {
		slog.Error("failed to write site session", "err", err, "site_id", record.SiteID)
	}
	return err
}

func DeleteSiteSession(siteID string) error {
	if DB == nil {
		return nil
	}
	_, err := DB.Exec("DELETE FROM site_sessions WHERE site_id = ?", siteID)
	if err != nil {
		slog.Error("failed to delete site session", "err", err, "site_id", siteID)
	}
	return err
}

func ListSiteSessions() ([]SiteSessionRecord, error) {
	if DB == nil {
		return nil, sql.ErrConnDone
	}
	rows, err := DB.Query(`SELECT site_id, creator_id, COALESCE(discord_message_id, ''), expires_at, data FROM site_sessions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []SiteSessionRecord
	for rows.Next() {
		var record SiteSessionRecord
		var expiresAt string
		if err := rows.Scan(&record.SiteID, &record.CreatorID, &record.DiscordMessageID, &expiresAt, &record.Data); err != nil {
			return nil, err
		}
		record.ExpiresAt, _ = time.Parse(time.RFC3339Nano, expiresAt)
		records = append(records, record)
	}
	return records, rows.Err()
}

func DeleteExpiredSiteSessions(now time.Time) error {
	if DB == nil {
		return nil
	}
	_, err := DB.Exec("DELETE FROM site_sessions WHERE expires_at <= ?", now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		slog.Error("failed to delete expired site sessions", "err", err)
	}
	return err
}
