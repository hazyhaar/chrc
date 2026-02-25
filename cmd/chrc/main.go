// CLAUDE:SUMMARY Entry point for the veille HTTP service — chi router, Basic Auth, usertenant pool, MCP/QUIC optional.
package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hazyhaar/chrc/veille"
	"github.com/hazyhaar/chrc/veille/catalog"
	"github.com/hazyhaar/pkg/audit"
	"github.com/hazyhaar/pkg/auth"
	"github.com/hazyhaar/pkg/connectivity"
	"github.com/hazyhaar/pkg/dbopen"
	"github.com/hazyhaar/pkg/horosafe"
	"github.com/hazyhaar/pkg/idgen"
	"github.com/hazyhaar/pkg/shield"
	"github.com/hazyhaar/pkg/mcpquic"
	"github.com/hazyhaar/pkg/trace"
	tenant "github.com/hazyhaar/usertenant"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

//go:embed static
var staticFS embed.FS

func main() {
	port := env("PORT", "8085")
	secretInput := os.Getenv("SESSION_SECRET")
	if secretInput == "" {
		secretInput = os.Getenv("AUTH_PASSWORD")
	}
	if secretInput == "" {
		slog.Error("SESSION_SECRET or AUTH_PASSWORD is required")
		os.Exit(1)
	}
	// Derive 32-byte JWT secret via SHA-256 (satisfies horosafe.MinSecretLen).
	secretHash := sha256.Sum256([]byte(secretInput))
	jwtSecret := secretHash[:]

	dataDir := env("DATA_DIR", "data")
	catalogPath := env("CATALOG_DB", "db/catalog.db")
	bufferDir := env("BUFFER_DIR", "buffer/pending")
	mcpTransport := env("MCP_TRANSPORT", "")
	logLevel := env("LOG_LEVEL", "info")

	// Logging.
	var lvl slog.Level
	switch logLevel {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
	slog.SetDefault(logger)

	// Signal context.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Trace DB — opened with raw "sqlite" driver (never "sqlite-trace" to avoid recursion).
	tracePath := env("TRACE_DB", "db/traces.db")
	traceDB, err := dbopen.Open(tracePath, dbopen.WithMkdirAll())
	if err != nil {
		slog.Error("trace db", "error", err)
		os.Exit(1)
	}
	defer traceDB.Close()
	traceStore := trace.NewStore(traceDB)
	if err := traceStore.Init(); err != nil {
		slog.Error("trace init", "error", err)
		os.Exit(1)
	}
	trace.SetStore(traceStore)
	defer traceStore.Close()

	// Catalog DB — opened with "sqlite-trace" driver for transparent SQL tracing.
	catalogDB, err := dbopen.Open(catalogPath, dbopen.WithMkdirAll(), dbopen.WithTrace())
	if err != nil {
		slog.Error("catalog db", "error", err)
		os.Exit(1)
	}
	defer catalogDB.Close()

	if err := tenant.InitCatalog(ctx, catalogDB); err != nil {
		slog.Error("init catalog", "error", err)
		os.Exit(1)
	}

	// Extend users table with auth columns.
	if err := migrateAuthColumns(catalogDB); err != nil {
		slog.Error("migrate auth columns", "error", err)
		os.Exit(1)
	}

	// Global admin tables (engines + source registry).
	if err := migrateGlobalTables(catalogDB); err != nil {
		slog.Error("migrate global tables", "error", err)
		os.Exit(1)
	}

	// Audit logger (writes to catalog DB).
	auditLogger := audit.NewSQLiteLogger(catalogDB)
	if err := auditLogger.Init(); err != nil {
		slog.Error("audit init", "error", err)
		os.Exit(1)
	}
	defer auditLogger.Close()

	// Seed admin user if no admin exists.
	if err := seedAdmin(ctx, catalogDB); err != nil {
		slog.Error("seed admin", "error", err)
		os.Exit(1)
	}

	// Seed global engines from catalog.
	seedGlobalEngines(ctx, catalogDB)

	// Usertenant pool.
	pool, err := tenant.New(dataDir, catalogDB)
	if err != nil {
		slog.Error("usertenant pool", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Migrate existing shard schemas (idempotent — adds new tables/indexes).
	migrateExistingShards(ctx, catalogDB, pool)

	// Connectivity router — enables plug-and-play external source handlers.
	router := connectivity.New(connectivity.WithLogger(logger))
	router.RegisterLocal("github_fetch", veille.NewGitHubService(""))
	router.RegisterLocal("api_fetch", veille.NewAPIService())

	// Veille service.
	svc, err := veille.New(pool, &veille.Config{
		DataDir:   dataDir,
		BufferDir: bufferDir,
	}, logger, veille.WithCatalogDB(catalogDB), veille.WithRouter(router), veille.WithAudit(auditLogger))
	if err != nil {
		slog.Error("veille service", "error", err)
		os.Exit(1)
	}
	defer svc.Close()

	// Optional MCP QUIC.
	if mcpTransport == "quic" {
		mcpSrv := mcp.NewServer(&mcp.Implementation{
			Name:    "veille",
			Version: "1.0.0",
		}, nil)
		svc.RegisterMCP(mcpSrv)

		quicAddr := env("MCP_QUIC_ADDR", ":9444")
		certFile := env("TLS_CERT", "")
		keyFile := env("TLS_KEY", "")

		var tlsCfg *tls.Config
		if certFile != "" && keyFile != "" {
			tlsCfg, err = mcpquic.ServerTLSConfig(certFile, keyFile)
		} else {
			tlsCfg, err = mcpquic.SelfSignedTLSConfig()
		}
		if err != nil {
			slog.Error("MCP QUIC TLS", "error", err)
		} else {
			ql, qErr := mcpquic.NewListener(quicAddr, tlsCfg, mcpSrv, logger)
			if qErr != nil {
				slog.Error("MCP QUIC listener", "error", qErr)
			} else {
				go func() {
					slog.Info("MCP QUIC starting", "addr", quicAddr)
					if sErr := ql.Serve(ctx); sErr != nil && ctx.Err() == nil {
						slog.Error("MCP QUIC", "error", sErr)
					}
				}()
			}
		}
	}

	// Start scheduler.
	svc.Start(ctx)

	// User service (DB operations for auth).
	users := &userService{db: catalogDB, pool: pool}

	// Router.
	r := chi.NewRouter()
	for _, mw := range shield.DefaultBOStack() {
		r.Use(mw)
	}
	r.Use(auth.Middleware(jwtSecret)) // Parse JWT on all routes (soft — doesn't enforce).

	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok"})
	})

	// Public auth endpoints (no session required).
	r.Post("/api/auth/login", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, 400, err)
			return
		}
		claims, err := users.authenticate(r.Context(), req.Email, req.Password)
		if err != nil {
			writeJSON(w, 401, map[string]string{"error": "identifiants invalides"})
			return
		}
		token, err := auth.GenerateToken(jwtSecret, claims, 30*24*time.Hour)
		if err != nil {
			writeError(w, 500, err)
			return
		}
		secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
		auth.SetTokenCookie(w, token, "", secure)
		writeJSON(w, 200, map[string]string{"id": claims.UserID, "name": claims.Username, "role": claims.Role})
	})

	r.Post("/api/auth/logout", func(w http.ResponseWriter, _ *http.Request) {
		auth.ClearTokenCookie(w, "")
		writeJSON(w, 200, map[string]string{"status": "ok"})
	})

	// SPA: serve index.html and static assets (no auth — login page is in the SPA).
	r.Get("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		f, err := staticFS.Open("static/index.html")
		if err != nil {
			http.Error(w, "not found", 404)
			return
		}
		defer f.Close()
		io.Copy(w, f)
	})
	r.Handle("/static/*", http.FileServerFS(staticFS))

	// All API endpoints require a valid session.
	r.Group(func(r chi.Router) {
		r.Use(requireSession)

		r.Get("/api/auth/me", func(w http.ResponseWriter, r *http.Request) {
			c := auth.GetClaims(r.Context())
			writeJSON(w, 200, map[string]string{"id": c.UserID, "name": c.Username, "role": c.Role})
		})

		// Admin: user management.
		r.Route("/api/admin/users", func(r chi.Router) {
			r.Use(requireAdmin)

			r.Get("/", func(w http.ResponseWriter, r *http.Request) {
				list, err := users.listUsers(r.Context())
				if err != nil {
					writeError(w, 500, err)
					return
				}
				writeJSON(w, 200, list)
			})

			r.Post("/", func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Email    string `json:"email"`
					Name     string `json:"name"`
					Password string `json:"password"`
					Role     string `json:"role"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					writeError(w, 400, err)
					return
				}
				if req.Role == "" {
					req.Role = "user"
				}
				user, err := users.createUser(r.Context(), req.Email, req.Name, req.Password, req.Role)
				if err != nil {
					writeError(w, 400, err)
					return
				}
				writeJSON(w, 201, user)
			})

			r.Delete("/{userID}", func(w http.ResponseWriter, r *http.Request) {
				userID := chi.URLParam(r, "userID")
				if err := users.deleteUser(r.Context(), userID); err != nil {
					writeError(w, 500, err)
					return
				}
				writeJSON(w, 200, map[string]string{"status": "deleted"})
			})
		})

		// Admin: global engines.
		r.Route("/api/admin/engines", func(r chi.Router) {
			r.Use(requireAdmin)
			r.Get("/", func(w http.ResponseWriter, r *http.Request) {
				engines, err := listGlobalEngines(r.Context(), catalogDB)
				if err != nil {
					writeError(w, 500, err)
					return
				}
				writeJSON(w, 200, engines)
			})
			r.Post("/", func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					ID          string `json:"id"`
					Name        string `json:"name"`
					Strategy    string `json:"strategy"`
					URLTemplate string `json:"url_template"`
					APIConfig   string `json:"api_config"`
					Selectors   string `json:"selectors"`
					RateLimitMs int64  `json:"rate_limit_ms"`
					MaxPages    int    `json:"max_pages"`
					Enabled     *bool  `json:"enabled"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					writeError(w, 400, err)
					return
				}
				id := req.ID
				if id == "" {
					id = idgen.New()
				}
				now := time.Now().UnixMilli()
				enabled := 1
				if req.Enabled != nil && !*req.Enabled {
					enabled = 0
				}
				if req.APIConfig == "" {
					req.APIConfig = "{}"
				}
				if req.Selectors == "" {
					req.Selectors = "{}"
				}
				if req.Strategy == "" {
					req.Strategy = "api"
				}
				if req.RateLimitMs == 0 {
					req.RateLimitMs = 2000
				}
				if req.MaxPages == 0 {
					req.MaxPages = 3
				}
				_, err := catalogDB.ExecContext(r.Context(),
					`INSERT INTO global_search_engines (id, name, strategy, url_template, api_config, selectors, rate_limit_ms, max_pages, enabled, created_at, updated_at)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
					id, req.Name, req.Strategy, req.URLTemplate, req.APIConfig, req.Selectors,
					req.RateLimitMs, req.MaxPages, enabled, now, now)
				if err != nil {
					writeError(w, 500, err)
					return
				}
				writeJSON(w, 201, map[string]string{"id": id, "name": req.Name})
			})
			r.Put("/{id}", func(w http.ResponseWriter, r *http.Request) {
				id := chi.URLParam(r, "id")
				var req struct {
					Name        string `json:"name"`
					Strategy    string `json:"strategy"`
					URLTemplate string `json:"url_template"`
					APIConfig   string `json:"api_config"`
					Selectors   string `json:"selectors"`
					RateLimitMs int64  `json:"rate_limit_ms"`
					MaxPages    int    `json:"max_pages"`
					Enabled     *bool  `json:"enabled"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					writeError(w, 400, err)
					return
				}
				now := time.Now().UnixMilli()
				enabled := 1
				if req.Enabled != nil && !*req.Enabled {
					enabled = 0
				}
				_, err := catalogDB.ExecContext(r.Context(),
					`UPDATE global_search_engines SET name=?, strategy=?, url_template=?, api_config=?, selectors=?, rate_limit_ms=?, max_pages=?, enabled=?, updated_at=? WHERE id=?`,
					req.Name, req.Strategy, req.URLTemplate, req.APIConfig, req.Selectors,
					req.RateLimitMs, req.MaxPages, enabled, now, id)
				if err != nil {
					writeError(w, 500, err)
					return
				}
				writeJSON(w, 200, map[string]string{"id": id, "status": "updated"})
			})
			r.Delete("/{id}", func(w http.ResponseWriter, r *http.Request) {
				id := chi.URLParam(r, "id")
				_, err := catalogDB.ExecContext(r.Context(),
					`DELETE FROM global_search_engines WHERE id = ?`, id)
				if err != nil {
					writeError(w, 500, err)
					return
				}
				writeJSON(w, 200, map[string]string{"status": "deleted"})
			})
		})

		// Admin: source registry.
		r.Route("/api/admin/source-registry", func(r chi.Router) {
			r.Use(requireAdmin)
			r.Get("/", func(w http.ResponseWriter, r *http.Request) {
				entries, err := listSourceRegistry(r.Context(), catalogDB)
				if err != nil {
					writeError(w, 500, err)
					return
				}
				writeJSON(w, 200, entries)
			})
			r.Post("/", func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					ID            string `json:"id"`
					Name          string `json:"name"`
					URL           string `json:"url"`
					SourceType    string `json:"source_type"`
					Category      string `json:"category"`
					ConfigJSON    string `json:"config_json"`
					Description   string `json:"description"`
					FetchInterval int64  `json:"fetch_interval"`
					Enabled       *bool  `json:"enabled"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					writeError(w, 400, err)
					return
				}
				id := req.ID
				if id == "" {
					id = idgen.New()
				}
				now := time.Now().UnixMilli()
				enabled := 1
				if req.Enabled != nil && !*req.Enabled {
					enabled = 0
				}
				if req.SourceType == "" {
					req.SourceType = "rss"
				}
				if req.ConfigJSON == "" {
					req.ConfigJSON = "{}"
				}
				if req.FetchInterval == 0 {
					req.FetchInterval = 3600000
				}
				_, err := catalogDB.ExecContext(r.Context(),
					`INSERT INTO source_registry (id, name, url, source_type, category, config_json, description, fetch_interval, enabled, created_at, updated_at)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
					id, req.Name, req.URL, req.SourceType, req.Category, req.ConfigJSON,
					req.Description, req.FetchInterval, enabled, now, now)
				if err != nil {
					writeError(w, 500, err)
					return
				}
				writeJSON(w, 201, map[string]string{"id": id, "name": req.Name})
			})
			r.Put("/{id}", func(w http.ResponseWriter, r *http.Request) {
				id := chi.URLParam(r, "id")
				var req struct {
					Name          string `json:"name"`
					URL           string `json:"url"`
					SourceType    string `json:"source_type"`
					Category      string `json:"category"`
					ConfigJSON    string `json:"config_json"`
					Description   string `json:"description"`
					FetchInterval int64  `json:"fetch_interval"`
					Enabled       *bool  `json:"enabled"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					writeError(w, 400, err)
					return
				}
				now := time.Now().UnixMilli()
				enabled := 1
				if req.Enabled != nil && !*req.Enabled {
					enabled = 0
				}
				_, err := catalogDB.ExecContext(r.Context(),
					`UPDATE source_registry SET name=?, url=?, source_type=?, category=?, config_json=?, description=?, fetch_interval=?, enabled=?, updated_at=? WHERE id=?`,
					req.Name, req.URL, req.SourceType, req.Category, req.ConfigJSON,
					req.Description, req.FetchInterval, enabled, now, id)
				if err != nil {
					writeError(w, 500, err)
					return
				}
				writeJSON(w, 200, map[string]string{"id": id, "status": "updated"})
			})
			r.Delete("/{id}", func(w http.ResponseWriter, r *http.Request) {
				id := chi.URLParam(r, "id")
				_, err := catalogDB.ExecContext(r.Context(),
					`DELETE FROM source_registry WHERE id = ?`, id)
				if err != nil {
					writeError(w, 500, err)
					return
				}
				writeJSON(w, 200, map[string]string{"status": "deleted"})
			})
		})

		// Admin: overview (cross-tenant).
		r.Route("/api/admin/overview", func(r chi.Router) {
			r.Use(requireAdmin)
			r.Get("/", func(w http.ResponseWriter, r *http.Request) {
				overview, err := buildOverview(r.Context(), catalogDB, pool, svc)
				if err != nil {
					writeError(w, 500, err)
					return
				}
				writeJSON(w, 200, overview)
			})
			r.Get("/{dossierID}/searches", func(w http.ResponseWriter, r *http.Request) {
				dossierID := chi.URLParam(r, "dossierID")
				limit := queryInt(r, "limit", 50)
				entries, err := svc.SearchLog(r.Context(), dossierID, limit)
				if err != nil {
					writeError(w, 500, err)
					return
				}
				writeJSON(w, 200, entries)
			})
			r.Post("/{dossierID}/promote", func(w http.ResponseWriter, r *http.Request) {
				dossierID := chi.URLParam(r, "dossierID")
				var req struct {
					Query      string   `json:"query"`
					Channels   []string `json:"channels"`
					ScheduleMs int64    `json:"schedule_ms"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					writeError(w, 400, err)
					return
				}
				if req.ScheduleMs == 0 {
					req.ScheduleMs = 86400000
				}
				channelsJSON, _ := json.Marshal(req.Channels)
				q := &veille.TrackedQuestion{
					Text:       req.Query,
					Keywords:   req.Query,
					Channels:   string(channelsJSON),
					ScheduleMs: req.ScheduleMs,
					MaxResults: 20,
					FollowLinks: true,
					Enabled:    true,
				}
				if err := svc.AddQuestion(r.Context(), dossierID, q); err != nil {
					writeError(w, 500, err)
					return
				}
				writeJSON(w, 201, map[string]string{"id": q.ID, "status": "promoted"})
			})
		})

		// User: browse source registry (read-only).
		r.Get("/api/source-registry", func(w http.ResponseWriter, r *http.Request) {
			entries, err := listSourceRegistry(r.Context(), catalogDB)
			if err != nil {
				writeError(w, 500, err)
				return
			}
			writeJSON(w, 200, entries)
		})

		// Dossiers: list, create, delete.
		r.Get("/api/dossiers", func(w http.ResponseWriter, r *http.Request) {
			rows, err := catalogDB.QueryContext(r.Context(),
				`SELECT id, name FROM shards WHERE status = 'active' ORDER BY name`)
			if err != nil {
				writeError(w, 500, err)
				return
			}
			defer rows.Close()
			var dossiers []map[string]string
			for rows.Next() {
				var id, name string
				if err := rows.Scan(&id, &name); err != nil {
					writeError(w, 500, err)
					return
				}
				dossiers = append(dossiers, map[string]string{"id": id, "name": name})
			}
			if dossiers == nil {
				dossiers = []map[string]string{}
			}
			writeJSON(w, 200, dossiers)
		})

		r.Post("/api/dossiers", func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				Name string `json:"name"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeError(w, 400, err)
				return
			}
			if req.Name == "" {
				writeError(w, 400, fmt.Errorf("name requis"))
				return
			}
			dossierID := idgen.New()
			ownerID := ""
			if c := auth.GetClaims(r.Context()); c != nil {
				ownerID = c.UserID
			}
			if err := pool.CreateShard(r.Context(), dossierID, ownerID, req.Name); err != nil {
				writeError(w, 500, err)
				return
			}
			writeJSON(w, 201, map[string]string{"id": dossierID, "name": req.Name})
		})

		r.Delete("/api/dossiers/{dossierID}", func(w http.ResponseWriter, r *http.Request) {
			dossierID := chi.URLParam(r, "dossierID")
			// Guard: don't delete if dossierID matches a known sub-resource path.
			if dossierID == "" {
				writeError(w, 400, fmt.Errorf("dossierID requis"))
				return
			}
			if err := pool.DeleteShard(r.Context(), dossierID); err != nil {
				writeError(w, 500, err)
				return
			}
			writeJSON(w, 200, map[string]string{"status": "deleted"})
		})

		// User: add source from registry.
		r.Post("/api/dossiers/{dossierID}/sources/from-registry/{regID}", func(w http.ResponseWriter, r *http.Request) {
			dossierID := chi.URLParam(r, "dossierID")
			regID := chi.URLParam(r, "regID")
			var name, url, sourceType, configJSON string
			var fetchInterval int64
			err := catalogDB.QueryRowContext(r.Context(),
				`SELECT name, url, source_type, config_json, fetch_interval FROM source_registry WHERE id = ? AND enabled = 1`, regID).
				Scan(&name, &url, &sourceType, &configJSON, &fetchInterval)
			if err != nil {
				writeError(w, 404, fmt.Errorf("source not found in registry"))
				return
			}
			src := &veille.Source{
				Name:          name,
				URL:           url,
				SourceType:    sourceType,
				FetchInterval: fetchInterval,
				Enabled:       true,
				ConfigJSON:    configJSON,
			}
			if err := svc.AddSource(r.Context(), dossierID, src); err != nil {
				switch {
				case errors.Is(err, veille.ErrDuplicateSource):
					writeError(w, 409, err)
				case errors.Is(err, veille.ErrInvalidInput),
					errors.Is(err, horosafe.ErrSSRF),
					errors.Is(err, horosafe.ErrPathTraversal),
					errors.Is(err, horosafe.ErrUnsafeScheme):
					writeError(w, 400, err)
				case errors.Is(err, veille.ErrQuotaExceeded):
					writeError(w, 429, err)
				default:
					writeError(w, 500, err)
				}
				return
			}
			writeJSON(w, 201, src)
		})

		// Sources.
		r.Post("/api/dossiers/{dossierID}/sources", func(w http.ResponseWriter, r *http.Request) {
			dossierID := chi.URLParam(r, "dossierID")
			var req struct {
				Name          string `json:"name"`
				URL           string `json:"url"`
				SourceType    string `json:"source_type"`
				FetchInterval int64  `json:"fetch_interval"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeError(w, 400, err)
				return
			}
			src := &veille.Source{
				Name:          req.Name,
				URL:           req.URL,
				SourceType:    req.SourceType,
				FetchInterval: req.FetchInterval,
				Enabled:       true,
			}
			if err := svc.AddSource(r.Context(), dossierID, src); err != nil {
				switch {
				case errors.Is(err, veille.ErrDuplicateSource):
					writeError(w, 409, err)
				case errors.Is(err, veille.ErrInvalidInput),
					errors.Is(err, horosafe.ErrSSRF),
					errors.Is(err, horosafe.ErrPathTraversal),
					errors.Is(err, horosafe.ErrUnsafeScheme):
					writeError(w, 400, err)
				case errors.Is(err, veille.ErrQuotaExceeded):
					writeError(w, 429, err)
				default:
					writeError(w, 500, err)
				}
				return
			}
			writeJSON(w, 201, src)
		})

		r.Get("/api/dossiers/{dossierID}/sources", func(w http.ResponseWriter, r *http.Request) {
			dossierID := chi.URLParam(r, "dossierID")
			sources, err := svc.ListSources(r.Context(), dossierID)
			if err != nil {
				writeError(w, 500, err)
				return
			}
			writeJSON(w, 200, sources)
		})

		r.Put("/api/dossiers/{dossierID}/sources/{id}", func(w http.ResponseWriter, r *http.Request) {
			dossierID := chi.URLParam(r, "dossierID")
			sourceID := chi.URLParam(r, "id")
			var req struct {
				Name          string `json:"name"`
				URL           string `json:"url"`
				Enabled       *bool  `json:"enabled"`
				FetchInterval int64  `json:"fetch_interval"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeError(w, 400, err)
				return
			}
			src := &veille.Source{
				ID:            sourceID,
				Name:          req.Name,
				URL:           req.URL,
				FetchInterval: req.FetchInterval,
			}
			if req.Enabled != nil {
				src.Enabled = *req.Enabled
			}
			if err := svc.UpdateSource(r.Context(), dossierID, src); err != nil {
				switch {
				case errors.Is(err, veille.ErrDuplicateSource):
					writeError(w, 409, err)
				case errors.Is(err, veille.ErrInvalidInput),
					errors.Is(err, horosafe.ErrSSRF),
					errors.Is(err, horosafe.ErrPathTraversal),
					errors.Is(err, horosafe.ErrUnsafeScheme):
					writeError(w, 400, err)
				default:
					writeError(w, 500, err)
				}
				return
			}
			writeJSON(w, 200, src)
		})

		r.Delete("/api/dossiers/{dossierID}/sources/{id}", func(w http.ResponseWriter, r *http.Request) {
			dossierID := chi.URLParam(r, "dossierID")
			sourceID := chi.URLParam(r, "id")
			if err := svc.DeleteSource(r.Context(), dossierID, sourceID); err != nil {
				writeError(w, 500, err)
				return
			}
			writeJSON(w, 200, map[string]string{"status": "deleted"})
		})

		r.Post("/api/dossiers/{dossierID}/sources/{id}/fetch", func(w http.ResponseWriter, r *http.Request) {
			dossierID := chi.URLParam(r, "dossierID")
			sourceID := chi.URLParam(r, "id")
			if err := svc.FetchNow(r.Context(), dossierID, sourceID); err != nil {
				writeError(w, 500, err)
				return
			}
			writeJSON(w, 200, map[string]string{"status": "fetched"})
		})

		r.Get("/api/dossiers/{dossierID}/sources/{id}/extractions", func(w http.ResponseWriter, r *http.Request) {
			dossierID := chi.URLParam(r, "dossierID")
			sourceID := chi.URLParam(r, "id")
			limit := queryInt(r, "limit", 50)
			exts, err := svc.ListExtractions(r.Context(), dossierID, sourceID, limit)
			if err != nil {
				writeError(w, 500, err)
				return
			}
			writeJSON(w, 200, exts)
		})

		r.Get("/api/dossiers/{dossierID}/sources/{id}/history", func(w http.ResponseWriter, r *http.Request) {
			dossierID := chi.URLParam(r, "dossierID")
			sourceID := chi.URLParam(r, "id")
			limit := queryInt(r, "limit", 50)
			hist, err := svc.FetchHistory(r.Context(), dossierID, sourceID, limit)
			if err != nil {
				writeError(w, 500, err)
				return
			}
			writeJSON(w, 200, hist)
		})

		// Search & chunks.
		r.Get("/api/dossiers/{dossierID}/search", func(w http.ResponseWriter, r *http.Request) {
			dossierID := chi.URLParam(r, "dossierID")
			q := r.URL.Query().Get("q")
			limit := queryInt(r, "limit", 20)
			results, err := svc.Search(r.Context(), dossierID, q, limit)
			if err != nil {
				writeError(w, 500, err)
				return
			}
			writeJSON(w, 200, results)
		})

		r.Get("/api/dossiers/{dossierID}/stats", func(w http.ResponseWriter, r *http.Request) {
			dossierID := chi.URLParam(r, "dossierID")
			stats, err := svc.Stats(r.Context(), dossierID)
			if err != nil {
				writeError(w, 500, err)
				return
			}
			writeJSON(w, 200, stats)
		})

		// Questions.
		r.Post("/api/dossiers/{dossierID}/questions", func(w http.ResponseWriter, r *http.Request) {
			dossierID := chi.URLParam(r, "dossierID")
			var req struct {
				Text        string `json:"text"`
				Keywords    string `json:"keywords"`
				Channels    string `json:"channels"`
				ScheduleMs  int64  `json:"schedule_ms"`
				MaxResults  int    `json:"max_results"`
				FollowLinks *bool  `json:"follow_links"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeError(w, 400, err)
				return
			}
			q := &veille.TrackedQuestion{
				Text:       req.Text,
				Keywords:   req.Keywords,
				Channels:   req.Channels,
				ScheduleMs: req.ScheduleMs,
				MaxResults: req.MaxResults,
				Enabled:    true,
			}
			if req.FollowLinks != nil {
				q.FollowLinks = *req.FollowLinks
			} else {
				q.FollowLinks = true
			}
			if err := svc.AddQuestion(r.Context(), dossierID, q); err != nil {
				writeError(w, 500, err)
				return
			}
			writeJSON(w, 201, q)
		})

		r.Get("/api/dossiers/{dossierID}/questions", func(w http.ResponseWriter, r *http.Request) {
			dossierID := chi.URLParam(r, "dossierID")
			questions, err := svc.ListQuestions(r.Context(), dossierID)
			if err != nil {
				writeError(w, 500, err)
				return
			}
			writeJSON(w, 200, questions)
		})

		r.Put("/api/dossiers/{dossierID}/questions/{id}", func(w http.ResponseWriter, r *http.Request) {
			dossierID := chi.URLParam(r, "dossierID")
			questionID := chi.URLParam(r, "id")
			var req struct {
				Text        string `json:"text"`
				Keywords    string `json:"keywords"`
				Channels    string `json:"channels"`
				ScheduleMs  int64  `json:"schedule_ms"`
				MaxResults  int    `json:"max_results"`
				FollowLinks *bool  `json:"follow_links"`
				Enabled     *bool  `json:"enabled"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeError(w, 400, err)
				return
			}
			q := &veille.TrackedQuestion{
				ID:         questionID,
				Text:       req.Text,
				Keywords:   req.Keywords,
				Channels:   req.Channels,
				ScheduleMs: req.ScheduleMs,
				MaxResults: req.MaxResults,
			}
			if req.FollowLinks != nil {
				q.FollowLinks = *req.FollowLinks
			}
			if req.Enabled != nil {
				q.Enabled = *req.Enabled
			}
			if err := svc.UpdateQuestion(r.Context(), dossierID, q); err != nil {
				writeError(w, 500, err)
				return
			}
			writeJSON(w, 200, q)
		})

		r.Delete("/api/dossiers/{dossierID}/questions/{id}", func(w http.ResponseWriter, r *http.Request) {
			dossierID := chi.URLParam(r, "dossierID")
			questionID := chi.URLParam(r, "id")
			if err := svc.DeleteQuestion(r.Context(), dossierID, questionID); err != nil {
				writeError(w, 500, err)
				return
			}
			writeJSON(w, 200, map[string]string{"status": "deleted"})
		})

		r.Post("/api/dossiers/{dossierID}/questions/{id}/run", func(w http.ResponseWriter, r *http.Request) {
			dossierID := chi.URLParam(r, "dossierID")
			questionID := chi.URLParam(r, "id")
			count, err := svc.RunQuestionNow(r.Context(), dossierID, questionID)
			if err != nil {
				writeError(w, 500, err)
				return
			}
			writeJSON(w, 200, map[string]any{"status": "ok", "new_results": count})
		})

		r.Get("/api/dossiers/{dossierID}/questions/{id}/results", func(w http.ResponseWriter, r *http.Request) {
			dossierID := chi.URLParam(r, "dossierID")
			questionID := chi.URLParam(r, "id")
			limit := queryInt(r, "limit", 50)
			results, err := svc.QuestionResults(r.Context(), dossierID, questionID, limit)
			if err != nil {
				writeError(w, 500, err)
				return
			}
			writeJSON(w, 200, results)
		})
	})

	// HTTP server.
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		slog.Info("server starting", "port", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown", "error", err)
	}
	slog.Info("server stopped")
}

// --- Auth middleware ---

// requireSession returns 401 JSON if no valid JWT claims in context.
// Used on API routes. auth.Middleware (applied globally) does the soft parsing.
func requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth.GetClaims(r.Context()) == nil {
			writeJSON(w, 401, map[string]string{"error": "non authentifie"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := auth.GetClaims(r.Context())
		if c == nil || c.Role != "admin" {
			writeJSON(w, 403, map[string]string{"error": "admin requis"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- User DB operations ---

func migrateAuthColumns(db *sql.DB) error {
	cols := []struct{ name, ddl string }{
		{"email", "ALTER TABLE users ADD COLUMN email TEXT DEFAULT ''"},
		{"password_hash", "ALTER TABLE users ADD COLUMN password_hash TEXT DEFAULT ''"},
		{"role", "ALTER TABLE users ADD COLUMN role TEXT DEFAULT 'user'"},
	}
	for _, c := range cols {
		var count int
		err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('users') WHERE name = ?`, c.name).Scan(&count)
		if err != nil {
			return err
		}
		if count == 0 {
			if _, err := db.Exec(c.ddl); err != nil {
				return fmt.Errorf("add column %s: %w", c.name, err)
			}
		}
	}
	_, _ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email ON users(email) WHERE email != ''`)
	return nil
}

func seedAdmin(ctx context.Context, db *sql.DB) error {
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role = 'admin' AND status = 'active'`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("admin123!!!"), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	id := idgen.New()
	_, err = db.ExecContext(ctx,
		`INSERT INTO users (id, name, email, password_hash, role, status, created_at) VALUES (?, ?, ?, ?, 'admin', 'active', ?)`,
		id, "admin", "admin", string(hash), time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("seed admin: %w", err)
	}
	slog.Info("admin user seeded", "email", "admin", "id", id)
	return nil
}

