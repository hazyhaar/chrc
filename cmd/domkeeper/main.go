// CLAUDE:SUMMARY CLI entry point for domkeeper — content extraction engine with search, stats, and daemon mode.
// Command domkeeper is the content extraction and search engine.
//
// Usage:
//
//	domkeeper -config domkeeper.yaml       # run with config file
//	domkeeper -db domkeeper.db             # run with defaults
//	domkeeper -db domkeeper.db -search "query"  # search and exit
//	domkeeper -db domkeeper.db -stats      # show stats and exit
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	_ "modernc.org/sqlite"

	"github.com/hazyhaar/chrc/domkeeper"
)

func main() {
	configPath := flag.String("config", "", "path to domkeeper.yaml config file")
	dbPath := flag.String("db", "", "path to SQLite database")
	searchQuery := flag.String("search", "", "search query (exit after results)")
	showStats := flag.Bool("stats", false, "show stats and exit")
	logLevel := flag.String("log-level", "info", "log level: debug, info, warn, error")
	limit := flag.Int("limit", 20, "max search results")
	flag.Parse()

	var level slog.Level
	switch *logLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, logger, *configPath, *dbPath, *searchQuery, *showStats, *limit); err != nil {
		logger.Error("domkeeper: fatal", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger, configPath, dbPath, searchQuery string, showStats bool, limit int) error {
	cfg, err := resolveConfig(configPath, dbPath)
	if err != nil {
		return err
	}

	k, err := domkeeper.New(cfg, logger)
	if err != nil {
		return fmt.Errorf("init: %w", err)
	}
	defer k.Close()

	// One-shot: search.
	if searchQuery != "" {
		results, err := k.Search(ctx, domkeeper.SearchOptions{
			Query: searchQuery,
			Limit: limit,
		})
		if err != nil {
			return fmt.Errorf("search: %w", err)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(results)
	}

	// One-shot: stats.
	if showStats {
		stats, err := k.Stats(ctx)
		if err != nil {
			return fmt.Errorf("stats: %w", err)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(stats)
	}

	// Daemon mode.
	k.Start(ctx)
	logger.Info("domkeeper: running", "db", cfg.DBPath)

	<-ctx.Done()
	logger.Info("domkeeper: shutting down")
	return nil
}

func resolveConfig(configPath, dbPath string) (*domkeeper.Config, error) {
	if configPath != "" {
		return domkeeper.LoadConfigFile(configPath)
	}

	cfg := &domkeeper.Config{}
	if dbPath != "" {
		cfg.DBPath = dbPath
	}

	if cfg.DBPath == "" {
		fmt.Fprintln(os.Stderr, "usage: domkeeper -config <file> | -db <path> [-search <query>] [-stats]")
		os.Exit(1)
	}
	return cfg, nil
}
