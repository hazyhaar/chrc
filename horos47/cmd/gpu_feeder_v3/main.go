package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"horos47/core/data"
	"horos47/services/gpufeeder"
	_ "modernc.org/sqlite"
)

func main() {
	logger := setupLogger()
	logger.Info("GPU Feeder V3 starting")

	// 1. Configuration
	cfg := gpufeeder.DefaultConfig()

	// Détecter infrastructure LMCache
	cfg.LMCacheEnabled = gpufeeder.DetectLMCache(cfg, logger)

	// 2. Connexion DB principale (workload stats)
	mainDB, err := data.OpenDB("/inference/horos47/data/main.db")
	if err != nil {
		logger.Error("Failed to open main DB", "error", err)
		os.Exit(1)
	}
	defer mainDB.Close()

	// 3. Connexion DB jobs (gpu_jobs table)
	jobsDB, err := openJobsDB(cfg.JobsDBPath)
	if err != nil {
		logger.Error("Failed to open jobs DB", "error", err)
		os.Exit(1)
	}
	defer jobsDB.Close()

	// Init schema
	if err := initSchema(jobsDB); err != nil {
		logger.Error("Failed to init schema", "error", err)
		os.Exit(1)
	}

	// Migration incrémentale (ajoute prompt_hash si absent)
	if err := gpufeeder.MigrateSchema(jobsDB); err != nil {
		logger.Error("Failed to migrate schema", "error", err)
		os.Exit(1)
	}

	// 4. Créer service (orchestration + gestion containers)
	svc := gpufeeder.New(mainDB, jobsDB, cfg, logger)

	// 5. Créer worker (poll gpu_jobs + HTTP requests)
	worker := gpufeeder.NewWorker(jobsDB, cfg, logger, svc)

	// Context avec signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		logger.Info("Shutdown signal received")
		cancel()
	}()

	// 6. Démarrer service (lance containers vLLM persistants)
	if err := svc.Start(ctx); err != nil {
		logger.Error("Failed to start service", "error", err)
		os.Exit(1)
	}

	// 7. Démarrer worker (poll jobs + send HTTP)
	go func() {
		if err := worker.Run(ctx); err != nil && err != context.Canceled {
			logger.Error("Worker failed", "error", err)
		}
	}()

	logger.Info("GPU Feeder V3 ready")
	logger.Info("- Service: orchestrating vLLM containers (Vision + Think)")
	logger.Info("- Worker: polling gpu_jobs and sending HTTP requests")
	logger.Info("- Vision server: http://localhost:8001")
	logger.Info("- Think server: http://localhost:8002")

	if cfg.LMCacheEnabled {
		logger.Info("LMCache KV tiering: COMPLETE mode",
			"config", cfg.LMCacheConfigPath,
			"kvcache_mount", cfg.KVCacheMountPoint,
			"docker_image", cfg.LMCacheDockerImage)
	} else {
		logger.Info("LMCache KV tiering: DEGRADED mode",
			"native_offloading_gb", cfg.NativeOffloadingSizeGB)
	}

	// Wait for shutdown signal
	<-ctx.Done()

	// Cleanup
	logger.Info("Shutting down...")
	if err := svc.Close(); err != nil {
		logger.Error("Error during shutdown", "error", err)
	}
}

// openJobsDB ouvre DB jobs avec WAL mode
func openJobsDB(dbPath string) (*sql.DB, error) {
	// Créer répertoire parent si nécessaire
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}

	dsn := dbPath + "?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(10000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	// Connexion unique pour éviter SQLITE_BUSY
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	return db, nil
}

// initSchema initialise schema SQLite
func initSchema(db *sql.DB) error {
	schemaSQL, err := os.ReadFile("/inference/horos47/services/gpufeeder/schema.sql")
	if err != nil {
		return fmt.Errorf("read schema: %w", err)
	}

	_, err = db.Exec(string(schemaSQL))
	if err != nil {
		return fmt.Errorf("exec schema: %w", err)
	}

	return nil
}

// setupLogger configure logger structuré
func setupLogger() *slog.Logger {
	f, err := os.OpenFile("/tmp/gpu_feeder_v3.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		// Fallback sur stderr
		return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}

	opts := &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}
	handler := slog.NewJSONHandler(f, opts)
	return slog.New(handler)
}