type userService struct {
	db   *sql.DB
	pool *tenant.Pool
}

func (s *userService) authenticate(ctx context.Context, email, password string) (*auth.HorosClaims, error) {
	var userID, name, role, hash string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, role, password_hash FROM users WHERE email = ? AND status = 'active'`, email).
		Scan(&userID, &name, &role, &hash)
	if err != nil {
		return nil, fmt.Errorf("user not found")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return nil, fmt.Errorf("wrong password")
	}
	return &auth.HorosClaims{
		UserID:   userID,
		Username: name,
		Role:     role,
		Email:    email,
	}, nil
}

func (s *userService) listUsers(ctx context.Context) ([]map[string]any, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, email, role, status, created_at FROM users ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []map[string]any
	for rows.Next() {
		var id, name, email, role, status string
		var createdAt int64
		if err := rows.Scan(&id, &name, &email, &role, &status, &createdAt); err != nil {
			return nil, err
		}
		users = append(users, map[string]any{
			"id": id, "name": name, "email": email,
			"role": role, "status": status, "created_at": createdAt,
		})
	}
	if users == nil {
		users = []map[string]any{}
	}
	return users, rows.Err()
}

func (s *userService) createUser(ctx context.Context, email, name, password, role string) (map[string]string, error) {
	if email == "" || password == "" {
		return nil, fmt.Errorf("email et mot de passe requis")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	id := idgen.New()
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO users (id, name, email, password_hash, role, status, created_at) VALUES (?, ?, ?, ?, ?, 'active', ?)`,
		id, name, email, string(hash), role, time.Now().UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("creation utilisateur: %w", err)
	}
	// Shard (dossier) creation is separate from user creation.
	// Use POST /api/dossiers to create a dossier for this user.
	return map[string]string{"id": id, "name": name, "email": email, "role": role}, nil
}

