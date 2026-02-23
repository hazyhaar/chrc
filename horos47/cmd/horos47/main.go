package main

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/quic-go/quic-go/http3"

	"horos47/core/chassis"
	"horos47/core/data"
	"horos47/core/jobs"
	"horos47/handlers"
	"horos47/services/gateway"
	"horos47/services/gpufeeder"
	"horos47/storage"
)

func main() {
	logger := setupLogger()
	logger.Info("HOROS 47 SINGULARITY starting")

	// 1. Database
	db, err := data.OpenDB("/inference/horos47/data/main.db")
	if err != nil {
		logger.Error("Failed to open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	logger.Info("Database opened", "path", "/inference/horos47/data/main.db")

	// 2. Init schemas (storage layer)
	if err := storage.InitDocumentsSchema(db); err != nil {
		logger.Error("Failed to init documents schema", "error", err)
	}
	if err := storage.InitEmbeddingsSchema(db); err != nil {
		logger.Error("Failed to init embeddings schema", "error", err)
	}

	// 3. Init services (only gateway + gpufeeder remain as services)
	gatewaySvc := gateway.New(db, logger)

	// GPU Submitter (Vision + Think jobs via gpu_jobs table, shared with GPU Feeder V3)
	var gpuSubmitter *gpufeeder.GPUSubmitter
	gpuCfg := gpufeeder.DefaultConfig()
	if sub, err := gpufeeder.NewGPUSubmitter(gpuCfg.JobsDBPath, gpuCfg.DataDir, logger); err != nil {
		logger.Warn("GPU Submitter not available", "error", err)
	} else {
		gpuSubmitter = sub
		logger.Info("GPU Submitter initialized", "jobs_db", gpuCfg.JobsDBPath)
	}

	logger.Info("Services initialized")

	// 4. Context for daemon and workers
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 5. Init agent workspaces
	for _, dir := range []string{
		"/inference/agents/sources/inbox",
		"/inference/agents/sources/processing",
		"/inference/agents/sources/done",
		"/inference/agents/sources/staging",
		"/inference/agents/syntheses",
		"/inference/agents/lexique",
		"/inference/agents/supervision",
		"/inference/agents/assistance",
		"/inference/agents/faq",
		"/inference/agents/benchmarks",
		"/inference/agents/search",
	} {
		os.MkdirAll(dir, 0755)
	}
	logger.Info("Agent workspaces initialized")

	// 6. Start gateway worker (envelope routing + dispatch)
	gatewaySvc.StartWorker(ctx)
	logger.Info("Gateway worker started")

	// 7. Create job worker and handlers
	worker := jobs.NewWorker(db, logger)
	queue, _ := jobs.NewQueue(db)

	h := &handlers.Handlers{
		DB:           db,
		Logger:       logger,
		Queue:        queue,
		GW:           gatewaySvc, // gateway.Service implements handlers.EnvelopeManager
		GPUSubmitter: gpuSubmitter,
		HTTPClient: &http.Client{
			Transport: &http.Transport{
				MaxIdleConnsPerHost: 2,
				IdleConnTimeout:     90 * time.Second,
			},
			Timeout: 30 * time.Second,
		},
		H3Client: &http.Client{
			Transport: &http3.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
					NextProtos:         []string{"h3"},
				},
			},
			Timeout: 5 * time.Minute, // large file downloads
		},
	}

	// Register ALL handlers from the unified handlers package
	h.RegisterAll(worker)
	logger.Info("All handlers registered via handlers package")

	// Configure concurrency per job type
	worker.SetConcurrency("image_to_ocr", 8)    // GPU batch — vLLM handles 8 parallel
	worker.SetConcurrency("ocr_to_database", 4)  // I/O parallel
	worker.SetConcurrency("pdf_to_images", 2)    // pdftoppm CPU-bound
	// all other types default to 1 (sequential)

	// Start worker
	go func() {
		if err := worker.Start(ctx); err != nil && err != context.Canceled {
			logger.Error("Job worker crashed", "error", err)
		}
	}()
	logger.Info("Job worker started")

	// Background embedder: auto-embeds chunks when embed server is available
	go h.RunBackgroundEmbedder(ctx)
	logger.Info("Background embedder started")

	// 8. Chassis (QUIC/HTTP3 server)
	server := chassis.NewServer(logger, ":8443")

	if err := server.RegisterService("gateway", gatewaySvc); err != nil {
		logger.Error("Failed to register gateway service", "error", err)
		os.Exit(1)
	}
	logger.Info("Gateway service registered on chassis")

	// 9. Start server
	go func() {
		logger.Info("Starting server on :8443")
		if err := server.Start(ctx); err != nil {
			logger.Error("Server crashed", "error", err)
			os.Exit(1)
		}
	}()

	// 10. Graceful Shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	logger.Info("Server ready - waiting for signals")
	<-sigChan
	logger.Info("Shutdown signal received")

	if err := server.Stop(ctx); err != nil {
		logger.Error("Error during shutdown", "error", err)
	}

	if gpuSubmitter != nil {
		gpuSubmitter.Close()
	}

	if err := gatewaySvc.Close(); err != nil {
		logger.Error("Error closing Gateway service", "error", err)
	}

	logger.Info("HOROS 47 stopped cleanly")
}

func setupLogger() *slog.Logger {
	opts := &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}
	handler := slog.NewJSONHandler(os.Stdout, opts)
	return slog.New(handler)
}
