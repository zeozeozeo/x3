package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// worldWriteMu serializes worldmem write transactions. Background stores run
// outside the channel interaction lock, so without this they race the hot
// path's cache/stats writes and hit SQLITE_BUSY.
var worldWriteMu sync.Mutex

// isBusyError reports SQLite lock contention.
func isBusyError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "sqlite_busy") || strings.Contains(msg, "database is locked")
}

// withWorldWrite runs fn serialized with retries on lock contention.
func withWorldWrite(fn func() error) error {
	worldWriteMu.Lock()
	defer worldWriteMu.Unlock()
	var err error
	for attempt := range 6 {
		if err = fn(); err == nil || !isBusyError(err) {
			return err
		}
		time.Sleep(time.Duration(25<<attempt) * time.Millisecond)
	}
	return err
}

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
	CreatedAt time.Time  `json:"created_at"`
	// ChannelKey is the owning channel/room. It is not serialized: exports are
	// already scoped to one channel, and imports re-target their own key.
	ChannelKey string `json:"-"`
}

const worldEntryColumns = `id, channel_key, text, tokens, parent_id, use_count, last_used, created_at`

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
func InsertWorldEntry(channelKey, text string, vec []float32, tokens int, parentID *int64, vecDim int) (id int64, err error) {
	err = withWorldWrite(func() error {
		var innerErr error
		id, innerErr = insertWorldEntry(channelKey, text, vec, tokens, parentID, vecDim)
		return innerErr
	})
	return id, err
}

