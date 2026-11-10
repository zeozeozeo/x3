package cache

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

const (
	DefaultPath = "x3-cache.db"
	DefaultTTL = 30 * 24 * time.Hour
	DefaultMaxBytes = int64(256 << 20)
	DefaultSweepInterval = 10 * time.Minute

	NamespaceImageDescription = "image_description"
	NamespaceAudioTranscript  = "audio_transcript"
	NamespaceDocumentText     = "document_text"
	NamespaceLinkMetadata     = "link_metadata"
)

var ErrValueTooLarge = errors.New("cache value exceeds max bytes")

var Default *Store

// Config configures a Store.
type Config struct {
	Path          string
	DefaultTTL    time.Duration
	MaxBytes      int64
	SweepInterval time.Duration
}

// ConfigFromEnv reads the cache configuration from the environment. Unset or
// unparsable values keep their defaults.
func ConfigFromEnv() Config {
	cfg := Config{
		Path:          DefaultPath,
		DefaultTTL:    DefaultTTL,
		MaxBytes:      DefaultMaxBytes,
		SweepInterval: DefaultSweepInterval,
	}
	if v := strings.TrimSpace(os.Getenv("X3_CACHE_DB")); v != "" {
		cfg.Path = v
	}
	if v := strings.TrimSpace(os.Getenv("X3_CACHE_TTL")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.DefaultTTL = d
		}
	}
	if v := strings.TrimSpace(os.Getenv("X3_CACHE_MAX_BYTES")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			cfg.MaxBytes = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("X3_CACHE_SWEEP_INTERVAL")); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.SweepInterval = d
		}
	}
	return cfg
}

// Store is a SQLite-backed key/value cache with per-entry expiry and a global
// size cap. The zero value and a nil *Store are safe to use: they miss and
// discard writes.
type Store struct {
	db            *sql.DB
	defaultTTL    time.Duration
	maxBytes      int64
	sweepInterval time.Duration
	now           func() time.Time
	bytes         atomic.Int64
	sweepMu       sync.Mutex
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
}

func withConnectionPragmas(dataSourceName string) string {
	pragmas := []string{
		"_pragma=busy_timeout%285000%29",
		"_pragma=journal_mode%28WAL%29",
		"_pragma=synchronous%28NORMAL%29",
		"_pragma=auto_vacuum%282%29",
	}
	sep := "?"
	if strings.Contains(dataSourceName, "?") {
		sep = "&"
	}
	return dataSourceName + sep + strings.Join(pragmas, "&")
}

const schema = `
CREATE TABLE IF NOT EXISTS cache_entries (
  namespace TEXT NOT NULL,
  key TEXT NOT NULL,
  value BLOB NOT NULL,
  size INTEGER NOT NULL,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  PRIMARY KEY (namespace, key)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS cache_entries_expires_idx ON cache_entries(expires_at);
CREATE INDEX IF NOT EXISTS cache_entries_created_idx ON cache_entries(created_at);
`

func Open(cfg Config) (*Store, error) {
	if cfg.Path == "" {
		cfg.Path = DefaultPath
	}
	if cfg.DefaultTTL <= 0 {
		cfg.DefaultTTL = DefaultTTL
	}
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = DefaultMaxBytes
	}
	if cfg.SweepInterval == 0 {
		cfg.SweepInterval = DefaultSweepInterval
	} else if cfg.SweepInterval < 0 {
		cfg.SweepInterval = 0 // disabled
	}

	if dir := filepath.Dir(cfg.Path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("create cache directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite", withConnectionPragmas(cfg.Path))
	if err != nil {
		return nil, fmt.Errorf("open cache database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	s := &Store{
		db:            db,
		defaultTTL:    cfg.DefaultTTL,
		maxBytes:      cfg.MaxBytes,
		sweepInterval: cfg.SweepInterval,
		now:           time.Now,
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping cache database: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create cache schema: %w", err)
	}

	var total int64
	if err := db.QueryRow(`SELECT COALESCE(SUM(size), 0) FROM cache_entries`).Scan(&total); err != nil {
		db.Close()
		return nil, fmt.Errorf("measure cache: %w", err)
	}
	s.bytes.Store(total)

	if s.sweepInterval > 0 {
		s.ctx, s.cancel = context.WithCancel(context.Background())
		s.wg.Add(1)
		go s.runSweeper()
	}
	return s, nil
}

func (s *Store) nowTime() time.Time {
	if s.now == nil {
		return time.Now()
	}
	return s.now()
}

// Get returns the value stored under namespace/key when it is still fresh.
func (s *Store) Get(namespace, key string) ([]byte, bool, error) {
	if s == nil || s.db == nil || namespace == "" || key == "" {
		return nil, false, nil
	}
	var value []byte
	err := s.db.QueryRow(
		`SELECT value FROM cache_entries WHERE namespace = ? AND key = ? AND expires_at > ?`,
		namespace, key, s.nowTime().UnixNano(),
	).Scan(&value)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("cache get: %w", err)
	}
	return value, true, nil
}

// Put stores value under namespace/key with the store's default TTL.
func (s *Store) Put(namespace, key string, value []byte) error {
	if s == nil {
		return nil
	}
	return s.PutWithTTL(namespace, key, value, s.defaultTTL)
}