func (s *userService) deleteUser(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET status = 'deleted' WHERE id = ?`, userID)
	return err
}

// --- Helpers ---

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func queryInt(r *http.Request, key string, def int) int {
	s := r.URL.Query().Get(key)
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}


// --- Global tables migration ---

func migrateGlobalTables(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS global_search_engines (
			id            TEXT PRIMARY KEY,
			name          TEXT NOT NULL UNIQUE,
			strategy      TEXT NOT NULL DEFAULT 'api',
			url_template  TEXT NOT NULL,
			api_config    TEXT NOT NULL DEFAULT '{}',
			selectors     TEXT NOT NULL DEFAULT '{}',
			rate_limit_ms INTEGER NOT NULL DEFAULT 2000,
			max_pages     INTEGER NOT NULL DEFAULT 3,
			enabled       INTEGER NOT NULL DEFAULT 1,
			created_at    INTEGER NOT NULL,
			updated_at    INTEGER NOT NULL
		);
		CREATE TABLE IF NOT EXISTS source_registry (
			id             TEXT PRIMARY KEY,
			name           TEXT NOT NULL,
			url            TEXT NOT NULL UNIQUE,
			source_type    TEXT NOT NULL DEFAULT 'rss',
			category       TEXT NOT NULL DEFAULT '',
			config_json    TEXT NOT NULL DEFAULT '{}',
			description    TEXT NOT NULL DEFAULT '',
			fetch_interval INTEGER NOT NULL DEFAULT 3600000,
			enabled        INTEGER NOT NULL DEFAULT 1,
			created_at     INTEGER NOT NULL,
			updated_at     INTEGER NOT NULL
		);
	`)
	return err
}