func insertWorldEntry(channelKey, text string, vec []float32, tokens int, parentID *int64, vecDim int) (int64, error) {
	if strings.TrimSpace(channelKey) == "" || strings.TrimSpace(text) == "" {
		return 0, fmt.Errorf("channel key and text are required")
	}
	if len(vec) != vecDim {
		return 0, fmt.Errorf("embedding dim %d != index dim %d", len(vec), vecDim)
	}
	if err := ensureWorldVecTable(vecDim); err != nil {
		return 0, fmt.Errorf("world vec table: %w", err)
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
	if len(queryVec) == 0 {
		return nil, fmt.Errorf("query vector is required")
	}
	if err := ensureWorldVecTable(len(queryVec)); err != nil {
		return nil, err
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
			`SELECT `+worldEntryColumns+` FROM world_memories WHERE id = ? AND channel_key = ?`,
			item.id, channelKey,
		).Scan(&e.ID, &e.ChannelKey, &e.Text, &e.Tokens, &parent, &e.UseCount, &lastUsed, &created)
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
		`SELECT `+worldEntryColumns+` FROM world_memories WHERE channel_key = ? ORDER BY id ASC`,
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
		`SELECT `+worldEntryColumns+` FROM world_memories WHERE channel_key = ? ORDER BY id DESC LIMIT ?`,
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

// CountAllWorldEntries counts entries across every channel.
func CountAllWorldEntries() (int, error) {
	var n int
	err := DB.QueryRow(`SELECT COUNT(*) FROM world_memories`).Scan(&n)
	return n, err
}

// CountWorldEntriesWithoutVectors counts stored facts that currently have no
// vector, so callers can tell "nothing to repair" from "reindex needed"
// without loading the facts themselves.
func CountWorldEntriesWithoutVectors() (int, error) {
	if !worldVecShadowTableExists() {
		return CountAllWorldEntries()
	}
	var n int
	err := DB.QueryRow(
		`SELECT COUNT(*) FROM world_memories WHERE id NOT IN (SELECT rowid FROM world_vec_rowids)`,
	).Scan(&n)
	return n, err
}

// WorldIndexMeta records which embedding model produced the vectors currently
// stored in world_vec. Knowing it lets x3 detect an embedding-model change
// without spending an embedding request on every startup.
type WorldIndexMeta struct {
	Model string
	Dim   int
}

const (
	worldIndexMetaModelKey = "embedding_model"
	worldIndexMetaDimKey   = "embedding_dim"
)

// GetWorldIndexMeta returns the recorded embedding model and dimension. ok is
// false when nothing has been recorded yet, which is the case for a database
// written before this bookkeeping existed.
func GetWorldIndexMeta() (m WorldIndexMeta, ok bool, err error) {
	rows, err := DB.Query(
		`SELECT key, value FROM world_index_meta WHERE key IN (?, ?)`,
		worldIndexMetaModelKey, worldIndexMetaDimKey,
	)
	if err != nil {
		return WorldIndexMeta{}, false, err
	}
	defer rows.Close()

	seen := 0
	for rows.Next() {
		var k, v string
		if err = rows.Scan(&k, &v); err != nil {
			return WorldIndexMeta{}, false, err
		}
		switch k {
		case worldIndexMetaModelKey:
			m.Model = v
			seen++
		case worldIndexMetaDimKey:
			if n, convErr := strconv.Atoi(v); convErr == nil {
				m.Dim = n
				seen++
			}
		}
	}
	if err = rows.Err(); err != nil {
		return WorldIndexMeta{}, false, err
	}
	return m, seen == 2 && m.Model != "" && m.Dim > 0, nil
}

// SetWorldIndexMeta records the embedding model and dimension behind world_vec.
func SetWorldIndexMeta(m WorldIndexMeta) error {
	if strings.TrimSpace(m.Model) == "" || m.Dim <= 0 {
		return fmt.Errorf("invalid world index metadata")
	}
	return withWorldWrite(func() error {
		tx, err := DB.Begin()
		if err != nil {
			return err
		}
		defer func() {
			if err != nil {
				_ = tx.Rollback()
			}
		}()
		for _, kv := range [][2]string{
			{worldIndexMetaModelKey, m.Model},
			{worldIndexMetaDimKey, strconv.Itoa(m.Dim)},
		} {
			if _, err = tx.Exec(
				`INSERT INTO world_index_meta(key, value) VALUES (?, ?)
				 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
				kv[0], kv[1],
			); err != nil {
				return err
			}
		}
		err = tx.Commit()
		return err
	})
}

func scanWorldEntries(rows *sql.Rows) ([]WorldEntry, error) {
	var out []WorldEntry
	for rows.Next() {
		var e WorldEntry
		var parent sql.NullInt64
		var lastUsed sql.NullTime
		var created sql.NullTime
		if err := rows.Scan(&e.ID, &e.ChannelKey, &e.Text, &e.Tokens, &parent, &e.UseCount, &lastUsed, &created); err != nil {
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
func ClearWorldEntries(channelKey string) (n int, err error) {
	err = withWorldWrite(func() error {
		var innerErr error
		n, innerErr = clearWorldEntries(channelKey)
		return innerErr
	})
	return n, err
}

func clearWorldEntries(channelKey string) (int, error) {
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
			if !strings.Contains(strings.ToLower(err.Error()), "no such table") {
				return 0, err
			}
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

var worldVecSchemaMu sync.Mutex

// ensureWorldVecTable makes sure the world_vec virtual table uses the requested
// embedding dimension.
//
// It never silently discards stored vectors. A vec0 column has a fixed width,
// so a mismatched dimension used to mean "drop the whole index", which turned a
// configuration mistake into memories that could never be retrieved again,
// with no error anywhere. Instead the mismatch is reported; worldmem.Reindex
// rebuilds the table and re-embeds every fact.
func ensureWorldVecTable(dim int) error {
	if dim <= 0 {
		return fmt.Errorf("invalid embedding dimension %d", dim)
	}
	worldVecSchemaMu.Lock()
	defer worldVecSchemaMu.Unlock()

	cur := currentWorldVecDim()
	if cur == dim {
		return nil
	}
	if cur != 0 && worldVecRowCount() > 0 {
		return fmt.Errorf(
			"world_vec holds %d-dim vectors but the configured embedding model produces %d; reindex required (worldmem.Reindex)",
			cur, dim)
	}
	return recreateWorldVecTableLocked(dim)
}

// CurrentWorldVecDim reports the embedding width of the world_vec table, or 0
// when the table does not exist yet.
func CurrentWorldVecDim() int { return currentWorldVecDim() }

// RebuildWorldVecTable drops and recreates world_vec at dim. It is destructive:
// every stored vector is discarded, so callers must re-embed stored facts
// afterwards. worldmem.Reindex does that; nothing else should call this.
func RebuildWorldVecTable(dim int) error {
	if dim <= 0 {
		return fmt.Errorf("invalid embedding dimension %d", dim)
	}
	worldVecSchemaMu.Lock()
	defer worldVecSchemaMu.Unlock()
	return recreateWorldVecTableLocked(dim)
}

func recreateWorldVecTableLocked(dim int) error {
	if _, err := DB.Exec(`DROP TABLE IF EXISTS world_vec`); err != nil {
		return fmt.Errorf("drop world_vec: %w", err)
	}
	if _, err := DB.Exec(fmt.Sprintf(`CREATE VIRTUAL TABLE world_vec USING vec0(embedding float[%d], channel TEXT PARTITION KEY)`, dim)); err != nil {
		return fmt.Errorf("create world_vec[%d]: %w", dim, err)
	}
	return nil
}

// worldVecRowCount reports how many vectors are stored. It returns 0 when the
// vector table (or its sqlite-vec shadow table) does not exist yet.
func worldVecRowCount() int {
	var n int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM world_vec_rowids`).Scan(&n); err != nil {
		return 0
	}
	return n
}

// WorldEntriesMissingVectors returns up to limit stored facts that have no
// vector, oldest first. Facts like these are what /worldmem import and an
// embedding-model change leave behind; re-embedding them restores retrieval.
func WorldEntriesMissingVectors(limit int) ([]WorldEntry, error) {
	if limit <= 0 {
		limit = 64
	}
	q := `SELECT ` + worldEntryColumns + ` FROM world_memories ORDER BY id ASC LIMIT ?`
	if worldVecShadowTableExists() {
		q = `SELECT ` + worldEntryColumns + ` FROM world_memories ` +
			`WHERE id NOT IN (SELECT rowid FROM world_vec_rowids) ORDER BY id ASC LIMIT ?`
	}
	rows, err := DB.Query(q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanWorldEntries(rows)
}

func worldVecShadowTableExists() bool {
	var n int
	err := DB.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type IN ('table','view') AND name = 'world_vec_rowids'`,
	).Scan(&n)
	return err == nil && n > 0
}

// UpsertWorldVector attaches vec to an existing world_memories row, replacing
// any vector already stored for it.
func UpsertWorldVector(id int64, channelKey string, vec []float32, vecDim int) error {
	if id == 0 || strings.TrimSpace(channelKey) == "" {
		return fmt.Errorf("row id and channel key are required")
	}
	if len(vec) != vecDim {
		return fmt.Errorf("embedding dim %d != index dim %d", len(vec), vecDim)
	}
	return withWorldWrite(func() error {
		if err := ensureWorldVecTable(vecDim); err != nil {
			return err
		}
		tx, err := DB.Begin()
		if err != nil {
			return err
		}
		defer func() {
			if err != nil {
				_ = tx.Rollback()
			}
		}()
		if _, err = tx.Exec(`DELETE FROM world_vec WHERE rowid = ?`, id); err != nil {
			return err
		}
		if _, err = tx.Exec(
			`INSERT INTO world_vec(rowid, embedding, channel) VALUES (?, ?, ?)`,
			id, worldVectorText(vec), channelKey,
		); err != nil {
			return err
		}
		err = tx.Commit()
		return err
	})
}

func currentWorldVecDim() int {
	var sqlText string
	err := DB.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='world_vec'`).Scan(&sqlText)
	if err != nil {
		return 0
	}
	m := regexp.MustCompile(`float\[(\d+)\]`).FindStringSubmatch(sqlText)
	if len(m) != 2 {
		return 0
	}
	dim, err := strconv.Atoi(m[1])
	if err != nil {
		return 0
	}
	return dim
}
