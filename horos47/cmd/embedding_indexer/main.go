package main

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"horos47/core/data"
	"horos47/core/jobs"

	_ "modernc.org/sqlite"
)

func main() {
	logger := setupLogger()
	logger.Info("HOROS Embedding Indexer starting")

	// Configuration
	dbPath := getEnv("DB_PATH", "/inference/horos47/data/main.db")
	pollInterval := 10 * time.Second
	batchSize := 32 // Optimal pour RTX 5090

	// Ouvrir database principale
	db, err := data.OpenDB(dbPath)
	if err != nil {
		logger.Error("Failed to open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	logger.Info("Database opened", "path", dbPath)

	// Initialiser job queue
	queue, err := jobs.NewQueue(db)
	if err != nil {
		logger.Error("Failed to initialize job queue", "error", err)
		os.Exit(1)
	}

	logger.Info("Job queue initialized")

	// Setup graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		logger.Info("Shutdown signal received")
		cancel()
	}()

	// Main processing loop
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	logger.Info("Indexer ready", "poll_interval", pollInterval, "batch_size", batchSize)

	for {
		select {
		case <-ctx.Done():
			logger.Info("Indexer stopped")
			return

		case <-ticker.C:
			// Trouver chunks sans embeddings
			chunks, err := findChunksWithoutEmbeddings(db, batchSize)
			if err != nil {
				logger.Error("Failed to find chunks", "error", err)
				continue
			}

			if len(chunks) == 0 {
				continue // Rien à indexer
			}

			logger.Info("Found chunks without embeddings", "count", len(chunks))

			// Créer jobs pour chaque chunk
			for _, chunk := range chunks {
				payload := map[string]interface{}{
					"texts": []string{chunk.Text},
					"chunk_id": chunk.ID.String(),
					"document_id": chunk.DocumentID.String(),
				}

				jobID, err := queue.Submit("rag_embed", payload)
				if err != nil {
					logger.Error("Failed to submit job", "chunk_id", chunk.ID.String(), "error", err)
					continue
				}

				logger.Debug("Job submitted", "job_id", jobID.String(), "chunk_id", chunk.ID.String())
			}

			logger.Info("Jobs submitted successfully", "count", len(chunks))
		}
	}
}

// ChunkInfo contient info d'un chunk à indexer
type ChunkInfo struct {
	ID         data.UUID
	DocumentID data.UUID
	Text       string
}

// findChunksWithoutEmbeddings récupère chunks sans embeddings via LEFT JOIN
func findChunksWithoutEmbeddings(db *sql.DB, limit int) ([]ChunkInfo, error) {
	query := `
		SELECT c.chunk_id, c.document_id, c.chunk_text
		FROM chunks c
		LEFT JOIN embeddings e ON c.chunk_id = e.chunk_id
		WHERE e.chunk_id IS NULL
		ORDER BY c.created_at ASC
		LIMIT ?
	`

	rows, err := db.Query(query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chunks []ChunkInfo
	for rows.Next() {
		var chunk ChunkInfo
		if err := rows.Scan(&chunk.ID, &chunk.DocumentID, &chunk.Text); err != nil {
			return nil, err
		}
		chunks = append(chunks, chunk)
	}

	return chunks, rows.Err()
}

func setupLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
