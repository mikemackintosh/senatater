package store

import (
	"context"
	"database/sql"
	"math"
	"path/filepath"
	"testing"
)

func TestEncodeDecodeVec_RoundTrip(t *testing.T) {
	in := []float32{0, 1, -1, 3.14159, -2.71828, 1e-7, 1e7}
	b := encodeVec(in)
	if len(b) != 4*len(in) {
		t.Fatalf("encoded length %d, want %d", len(b), 4*len(in))
	}
	out := decodeVec(b)
	if len(out) != len(in) {
		t.Fatalf("decoded length %d, want %d", len(out), len(in))
	}
	for i := range in {
		if in[i] != out[i] {
			t.Errorf("index %d: got %v, want %v", i, out[i], in[i])
		}
	}
}

func TestCosine(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{1, 0, 0}
	c := []float32{0, 1, 0}
	d := []float32{-1, 0, 0}

	if got := cosine(a, b, norm(a)); !approx(got, 1) {
		t.Errorf("parallel: got %v, want 1", got)
	}
	if got := cosine(a, c, norm(a)); !approx(got, 0) {
		t.Errorf("orthogonal: got %v, want 0", got)
	}
	if got := cosine(a, d, norm(a)); !approx(got, -1) {
		t.Errorf("opposite: got %v, want -1", got)
	}
}

func TestCosine_ZeroAndMismatched(t *testing.T) {
	zero := []float32{0, 0, 0}
	v := []float32{1, 2, 3}
	if got := cosine(zero, v, norm(zero)); got != 0 {
		t.Errorf("zero query: got %v, want 0", got)
	}
	if got := cosine(v, zero, norm(v)); got != 0 {
		t.Errorf("zero target: got %v, want 0", got)
	}
	if got := cosine(v, []float32{1, 2}, norm(v)); got != 0 {
		t.Errorf("mismatched dims: got %v, want 0", got)
	}
}

func approx(a, b float32) bool {
	return math.Abs(float64(a-b)) < 1e-5
}

func TestHasMessageID(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	has, err := s.HasMessageID(ctx, "<missing@example.com>")
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Errorf("empty store should not report has=true")
	}
	if has, _ := s.HasMessageID(ctx, ""); has {
		t.Errorf("empty id must return false without querying")
	}

	err = s.InsertBatch(ctx, []Chunk{{
		Source: "x.mbox", SourceType: "mbox", MessageID: "<known@example.com>",
		Content: "hi", Metadata: map[string]string{}, Embedding: []float32{0.1, 0.2},
	}})
	if err != nil {
		t.Fatal(err)
	}
	has, err = s.HasMessageID(ctx, "<known@example.com>")
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Errorf("inserted id not found")
	}
}

func TestMigrate_AddsMessageIDToLegacyDB(t *testing.T) {
	// Simulate a pre-migration database: original schema, no message_id column,
	// user_version still 0. The Store.migrate() call from Open() must lift it forward.
	path := filepath.Join(t.TempDir(), "legacy.db")
	raw, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Exec(`
		CREATE TABLE chunks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			source TEXT NOT NULL,
			source_type TEXT NOT NULL,
			content TEXT NOT NULL,
			metadata TEXT NOT NULL,
			embedding BLOB NOT NULL
		);
		CREATE INDEX chunks_source ON chunks(source);
	`)
	if err != nil {
		t.Fatal(err)
	}
	raw.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// After migration, message_id should exist and user_version should be 1.
	var hasCol int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('chunks') WHERE name='message_id'`,
	).Scan(&hasCol); err != nil {
		t.Fatal(err)
	}
	if hasCol != 1 {
		t.Errorf("message_id column missing after migration")
	}
	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 1 {
		t.Errorf("user_version after migration: %d, want 1", version)
	}
}

func newStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
