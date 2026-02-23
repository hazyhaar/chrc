package storage

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
)

// InitEmbeddingsSchema creates tables for embeddings and vector search cache.
func InitEmbeddingsSchema(db *sql.DB) error {
	schema := `
		CREATE TABLE IF NOT EXISTS embeddings (
			chunk_id BLOB PRIMARY KEY,
			document_id BLOB NOT NULL,
			embedding BLOB NOT NULL,
			dimension INTEGER NOT NULL,
			norm REAL NOT NULL,
			model_name TEXT NOT NULL DEFAULT 'gte-Qwen2-1.5B-instruct',
			created_at INTEGER NOT NULL,
			FOREIGN KEY (chunk_id) REFERENCES chunks(chunk_id) ON DELETE CASCADE,
			FOREIGN KEY (document_id) REFERENCES documents(document_id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS idx_embeddings_created ON embeddings(created_at);
		CREATE INDEX IF NOT EXISTS idx_embeddings_document ON embeddings(document_id);

		CREATE TABLE IF NOT EXISTS vector_search_cache (
			query_hash BLOB PRIMARY KEY,
			query_text TEXT NOT NULL,
			results BLOB NOT NULL,
			created_at INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_vector_cache_created ON vector_search_cache(created_at);
	`
	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("init embeddings schema: %w", err)
	}
	return nil
}

// SerializeVector converts a float32 slice to bytes (little endian).
func SerializeVector(vector []float32) []byte {
	blob := make([]byte, len(vector)*4)
	for i, v := range vector {
		binary.LittleEndian.PutUint32(blob[i*4:], math.Float32bits(v))
	}
	return blob
}

// DeserializeVector converts bytes to a float32 slice.
func DeserializeVector(blob []byte) []float32 {
	vector := make([]float32, len(blob)/4)
	for i := range vector {
		bits := binary.LittleEndian.Uint32(blob[i*4:])
		vector[i] = math.Float32frombits(bits)
	}
	return vector
}

// CosineSimilarity computes cosine similarity between two vectors.
func CosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

// CosineSimilarityOptimized computes cosine similarity with pre-calculated norms.
func CosineSimilarityOptimized(a, b []float32, normA, normB float64) float64 {
	if len(a) != len(b) || normA == 0 || normB == 0 {
		return 0
	}
	var dotProduct float64
	for i := range a {
		dotProduct += float64(a[i]) * float64(b[i])
	}
	return dotProduct / (normA * normB)
}

// CalculateNorm computes the L2 norm of a vector.
func CalculateNorm(vec []float32) float64 {
	var sum float64
	for _, v := range vec {
		sum += float64(v) * float64(v)
	}
	return math.Sqrt(sum)
}
