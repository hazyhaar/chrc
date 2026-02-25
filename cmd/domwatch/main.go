// CLAUDE:SUMMARY CLI entry point for domwatch — DOM observation daemon with single-page, profile, and config modes.
// Command domwatch is the DOM observation daemon.
//
// Usage:
//
//	domwatch -config domwatch.yaml          # observe pages from YAML config
//	domwatch -url https://example.com       # quick single-page observation
//	domwatch -profile https://example.com   # profile a single page
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "modernc.org/sqlite"

	"github.com/hazyhaar/chrc/domwatch"
	"github.com/hazyhaar/chrc/domwatch/mutation"
	"github.com/hazyhaar/pkg/idgen"
)

func main() {
	configPath := flag.String("config", "", "path to domwatch.yaml config file")
	singleURL := flag.String("url", "", "observe a single URL (stdout sink)")
	profileURL := flag.String("profile", "", "profile a single URL and exit")
	logLevel := flag.String("log-level", "info", "log level: debug, info, warn, error")
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

	if err := run(ctx, logger, *configPath, *singleURL, *profileURL); err != nil {
		logger.Error("domwatch: fatal", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger, configPath, singleURL, profileURL string) error {
	if profileURL != "" {
		return runProfile(ctx, logger, profileURL)
	}

	if singleURL != "" {
		return runSingle(ctx, logger, singleURL)
	}

	if configPath != "" {
		return runConfig(ctx, logger, configPath)
	}

	fmt.Fprintln(os.Stderr, "usage: domwatch -config <file> | -url <url> | -profile <url>")
	os.Exit(1)
	return nil
}

func runProfile(ctx context.Context, logger *slog.Logger, url string) error {
	cfg := defaultConfig()
	stdout := domwatch.NewStdoutSink(nil)
	w := domwatch.New(cfg, logger, stdout)

	if err := w.Start(ctx); err != nil {
		logger.Warn("domwatch: browser start issue", "error", err)
	}
	defer w.Stop()

	prof, err := w.ProfilePage(ctx, url, idgen.New())
	if err != nil {
		return fmt.Errorf("profile: %w", err)
	}

	data, _ := mutation.MarshalProfile(prof)
	os.Stdout.Write(data)
	os.Stdout.Write([]byte("\n"))
	return nil
}

func runSingle(ctx context.Context, logger *slog.Logger, url string) error {
	cfg := defaultConfig()
	cfg.Pages = []domwatch.PageConfig{{
		ID:           idgen.New(),
		URL:          url,
		StealthLevel: "auto",
		Profile:      true,
	}}

	stdout := domwatch.NewStdoutSink(nil)
	w := domwatch.New(cfg, logger, stdout)

	if err := w.Start(ctx); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	<-ctx.Done()
	w.Stop()
	return nil
}

func runConfig(ctx context.Context, logger *slog.Logger, path string) error {
	cfg, err := domwatch.LoadConfigFile(path)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	var sinks []domwatch.Sink
	for _, sc := range cfg.Sinks {
		switch sc.Type {
		case "stdout":
			sinks = append(sinks, domwatch.NewStdoutSink(nil))
		case "webhook":
			sinks = append(sinks, domwatch.NewWebhookSink(sc.URL, logger))
		default:
			logger.Warn("domwatch: unknown sink type", "type", sc.Type)
		}
	}
	if len(sinks) == 0 {
		sinks = append(sinks, domwatch.NewStdoutSink(nil))
	}

	w := domwatch.New(cfg, logger, sinks...)

	if err := w.Start(ctx); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	<-ctx.Done()
	w.Stop()
	return nil
}

func defaultConfig() *domwatch.Config {
	return &domwatch.Config{
		Browser: domwatch.BrowserConfig{
			Stealth:          "headless",
			MemoryLimit:      1 << 30,
			RecycleInterval:  4 * time.Hour,
			ResourceBlocking: []string{"images", "fonts", "media"},
		},
		Debounce: domwatch.DebounceConfig{
			Window:    250 * time.Millisecond,
			MaxBuffer: 1000,
		},
	}
}
