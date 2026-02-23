package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"horos47/core/data"
)

// InitDocumentsSchema creates tables for documents, chunks, and pdf_pages.
func InitDocumentsSchema(db *sql.DB) error {
	schema := `
		CREATE TABLE IF NOT EXISTS documents (
			document_id BLOB PRIMARY KEY,
			title TEXT NOT NULL,
			source TEXT,
			content_type TEXT,
			metadata TEXT,
			created_at INTEGER NOT NULL
		);

		CREATE TABLE IF NOT EXISTS chunks (
			chunk_id BLOB PRIMARY KEY,
			document_id BLOB NOT NULL,
			chunk_index INTEGER NOT NULL,
			chunk_text TEXT NOT NULL,
			word_count INTEGER NOT NULL,
			created_at INTEGER NOT NULL,
			FOREIGN KEY (document_id) REFERENCES documents(document_id)
		);
		CREATE INDEX IF NOT EXISTS idx_chunks_document ON chunks(document_id);

		CREATE TABLE IF NOT EXISTS pdf_pages (
			page_id BLOB PRIMARY KEY,
			document_id BLOB NOT NULL,
			page_number INTEGER NOT NULL,
			blob_index INTEGER NOT NULL DEFAULT 0,
			total_blobs INTEGER NOT NULL DEFAULT 1,
			blob_data BLOB,
			ocr_text TEXT,
			image_hash TEXT,
			metadata TEXT,
			created_at INTEGER NOT NULL,
			FOREIGN KEY (document_id) REFERENCES documents(document_id)
		);
		CREATE INDEX IF NOT EXISTS idx_pdf_pages_document ON pdf_pages(document_id);
		CREATE INDEX IF NOT EXISTS idx_pdf_pages_page_number ON pdf_pages(document_id, page_number, blob_index);

		CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(
			chunk_id UNINDEXED,
			chunk_text,
			tokenize='porter unicode61'
		);

		CREATE TABLE IF NOT EXISTS vision_jobs (
			job_id BLOB PRIMARY KEY,
			document_id BLOB NOT NULL,
			page_number INTEGER NOT NULL,
			status TEXT NOT NULL,
			error_message TEXT,
			ocr_model TEXT,
			processing_time_ms INTEGER,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			FOREIGN KEY (document_id) REFERENCES documents(document_id)
		);
		CREATE INDEX IF NOT EXISTS idx_vision_jobs_status ON vision_jobs(status);
		CREATE INDEX IF NOT EXISTS idx_vision_jobs_document ON vision_jobs(document_id);
	`
	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("init documents schema: %w", err)
	}
	return nil
}

// Document represents an ingested document.
type Document struct {
	ID          data.UUID              `json:"id"`
	Title       string                 `json:"title"`
	Source      string                 `json:"source"`
	ContentType string                 `json:"content_type"`
	Metadata    map[string]interface{} `json:"metadata"`
	CreatedAt   time.Time              `json:"created_at"`
}

// Chunk represents a text chunk of a document.
type Chunk struct {
	ID         data.UUID `json:"id"`
	DocumentID data.UUID `json:"document_id"`
	ChunkIndex int       `json:"chunk_index"`
	ChunkText  string    `json:"chunk_text"`
	WordCount  int       `json:"word_count"`
	CreatedAt  time.Time `json:"created_at"`
}

// PDFPage represents a PDF page with 64KB blob splitting.
type PDFPage struct {
	PageID     data.UUID              `json:"page_id"`
	DocumentID data.UUID              `json:"document_id"`
	PageNumber int                    `json:"page_number"`
	BlobIndex  int                    `json:"blob_index"`
	TotalBlobs int                    `json:"total_blobs"`
	BlobData   []byte                 `json:"blob_data,omitempty"`
	OCRText    string                 `json:"ocr_text,omitempty"`
	ImageHash  string                 `json:"image_hash,omitempty"`
	Metadata   map[string]interface{} `json:"metadata"`
	CreatedAt  time.Time              `json:"created_at"`
}

// MaxBlobSize is the recommended SQLite blob limit (64 KB).
const MaxBlobSize = 64 * 1024

