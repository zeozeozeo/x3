package db

import (
	"path/filepath"
	"testing"
)

func initWorldmemTestDB(t *testing.T) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "worldmem_test.db")
	if err := InitDB(path); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() {
		if DB != nil {
			_ = DB.Close()
			DB = nil
		}
	})
}

// unitBasis returns a unit vector with vecDim components.
func unitBasisForTest(dim, hot int) []float32 {
	v := make([]float32, dim)
	v[hot%dim] = 1
	return v
}

func TestWorldInsertAndScope(t *testing.T) {
	initWorldmemTestDB(t)

	if _, err := InsertWorldEntry("chan-a", "the tower fell", unitBasisForTest(1024, 0), 3, nil, 1024); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := InsertWorldEntry("chan-b", "the dragon woke", unitBasisForTest(1024, 1), 3, nil, 1024); err != nil {
		t.Fatalf("insert: %v", err)
	}

	hits, err := SearchWorldSimilar("chan-a", unitBasisForTest(1024, 0), 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatalf("expected hits for chan-a")
	}
	for _, h := range hits {
		if h.Entry.Text == "the dragon woke" {
			t.Fatalf("chan-b memory leaked into chan-a results")
		}
	}
	if hits[0].Entry.Text != "the tower fell" {
		t.Fatalf("best hit = %q, want tower fact", hits[0].Entry.Text)
	}

	n, err := CountWorldEntries("chan-a")
	if err != nil || n != 1 {
		t.Fatalf("count chan-a = %d, %v", n, err)
	}
}

func TestWorldDimMismatchRejected(t *testing.T) {
	initWorldmemTestDB(t)
	if _, err := InsertWorldEntry("chan-a", "bad dim", unitBasisForTest(128, 0), 2, nil, 1024); err == nil {
		t.Fatalf("expected dim mismatch error")
	}
}

func TestWorldClearScoped(t *testing.T) {
	initWorldmemTestDB(t)
	if _, err := InsertWorldEntry("chan-a", "fact a", unitBasisForTest(1024, 0), 2, nil, 1024); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := InsertWorldEntry("chan-b", "fact b", unitBasisForTest(1024, 1), 2, nil, 1024); err != nil {
		t.Fatalf("insert: %v", err)
	}
	n, err := ClearWorldEntries("chan-a")
	if err != nil || n != 1 {
		t.Fatalf("clear = %d, %v", n, err)
	}
	if n, _ := CountWorldEntries("chan-b"); n != 1 {
		t.Fatalf("chan-b should be untouched, count = %d", n)
	}
}

func TestWorldExportImportRoundTrip(t *testing.T) {
	initWorldmemTestDB(t)
	if _, err := InsertWorldEntry("chan-a", "fact one", unitBasisForTest(1024, 0), 2, nil, 1024); err != nil {
		t.Fatalf("insert: %v", err)
	}

	data, err := ExportWorldEntries("chan-a")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	entries, err := ParseWorldExport(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(entries) != 1 || entries[0].Text != "fact one" {
		t.Fatalf("round trip = %#v", entries)
	}

	if _, err := ParseWorldExport([]byte(`{"version":999,"entries":[]}`)); err == nil {
		t.Fatalf("expected version error")
	}
	if _, err := ParseWorldExport([]byte(`not json`)); err == nil {
		t.Fatalf("expected JSON error")
	}
}
