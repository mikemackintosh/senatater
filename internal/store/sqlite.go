package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"sort"

	_ "github.com/mattn/go-sqlite3"
)

// Chunk is a stored text chunk with its embedding and metadata.
type Chunk struct {
	ID         int64
	Source     string
	SourceType string
	Content    string
	Metadata   map[string]string
	Embedding  []float32
}

// Result represents a search hit with its cosine similarity score.
type Result struct {
	Chunk Chunk
	Score float32
}

// Store wraps a SQLite database holding chunks and their embeddings.
type Store struct {
	db *sql.DB
}

// Open creates or opens a SQLite database at the given path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", path+"?_journal=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

// Close releases the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// migrate creates the schema if it does not yet exist.
func (s *Store) migrate() error {
	_, err := s.db.Exec(`
	CREATE TABLE IF NOT EXISTS chunks (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		source      TEXT NOT NULL,
		source_type TEXT NOT NULL,
		content     TEXT NOT NULL,
		metadata    TEXT NOT NULL,
		embedding   BLOB NOT NULL
	);
	CREATE INDEX IF NOT EXISTS chunks_source ON chunks(source);
	`)
	return err
}

// InsertBatch stores multiple chunks in a single transaction.
func (s *Store) InsertBatch(ctx context.Context, chunks []Chunk) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO chunks (source, source_type, content, metadata, embedding) VALUES (?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, c := range chunks {
		meta, err := json.Marshal(c.Metadata)
		if err != nil {
			return err
		}
		if _, err := stmt.ExecContext(ctx, c.Source, c.SourceType, c.Content, string(meta), encodeVec(c.Embedding)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Search returns the top-k most similar chunks by cosine similarity.
// If sourceType is non-empty, only chunks of that type are considered.
// Loads matching rows and computes similarity in Go; fine for corpora under ~100k chunks.
func (s *Store) Search(ctx context.Context, query []float32, k int, sourceType string) ([]Result, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if sourceType == "" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, source, source_type, content, metadata, embedding FROM chunks`)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, source, source_type, content, metadata, embedding FROM chunks WHERE source_type = ?`,
			sourceType)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	qNorm := norm(query)
	var results []Result
	for rows.Next() {
		var c Chunk
		var metaJSON string
		var emb []byte
		if err := rows.Scan(&c.ID, &c.Source, &c.SourceType, &c.Content, &metaJSON, &emb); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(metaJSON), &c.Metadata); err != nil {
			return nil, err
		}
		c.Embedding = decodeVec(emb)
		results = append(results, Result{Chunk: c, Score: cosine(query, c.Embedding, qNorm)})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	if len(results) > k {
		results = results[:k]
	}
	return results, nil
}

// HasSource reports whether any chunks already exist for a given source path.
func (s *Store) HasSource(ctx context.Context, source string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM chunks WHERE source = ?`, source).Scan(&n)
	return n > 0, err
}

// Stats returns the total chunk count.
func (s *Store) Stats(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM chunks`).Scan(&n)
	return n, err
}

// encodeVec packs a float32 slice into a little-endian byte slice.
func encodeVec(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, f := range v {
		bits := math.Float32bits(f)
		b[i*4+0] = byte(bits)
		b[i*4+1] = byte(bits >> 8)
		b[i*4+2] = byte(bits >> 16)
		b[i*4+3] = byte(bits >> 24)
	}
	return b
}

// decodeVec unpacks a little-endian byte slice into a float32 slice.
func decodeVec(b []byte) []float32 {
	v := make([]float32, len(b)/4)
	for i := range v {
		bits := uint32(b[i*4+0]) | uint32(b[i*4+1])<<8 | uint32(b[i*4+2])<<16 | uint32(b[i*4+3])<<24
		v[i] = math.Float32frombits(bits)
	}
	return v
}

// cosine computes cosine similarity using a precomputed query norm.
func cosine(a, b []float32, aNorm float32) float32 {
	if len(a) != len(b) {
		return 0
	}
	var dot, bSqSum float32
	for i := range a {
		dot += a[i] * b[i]
		bSqSum += b[i] * b[i]
	}
	if aNorm == 0 || bSqSum == 0 {
		return 0
	}
	return dot / (aNorm * float32(math.Sqrt(float64(bSqSum))))
}

// norm computes the L2 norm of a vector.
func norm(v []float32) float32 {
	var s float32
	for _, x := range v {
		s += x * x
	}
	return float32(math.Sqrt(float64(s)))
}