func seedGlobalEngines(ctx context.Context, db *sql.DB) {
	var count int
	db.QueryRowContext(ctx, `SELECT COUNT(*) FROM global_search_engines`).Scan(&count)
	if count > 0 {
		return
	}
	insertFn := func(ctx context.Context, e *catalog.SearchEngineInput) error {
		now := time.Now().UnixMilli()
		enabled := 0
		if e.Enabled {
			enabled = 1
		}
		_, err := db.ExecContext(ctx,
			`INSERT OR IGNORE INTO global_search_engines (id, name, strategy, url_template, api_config, selectors, rate_limit_ms, max_pages, enabled, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			e.ID, e.Name, e.Strategy, e.URLTemplate, e.APIConfigJSON, e.SelectorsJSON,
			e.RateLimitMs, e.MaxPages, enabled, now, now)
		return err
	}
	n, err := catalog.PopulateSearchEngines(ctx, insertFn)
	if err != nil {
		slog.Warn("seed global engines", "error", err)
	} else if n > 0 {
		slog.Info("seeded global engines", "count", n)
	}

	// Seed source registry from catalog.
	var regCount int
	db.QueryRowContext(ctx, `SELECT COUNT(*) FROM source_registry`).Scan(&regCount)
	if regCount > 0 {
		return
	}
	now := time.Now().UnixMilli()
	for _, cat := range catalog.Categories() {
		defs, _ := catalog.Sources(cat)
		for _, def := range defs {
			interval := def.Interval
			if interval == 0 {
				interval = 3600000
			}
			configJSON := def.ConfigJSON
			if configJSON == "" {
				configJSON = "{}"
			}
			id := idgen.New()
			db.ExecContext(ctx,
				`INSERT OR IGNORE INTO source_registry (id, name, url, source_type, category, config_json, fetch_interval, enabled, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
				id, def.Name, def.URL, def.SourceType, cat, configJSON, interval, now, now)
		}
	}
	slog.Info("seeded source registry from catalog")
}

