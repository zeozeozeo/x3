package cryptohelper

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"go.mau.fi/util/dbutil"
	"maunium.net/go/mautrix/crypto"
	"maunium.net/go/mautrix/id"
)

func TestModerncDriverCryptoStore(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "crypto.db")
	db, err := dbutil.NewWithDialect("file:"+dbPath, "sqlite3-fk-wal")
	if err != nil {
		t.Fatalf("failed to open modernc database: %v", err)
	}
	defer db.Close()

	store := crypto.NewSQLCryptoStore(db, dbutil.ZeroLogger(zerolog.Nop()), "test", "DEVICE", []byte("testkey"))
	if err = store.DB.Upgrade(context.Background()); err != nil {
		t.Fatalf("failed to upgrade crypto store: %v", err)
	}

	// Verify the schema was created and the connection pragmas were applied.
	raw, err := sql.Open("sqlite3-fk-wal", "file:"+dbPath)
	if err != nil {
		t.Fatalf("failed to reopen database: %v", err)
	}
	defer raw.Close()

	var tableCount int
	if err = raw.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='crypto_account'").Scan(&tableCount); err != nil {
		t.Fatalf("failed to query schema: %v", err)
	}
	if tableCount != 1 {
		t.Fatalf("expected crypto_account table to exist, got %d", tableCount)
	}

	var foreignKeys, journalMode string
	if err = raw.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("failed to query foreign_keys pragma: %v", err)
	}
	if foreignKeys != "1" {
		t.Fatalf("expected foreign_keys=1, got %q", foreignKeys)
	}
	if err = raw.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("failed to query journal_mode pragma: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("expected journal_mode=wal, got %q", journalMode)
	}

	// Verify a basic roundtrip through the store works on modernc.
	if err = store.PutNextBatch(context.Background(), "s_1"); err != nil {
		t.Fatalf("failed to put next batch: %v", err)
	}
	got, err := store.GetNextBatch(context.Background())
	if err != nil {
		t.Fatalf("failed to get next batch: %v", err)
	}
	if got != "s_1" {
		t.Fatalf("expected next batch s_1, got %q", got)
	}
	_ = id.DeviceID("")
}
