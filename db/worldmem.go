package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// WorldEntry is one stored world fact. Embeddings live in the world_vec
// virtual table keyed by the same rowid; every access is scoped by
// ChannelKey so memories never leak across channels.
type WorldEntry struct {
	ID        int64      `json:"id"`
	Text      string     `json:"text"`
	Tokens    int        `json:"tokens,omitempty"`
	ParentID  *int64     `json:"parent_id,omitempty"`
	UseCount  int        `json:"use_count,omitempty"`
	LastUsed  *time.Time `json:"last_used,omitempty"`
	CreatedAt time.Time  `json:"created_at,omitempty"`
}

// worldVectorText serializes a float vector for sqlite-vec MATCH queries.
func worldVectorText(vec []float32) string {
	var sb strings.Builder
	sb.WriteByte('[')
	for i, v := range vec {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, "%g", v)
	}
	sb.WriteByte(']')
	return sb.String()
}

// InsertWorldEntry stores one fact chunk and its embedding. The vec rowid
// mirrors the world_memories id. Vectors with a wrong dimension are rejected
// (changing the embedding model requires rebuilding the index).
func InsertWorldEntry(channelKey, text string, vec []float32, tokens int, parentID *int64, vecDim int) (int64, error) {
	if strings.TrimSpace(channelKey) == "" || strings.TrimSpace(text) == "" {
		return 0, fmt.Errorf("channel key and text are required")
	}
	if len(vec) != vecDim {
		return 0, fmt.Errorf("embedding dim %d != index dim %d", len(vec), vecDim)
	}
	tx, err := DB.Begin()
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	res, err := tx.Exec(
		`INSERT INTO world_memories (channel_key, text, tokens, parent_id) VALUES (?, ?, ?, ?)`,
		channelKey, text, tokens, parentID,
	)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if _, err = tx.Exec(
		`INSERT INTO world_vec(rowid, embedding, channel) VALUES (?, ?, ?)`,
		id, worldVectorText(vec), channelKey,
	); err != nil {
		return 0, err
	}
	err = tx.Commit()
	return id, err
}

// WorldSimilarityHit pairs an entry with its vector distance.
type WorldSimilarityHit struct {
	Entry    WorldEntry
	Distance float64
}

// SearchWorldSimilar returns up to k entries from this channel only, ordered
// by ascending vector distance.
func SearchWorldSimilar(channelKey string, queryVec []float32, k int) ([]WorldSimilarityHit, error) {
	if strings.TrimSpace(channelKey) == "" {
		return nil, fmt.Errorf("channel key is required")
	}
	if k <= 0 {
		k = 8
	}
	rows, err := DB.Query(
		`SELECT rowid, distance FROM world_vec WHERE embedding MATCH ? AND k = ? AND channel = ?`,
		worldVectorText(queryVec), k, channelKey,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type idDist struct {
		id   int64
		dist float64
	}
	var ids []idDist
	for rows.Next() {
		var hit idDist
		if err := rows.Scan(&hit.id, &hit.dist); err != nil {
			return nil, err
		}
		ids = append(ids, hit)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}

	// Fetch texts in distance order, verifying channel ownership row by row.
	hits := make([]WorldSimilarityHit, 0, len(ids))
	for _, item := range ids {
		var e WorldEntry
		var parent sql.NullInt64
		var lastUsed sql.NullTime
		var created sql.NullTime
		err := DB.QueryRow(
			`SELECT id, text, tokens, parent_id, use_count, last_used, created_at FROM world_memories WHERE id = ? AND channel_key = ?`,
			item.id, channelKey,
		).Scan(&e.ID, &e.Text, &e.Tokens, &parent, &e.UseCount, &lastUsed, &created)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return nil, err
		}
		if parent.Valid {
			v := parent.Int64
			e.ParentID = &v
		}
		if lastUsed.Valid {
			v := lastUsed.Time
			e.LastUsed = &v
		}
		if created.Valid {
			e.CreatedAt = created.Time
		}
		hits = append(hits, WorldSimilarityHit{Entry: e, Distance: item.dist})
	}
	return hits, nil
}