// --- Admin helpers ---

func listGlobalEngines(ctx context.Context, db *sql.DB) ([]map[string]any, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, name, strategy, url_template, api_config, selectors, rate_limit_ms, max_pages, enabled, created_at, updated_at
		FROM global_search_engines ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var engines []map[string]any
	for rows.Next() {
		var id, name, strategy, urlTemplate, apiConfig, selectors string
		var rateLimitMs int64
		var maxPages, enabled int
		var createdAt, updatedAt int64
		if err := rows.Scan(&id, &name, &strategy, &urlTemplate, &apiConfig, &selectors,
			&rateLimitMs, &maxPages, &enabled, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		engines = append(engines, map[string]any{
			"id": id, "name": name, "strategy": strategy, "url_template": urlTemplate,
			"api_config": apiConfig, "selectors": selectors, "rate_limit_ms": rateLimitMs,
			"max_pages": maxPages, "enabled": enabled != 0, "created_at": createdAt, "updated_at": updatedAt,
		})
	}
	if engines == nil {
		engines = []map[string]any{}
	}
	return engines, rows.Err()
}

func listSourceRegistry(ctx context.Context, db *sql.DB) ([]map[string]any, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, name, url, source_type, category, config_json, description, fetch_interval, enabled, created_at, updated_at
		FROM source_registry ORDER BY category, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []map[string]any
	for rows.Next() {
		var id, name, url, sourceType, category, configJSON, description string
		var fetchInterval int64
		var enabled int
		var createdAt, updatedAt int64
		if err := rows.Scan(&id, &name, &url, &sourceType, &category, &configJSON, &description,
			&fetchInterval, &enabled, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, map[string]any{
			"id": id, "name": name, "url": url, "source_type": sourceType,
			"category": category, "config_json": configJSON, "description": description,
			"fetch_interval": fetchInterval, "enabled": enabled != 0,
			"created_at": createdAt, "updated_at": updatedAt,
		})
	}
	if entries == nil {
		entries = []map[string]any{}
	}
	return entries, rows.Err()
}

func buildOverview(ctx context.Context, catalogDB *sql.DB, pool *tenant.Pool, svc *veille.Service) (map[string]any, error) {
	// List all users.
	userRows, err := catalogDB.QueryContext(ctx,
		`SELECT id, name, email, role, status FROM users WHERE status = 'active' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer userRows.Close()

	type userInfo struct {
		ID, Name, Email, Role string
	}
	var users []userInfo
	for userRows.Next() {
		var u userInfo
		var status string
		if err := userRows.Scan(&u.ID, &u.Name, &u.Email, &u.Role, &status); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	if err := userRows.Err(); err != nil {
		return nil, err
	}

	// List all shards.
	shardRows, err := catalogDB.QueryContext(ctx,
		`SELECT id, name FROM shards WHERE status = 'active'`)
	if err != nil {
		return nil, err
	}
	defer shardRows.Close()

	type shardEntry struct {
		DossierID string         `json:"dossier_id"`
		Name      string         `json:"name"`
		Stats     map[string]any `json:"stats"`
	}
	var shards []shardEntry
	for shardRows.Next() {
		var s shardEntry
		if err := shardRows.Scan(&s.DossierID, &s.Name); err != nil {
			return nil, err
		}
		// Try to get stats for each shard.
		stats, err := svc.Stats(ctx, s.DossierID)
		if err == nil && stats != nil {
			s.Stats = map[string]any{
				"sources":     stats.Sources,
				"extractions": stats.Extractions,
			}
		} else {
			s.Stats = map[string]any{}
		}
		shards = append(shards, s)
	}
	if err := shardRows.Err(); err != nil {
		return nil, err
	}

	var userList []map[string]any
	for _, u := range users {
		userList = append(userList, map[string]any{
			"id": u.ID, "name": u.Name, "email": u.Email, "role": u.Role,
		})
	}
	if userList == nil {
		userList = []map[string]any{}
	}
	if shards == nil {
		shards = []shardEntry{}
	}

	return map[string]any{
		"users":  userList,
		"shards": shards,
	}, nil
}

// migrateExistingShards applies the veille schema to all existing shard databases.
// This is idempotent (CREATE IF NOT EXISTS) and ensures new tables like
// extractions_fts are added to shards created before the schema change.
func migrateExistingShards(ctx context.Context, catalogDB *sql.DB, pool *tenant.Pool) {
	rows, err := catalogDB.QueryContext(ctx,
		`SELECT id FROM shards WHERE status = 'active'`)
	if err != nil {
		slog.Warn("migrate shards: list", "error", err)
		return
	}
	defer rows.Close()

	var migrated int
	for rows.Next() {
		var dossierID string
		if err := rows.Scan(&dossierID); err != nil {
			continue
		}
		db, err := pool.Resolve(ctx, dossierID)
		if err != nil {
			slog.Warn("migrate shards: resolve", "dossier_id", dossierID, "error", err)
			continue
		}
		if err := veille.ApplySchema(db); err != nil {
			slog.Warn("migrate shards: apply", "dossier_id", dossierID, "error", err)
			continue
		}
		migrated++
	}
	if migrated > 0 {
		slog.Info("migrated existing shards", "count", migrated)
	}
}
