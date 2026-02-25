// CLAUDE:SUMMARY Wraps horosvec ANN index with SQLite/dbopen management, MCP tools, and connectivity handlers.
// Package vecbridge wraps horosvec (Vamana+RaBitQ ANN index) with HOROS
// integration layers: MCP tools, connectivity handlers, and dbopen management.
//
// horosvec is a standalone pure-Go ANN engine. vecbridge is the thin glue that
// exposes it to the HOROS ecosystem via MCP and connectivity, without modifying
// horosvec internals.
//
// Usage:
//
//	svc, err := vecbridge.New(vecbridge.Config{
//	    DBPath: "/data/vec.db",
//	})
//	defer svc.Close()
//	svc.RegisterMCP(mcpServer)
//	svc.RegisterConnectivity(router)
package vecbridge

import (
	"database/sql"
	"fmt"
	"log/slog"
	"math"

	"github.com/hazyhaar/pkg/dbopen"
	"github.com/hazyhaar/horosvec"
)

// Config configures the vecbridge service.
type Config struct {
	// DBPath is the SQLite database file for the vector index.
	DBPath string `json:"db_path" yaml:"db_path"`

	// Horosvec contains the Vamana+RaBitQ engine configuration.
	Horosvec horosvec.Config `json:"horosvec" yaml:"horosvec"`

	// CacheSize sets PRAGMA cache_size for SQLite. Default: -512000 (512 MB).
	CacheSize int `json:"cache_size" yaml:"cache_size"`

	// Logger for debug/error messages.
	Logger *slog.Logger `json:"-" yaml:"-"`
}

func (c *Config) defaults() {
	if c.CacheSize == 0 {
		c.CacheSize = -512000
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
}

// Service wraps a horosvec.Index with HOROS integration.
type Service struct {
	Index  *horosvec.Index
	db     *sql.DB
	logger *slog.Logger
}

// New opens the SQLite database and creates or loads a horosvec Index.
func New(cfg Config) (*Service, error) {
	cfg.defaults()

	db, err := dbopen.Open(cfg.DBPath,
		dbopen.WithMkdirAll(),
		dbopen.WithCacheSize(cfg.CacheSize),
	)
	if err != nil {
		return nil, err
	}

	idx, err := horosvec.New(db, cfg.Horosvec)
	if err != nil {
		db.Close()
		return nil, err
	}

	return &Service{
		Index:  idx,
		db:     db,
		logger: cfg.Logger,
	}, nil
}

// NewFromDB creates a Service from an existing *sql.DB (e.g. shared with another component).
func NewFromDB(db *sql.DB, cfg horosvec.Config, logger *slog.Logger) (*Service, error) {
	if logger == nil {
		logger = slog.Default()
	}
	idx, err := horosvec.New(db, cfg)
	if err != nil {
		return nil, err
	}
	return &Service{
		Index:  idx,
		db:     db,
		logger: logger,
	}, nil
}

// Close closes the horosvec index. If the DB was opened by New, it is also closed.
func (s *Service) Close() error {
	return s.Index.Close()
}

// loadVector reads a raw vector from the vec_nodes table by ext_id.
func (s *Service) loadVector(extID []byte) ([]float32, error) {
	var blob []byte
	err := s.db.QueryRow("SELECT vector FROM vec_nodes WHERE ext_id = ?", extID).Scan(&blob)
	if err != nil {
		return nil, fmt.Errorf("load vector for ext_id: %w", err)
	}
	return deserializeFloat32s(blob), nil
}

// deserializeFloat32s converts a little-endian byte slice to float32 slice.
func deserializeFloat32s(blob []byte) []float32 {
	n := len(blob) / 4
	vec := make([]float32, n)
	for i := 0; i < n; i++ {
		bits := uint32(blob[i*4]) | uint32(blob[i*4+1])<<8 | uint32(blob[i*4+2])<<16 | uint32(blob[i*4+3])<<24
		vec[i] = math.Float32frombits(bits)
	}
	return vec
}