// GetWorldEntries lists all entries for a channel, oldest first.
func GetWorldEntries(channelKey string) ([]WorldEntry, error) {
	rows, err := DB.Query(
		`SELECT id, text, tokens, parent_id, use_count, last_used, created_at FROM world_memories WHERE channel_key = ? ORDER BY id ASC`,
		channelKey,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanWorldEntries(rows)
}

// GetRecentWorldEntries returns the n newest entries for a channel.
func GetRecentWorldEntries(channelKey string, n int) ([]WorldEntry, error) {
	rows, err := DB.Query(
		`SELECT id, text, tokens, parent_id, use_count, last_used, created_at FROM world_memories WHERE channel_key = ? ORDER BY id DESC LIMIT ?`,
		channelKey, n,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries, err := scanWorldEntries(rows)
	if err != nil {
		return nil, err
	}
	// Restore chronological order.
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	return entries, nil
}

// CountWorldEntries counts entries for a channel.
func CountWorldEntries(channelKey string) (int, error) {
	var n int
	err := DB.QueryRow(`SELECT COUNT(*) FROM world_memories WHERE channel_key = ?`, channelKey).Scan(&n)
	return n, err
}

func scanWorldEntries(rows *sql.Rows) ([]WorldEntry, error) {
	var out []WorldEntry
	for rows.Next() {
		var e WorldEntry
		var parent sql.NullInt64
		var lastUsed sql.NullTime
		var created sql.NullTime
		if err := rows.Scan(&e.ID, &e.Text, &e.Tokens, &parent, &e.UseCount, &lastUsed, &created); err != nil {
			return nil, err
		}
		if parent.Valid {
			v := parent.Int64
			e.ParentID = &v
		}
		if lastUsed.Valid {
			v := lastUsed.Time
			e.LastUsed = &v
		}
		if created.Valid {
			e.CreatedAt = created.Time
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// TouchWorldEntries records usage for retrieved entries.
func TouchWorldEntries(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	for _, id := range ids {
		if _, err := DB.Exec(
			`UPDATE world_memories SET use_count = use_count + 1, last_used = CURRENT_TIMESTAMP WHERE id = ?`,
			id,
		); err != nil {
			return err
		}
	}
	return nil
}

// ClearWorldEntries deletes all entries (and vectors) for a channel.
func ClearWorldEntries(channelKey string) (int, error) {
	var ids []int64
	rows, err := DB.Query(`SELECT id FROM world_memories WHERE channel_key = ?`, channelKey)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	tx, err := DB.Begin()
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	for _, id := range ids {
		if _, err = tx.Exec(`DELETE FROM world_vec WHERE rowid = ?`, id); err != nil {
			return 0, err
		}
	}
	res, err := tx.Exec(`DELETE FROM world_memories WHERE channel_key = ?`, channelKey)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	err = tx.Commit()
	return int(n), err
}

// worldExport is the JSON format for /worldmem export and import.
type worldExport struct {
	Version int          `json:"version"`
	Entries []WorldEntry `json:"entries"`
}

const worldExportVersion = 1

// ExportWorldEntries serializes a channel's entries (texts only, no vectors;
// vectors are re-embedded on import).
func ExportWorldEntries(channelKey string) ([]byte, error) {
	entries, err := GetWorldEntries(channelKey)
	if err != nil {
		return nil, err
	}
	if entries == nil {
		entries = []WorldEntry{}
	}
	return json.MarshalIndent(worldExport{Version: worldExportVersion, Entries: entries}, "", "  ")
}

// ParseWorldExport validates an imported archive.
func ParseWorldExport(data []byte) ([]WorldEntry, error) {
	var exp worldExport
	if err := json.Unmarshal(data, &exp); err != nil {
		return nil, fmt.Errorf("invalid world archive JSON: %w", err)
	}
	if exp.Version != worldExportVersion {
		return nil, fmt.Errorf("unsupported world archive version %d", exp.Version)
	}
	out := make([]WorldEntry, 0, len(exp.Entries))
	for _, e := range exp.Entries {
		if strings.TrimSpace(e.Text) == "" {
			continue
		}
		out = append(out, WorldEntry{Text: strings.TrimSpace(e.Text)})
	}
	return out, nil
}