// PutWithTTL stores value under namespace/key for ttl. A non-positive ttl
// falls back to the store's default TTL.
func (s *Store) PutWithTTL(namespace, key string, value []byte, ttl time.Duration) error {
	if s == nil || s.db == nil || namespace == "" || key == "" {
		return nil
	}
	if ttl <= 0 {
		ttl = s.defaultTTL
	}
	size := int64(len(value))
	if size > s.maxBytes {
		return ErrValueTooLarge
	}

	now := s.nowTime()
	_, err := s.db.Exec(
		`INSERT INTO cache_entries (namespace, key, value, size, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(namespace, key) DO UPDATE SET
		   value = excluded.value,
		   size = excluded.size,
		   created_at = excluded.created_at,
		   expires_at = excluded.expires_at`,
		namespace, key, value, size, now.UnixNano(), now.Add(ttl).UnixNano(),
	)
	if err != nil {
		return fmt.Errorf("cache put: %w", err)
	}

	s.bytes.Add(size)
	if s.bytes.Load() > s.maxBytes {
		s.EnforceSize()
	}
	return nil
}

// EnforceSize evicts the oldest entries until the store fits under its cap.
func (s *Store) EnforceSize() {
	if s == nil || s.db == nil {
		return
	}
	s.sweepMu.Lock()
	defer s.sweepMu.Unlock()
	s.enforceSizeLocked()
}

type evictionCandidate struct {
	namespace string
	key       string
	size      int64
	createdAt int64
}

func (s *Store) enforceSizeLocked() {
	total, err := s.totalBytes()
	if err != nil {
		slog.Warn("cache size check failed", "err", err)
		return
	}
	for total > s.maxBytes {
		rows, err := s.db.Query(
			`SELECT namespace, key, size, created_at FROM cache_entries ORDER BY created_at ASC LIMIT 128`,
		)
		if err != nil {
			slog.Warn("cache eviction query failed", "err", err)
			break
		}
		var batch []evictionCandidate
		for rows.Next() {
			var c evictionCandidate
			if err := rows.Scan(&c.namespace, &c.key, &c.size, &c.createdAt); err != nil {
				slog.Warn("cache eviction scan failed", "err", err)
				batch = nil
				break
			}
			batch = append(batch, c)
		}
		if err := rows.Err(); err != nil {
			slog.Warn("cache eviction rows failed", "err", err)
			batch = nil
		}
		rows.Close()
		if len(batch) == 0 {
			break
		}

		removed := 0
		tx, err := s.db.Begin()
		if err != nil {
			slog.Warn("cache eviction begin failed", "err", err)
			break
		}
		for _, c := range batch {
			if total <= s.maxBytes {
				break // oldest first
			}
			res, err := tx.Exec(
				`DELETE FROM cache_entries WHERE namespace = ? AND key = ? AND created_at = ?`,
				c.namespace, c.key, c.createdAt,
			)
			if err != nil {
				slog.Warn("cache eviction delete failed", "err", err)
				break
			}
			if n, err := res.RowsAffected(); err == nil && n > 0 {
				total -= c.size
				removed++
			}
		}
		if err := tx.Commit(); err != nil {
			slog.Warn("cache eviction commit failed", "err", err)
			break
		}
		if removed == 0 {
			break
		}
	}
	if total < 0 {
		total = 0
	}
	s.bytes.Store(total)
}

func (s *Store) totalBytes() (int64, error) {
	var total int64
	err := s.db.QueryRow(`SELECT COALESCE(SUM(size), 0) FROM cache_entries`).Scan(&total)
	return total, err
}

// Sweep removes expired entries, re-applies the size cap, and returns freed
// pages to the filesystem.
func (s *Store) Sweep() error {
	if s == nil || s.db == nil {
		return nil
	}
	s.sweepMu.Lock()
	defer s.sweepMu.Unlock()

	res, err := s.db.Exec(`DELETE FROM cache_entries WHERE expires_at <= ?`, s.nowTime().UnixNano())
	if err != nil {
		return fmt.Errorf("cache sweep: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n > 0 {
		slog.Debug("cache sweep removed expired entries", "entries", n)
	}

	s.enforceSizeLocked()

	if _, err := s.db.Exec(`PRAGMA incremental_vacuum(256)`); err != nil {
		slog.Debug("cache incremental vacuum failed", "err", err)
	}
	return nil
}

func (s *Store) runSweeper() {
	defer s.wg.Done()
	ticker := time.NewTicker(s.sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			if err := s.Sweep(); err != nil {
				slog.Warn("cache sweep failed", "err", err)
			}
		}
	}
}

// Stats reports the number of stored entries and their total value size.
func (s *Store) Stats() (int, int64, error) {
	if s == nil || s.db == nil {
		return 0, 0, nil
	}
	var entries int
	var bytes int64
	if err := s.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(size), 0) FROM cache_entries`).Scan(&entries, &bytes); err != nil {
		return 0, 0, fmt.Errorf("cache stats: %w", err)
	}
	return entries, bytes, nil
}

// Close stops the sweeper and closes the database.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
	err := s.db.Close()
	s.db = nil
	if err != nil {
		return fmt.Errorf("close cache database: %w", err)
	}
	return nil
}

// Get reads from the process-wide store, missing when the cache is disabled.
func Get(namespace, key string) ([]byte, bool, error) {
	return Default.Get(namespace, key)
}

// Put writes to the process-wide store, doing nothing when it is disabled.
func Put(namespace, key string, value []byte) error {
	return Default.Put(namespace, key, value)
}

// PutWithTTL writes to the process-wide store with an explicit TTL.
func PutWithTTL(namespace, key string, value []byte, ttl time.Duration) error {
	return Default.PutWithTTL(namespace, key, value, ttl)
}