// SaveDocument saves a document and its chunks in a transaction.
func SaveDocument(db *sql.DB, doc *Document, chunks []Chunk) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer data.SafeTxRollback(tx, "save document")

	metadataJSON, err := json.Marshal(doc.Metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	_, err = tx.Exec(`
		INSERT INTO documents (document_id, title, source, content_type, metadata, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, doc.ID, doc.Title, doc.Source, doc.ContentType, string(metadataJSON), doc.CreatedAt.Unix())
	if err != nil {
		return fmt.Errorf("insert document: %w", err)
	}

	for _, chunk := range chunks {
		_, err = tx.Exec(`
			INSERT INTO chunks (chunk_id, document_id, chunk_index, chunk_text, word_count, created_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, chunk.ID, chunk.DocumentID, chunk.ChunkIndex, chunk.ChunkText, chunk.WordCount, chunk.CreatedAt.Unix())
		if err != nil {
			return fmt.Errorf("insert chunk %d: %w", chunk.ChunkIndex, err)
		}
		// Sync FTS5 index
		_, err = tx.Exec(`
			INSERT INTO chunks_fts (chunk_id, chunk_text) VALUES (?, ?)
		`, chunk.ID, chunk.ChunkText)
		if err != nil {
			return fmt.Errorf("insert chunk_fts %d: %w", chunk.ChunkIndex, err)
		}
	}

	return tx.Commit()
}

// GetDocument retrieves a document by ID.
func GetDocument(db *sql.DB, documentID data.UUID) (*Document, error) {
	row := db.QueryRow(`
		SELECT document_id, title, source, content_type, metadata, created_at
		FROM documents WHERE document_id = ?
	`, documentID)

	var doc Document
	var metadataJSON string
	var createdAtUnix int64

	err := row.Scan(&doc.ID, &doc.Title, &doc.Source, &doc.ContentType, &metadataJSON, &createdAtUnix)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(metadataJSON), &doc.Metadata); err != nil {
		return nil, fmt.Errorf("unmarshal metadata: %w", err)
	}
	doc.CreatedAt = time.Unix(createdAtUnix, 0)
	return &doc, nil
}

// GetChunks retrieves all chunks for a document ordered by index.
func GetChunks(db *sql.DB, documentID data.UUID) ([]Chunk, error) {
	rows, err := db.Query(`
		SELECT chunk_id, document_id, chunk_index, chunk_text, word_count, created_at
		FROM chunks WHERE document_id = ? ORDER BY chunk_index ASC
	`, documentID)
	if err != nil {
		return nil, err
	}
	defer data.SafeClose(rows, "get chunks")

	var chunks []Chunk
	for rows.Next() {
		var chunk Chunk
		var createdAtUnix int64
		err := rows.Scan(&chunk.ID, &chunk.DocumentID, &chunk.ChunkIndex, &chunk.ChunkText, &chunk.WordCount, &createdAtUnix)
		if err != nil {
			return nil, err
		}
		chunk.CreatedAt = time.Unix(createdAtUnix, 0)
		chunks = append(chunks, chunk)
	}
	return chunks, nil
}

// SavePDFPages saves PDF page blobs in a transaction.
func SavePDFPages(db *sql.DB, pages []PDFPage) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer data.SafeTxRollback(tx, "save pdf pages")

	for _, page := range pages {
		metadataJSON, err := json.Marshal(page.Metadata)
		if err != nil {
			return fmt.Errorf("marshal metadata: %w", err)
		}
		_, err = tx.Exec(`
			INSERT INTO pdf_pages (page_id, document_id, page_number, blob_index, total_blobs,
				blob_data, ocr_text, image_hash, metadata, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, page.PageID, page.DocumentID, page.PageNumber, page.BlobIndex, page.TotalBlobs,
			page.BlobData, page.OCRText, page.ImageHash, string(metadataJSON), page.CreatedAt.Unix())
		if err != nil {
			return fmt.Errorf("insert page %d blob %d: %w", page.PageNumber, page.BlobIndex, err)
		}
	}
	return tx.Commit()
}

// SplitImageBlobs splits image data into MaxBlobSize chunks.
func SplitImageBlobs(imageData []byte) [][]byte {
	if len(imageData) <= MaxBlobSize {
		return [][]byte{imageData}
	}
	var blobs [][]byte
	for i := 0; i < len(imageData); i += MaxBlobSize {
		end := i + MaxBlobSize
		if end > len(imageData) {
			end = len(imageData)
		}
		blobs = append(blobs, imageData[i:end])
	}
	return blobs
}
