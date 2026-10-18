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

func worldVecRowCountForTest(t *testing.T) int {
	t.Helper()
	var n int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM world_vec_rowids`).Scan(&n); err != nil {
		t.Fatalf("count vectors: %v", err)
	}
	return n
}

// A dimension change must never silently drop the index. Before this guard the
// vector table was recreated at the new width, which orphaned every stored
// vector with no error and made retrieval quietly return nothing forever.
func TestWorldDimMismatchKeepsStoredVectors(t *testing.T) {
	initWorldmemTestDB(t)

	if _, err := InsertWorldEntry("chan-a", "the tower fell", unitBasisForTest(1024, 0), 3, nil, 1024); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := InsertWorldEntry("chan-a", "the dragon woke", unitBasisForTest(768, 1), 3, nil, 768); err == nil {
		t.Fatalf("expected a dimension mismatch error")
	}

	if got := worldVecRowCountForTest(t); got != 1 {
		t.Fatalf("stored vectors = %d, want 1 (the index must not be dropped)", got)
	}
	if got := CurrentWorldVecDim(); got != 1024 {
		t.Fatalf("world_vec dim = %d, want 1024", got)
	}

	// The original fact is still searchable.
	hits, err := SearchWorldSimilar("chan-a", unitBasisForTest(1024, 0), 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 || hits[0].Entry.Text != "the tower fell" {
		t.Fatalf("hits = %#v, want the tower fact", hits)
	}
}

func TestRebuildWorldVecTableKeepsMemories(t *testing.T) {
	initWorldmemTestDB(t)
	if _, err := InsertWorldEntry("chan-a", "the tower fell", unitBasisForTest(1024, 0), 3, nil, 1024); err != nil {
		t.Fatalf("insert: %v", err)
	}

	if err := RebuildWorldVecTable(768); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if got := CurrentWorldVecDim(); got != 768 {
		t.Fatalf("world_vec dim = %d, want 768", got)
	}
	if got := worldVecRowCountForTest(t); got != 0 {
		t.Fatalf("stored vectors = %d, want 0 after a rebuild", got)
	}

	// The memories themselves survive; only the vectors are gone, so they can
	// be recomputed.
	if n, err := CountAllWorldEntries(); err != nil || n != 1 {
		t.Fatalf("memories after rebuild = %d, %v; want 1", n, err)
	}
}

func TestWorldEntriesMissingVectors(t *testing.T) {
	initWorldmemTestDB(t)

	if _, err := InsertWorldEntry("chan-a", "has a vector", unitBasisForTest(1024, 0), 2, nil, 1024); err != nil {
		t.Fatalf("insert: %v", err)
	}
	// A memory with no vector, as an un-embedded import leaves behind.
	res, err := DB.Exec(`INSERT INTO world_memories (channel_key, text, tokens) VALUES (?, ?, ?)`,
		"chan-b", "has no vector", 2)
	if err != nil {
		t.Fatalf("insert memory: %v", err)
	}
	id, _ := res.LastInsertId()

	missing, err := WorldEntriesMissingVectors(10)
	if err != nil {
		t.Fatalf("missing: %v", err)
	}
	if len(missing) != 1 {
		t.Fatalf("missing = %#v, want exactly the un-embedded memory", missing)
	}
	if missing[0].ID != id || missing[0].ChannelKey != "chan-b" || missing[0].Text != "has no vector" {
		t.Fatalf("missing[0] = %#v", missing[0])
	}

	// Storing a vector clears it from the missing set and makes it searchable
	// from its own channel only.
	if err := UpsertWorldVector(missing[0].ID, "chan-b", unitBasisForTest(1024, 5), 1024); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	missing, err = WorldEntriesMissingVectors(10)
	if err != nil {
		t.Fatalf("missing: %v", err)
	}
	if len(missing) != 0 {
		t.Fatalf("still missing after upsert: %#v", missing)
	}
	hits, err := SearchWorldSimilar("chan-b", unitBasisForTest(1024, 5), 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 || hits[0].Entry.Text != "has no vector" {
		t.Fatalf("hits = %#v", hits)
	}
	hits, err = SearchWorldSimilar("chan-a", unitBasisForTest(1024, 5), 5)
	if err != nil {
		t.Fatalf("search chan-a: %v", err)
	}
	for _, h := range hits {
		if h.Entry.Text == "has no vector" {
			t.Fatalf("chan-b vector leaked into chan-a results: %#v", hits)
		}
	}
}

func TestUpsertWorldVectorReplacesExisting(t *testing.T) {
	initWorldmemTestDB(t)
	id, err := InsertWorldEntry("chan-a", "the tower fell", unitBasisForTest(1024, 0), 3, nil, 1024)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := UpsertWorldVector(id, "chan-a", unitBasisForTest(1024, 9), 1024); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if got := worldVecRowCountForTest(t); got != 1 {
		t.Fatalf("stored vectors = %d, want 1", got)
	}

	hits, err := SearchWorldSimilar("chan-a", unitBasisForTest(1024, 9), 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 || hits[0].Entry.ID != id {
		t.Fatalf("hits = %#v, want the updated vector's memory", hits)
	}
}

func TestUpsertWorldVectorRejectsBadInput(t *testing.T) {
	initWorldmemTestDB(t)
	id, err := InsertWorldEntry("chan-a", "the tower fell", unitBasisForTest(1024, 0), 3, nil, 1024)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := UpsertWorldVector(id, "chan-a", unitBasisForTest(768, 0), 1024); err == nil {
		t.Fatalf("expected a dimension error")
	}
	if err := UpsertWorldVector(0, "chan-a", unitBasisForTest(1024, 0), 1024); err == nil {
		t.Fatalf("expected an id error")
	}
	if err := UpsertWorldVector(id, "  ", unitBasisForTest(1024, 0), 1024); err == nil {
		t.Fatalf("expected a channel key error")
	}
}

func TestCountWorldEntriesWithoutVectors(t *testing.T) {
	initWorldmemTestDB(t)

	if n, err := CountWorldEntriesWithoutVectors(); err != nil || n != 0 {
		t.Fatalf("empty db count = %d, %v; want 0", n, err)
	}

	if _, err := InsertWorldEntry("chan-a", "has a vector", unitBasisForTest(1024, 0), 2, nil, 1024); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := DB.Exec(`INSERT INTO world_memories (channel_key, text, tokens) VALUES (?, ?, ?)`,
		"chan-b", "has no vector", 2); err != nil {
		t.Fatalf("insert memory: %v", err)
	}

	n, err := CountWorldEntriesWithoutVectors()
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("count = %d, want 1", n)
	}
}

func TestWorldIndexMetaRoundTrip(t *testing.T) {
	initWorldmemTestDB(t)

	if _, ok, err := GetWorldIndexMeta(); err != nil || ok {
		t.Fatalf("fresh database meta: ok=%v err=%v, want not ok", ok, err)
	}

	want := WorldIndexMeta{Model: "@cf/example/model", Dim: 1024}
	if err := SetWorldIndexMeta(want); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, ok, err := GetWorldIndexMeta()
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !ok || got != want {
		t.Fatalf("get = %+v ok=%v, want %+v", got, ok, want)
	}

	// Recording a new model must replace the old values, not accumulate.
	next := WorldIndexMeta{Model: "@cf/example/other", Dim: 768}
	if err := SetWorldIndexMeta(next); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, ok, err = GetWorldIndexMeta()
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !ok || got != next {
		t.Fatalf("get = %+v ok=%v, want %+v", got, ok, next)
	}

	var rows int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM world_index_meta`).Scan(&rows); err != nil {
		t.Fatalf("count meta rows: %v", err)
	}
	if rows != 2 {
		t.Fatalf("meta rows = %d, want 2", rows)
	}

	if err := SetWorldIndexMeta(WorldIndexMeta{}); err == nil {
		t.Fatalf("expected an error for empty metadata")
	}
	if err := SetWorldIndexMeta(WorldIndexMeta{Model: "m", Dim: 0}); err == nil {
		t.Fatalf("expected an error for a zero dimension")
	}
}
