package chassis

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
)

// Service représente un service enregistrable (ingest, rag, think)
type Service interface {
	RegisterHTTP(r chi.Router)
	RegisterMCP(server *mcp.Server) error
}

// Server est le châssis unifié QUIC/HTTP3 pour HOROS 47
type Server struct {
	addr       string
	logger     *slog.Logger
	services   map[string]Service
	httpRouter *chi.Mux
	mcpServer  *mcp.Server
	quicServer *http3.Server
	mu         sync.RWMutex
}

// NewServer crée un nouveau serveur unifié
func NewServer(logger *slog.Logger, addr string) *Server {
	if logger == nil {
		logger = slog.Default()
	}

	// Router Chi pour HTTP
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)

	// Serveur MCP (TODO: adapter API correcte)
	// mcpServer := mcp.NewServer(nil)
	var mcpServer *mcp.Server = nil

	return &Server{
		addr:       addr,
		logger:     logger,
		services:   make(map[string]Service),
		httpRouter: r,
		mcpServer:  mcpServer,
	}
}

// RegisterService enregistre un service avec ses endpoints HTTP et MCP
func (s *Server) RegisterService(name string, svc Service) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.services[name]; exists {
		return fmt.Errorf("service %s already registered", name)
	}

	s.logger.Info("Registering service", "name", name)

	// Enregistrer endpoints HTTP
	svc.RegisterHTTP(s.httpRouter)

	// Enregistrer tools MCP (TODO: adapter API)
	// if err := svc.RegisterMCP(s.mcpServer); err != nil {
	// 	return fmt.Errorf("failed to register MCP tools for %s: %w", name, err)
	// }

	s.services[name] = svc
	return nil
}

// Start démarre le serveur QUIC avec dual transport HTTP3/MCP
func (s *Server) Start(ctx context.Context) error {
	s.logger.Info("Starting HOROS 47 Unified Server", "addr", s.addr)

	// Générer certificat TLS (dev mode autosigné)
	tlsConfig, err := s.generateTLSConfig()
	if err != nil {
		return fmt.Errorf("failed to generate TLS config: %w", err)
	}

	// Configuration QUIC
	quicConfig := &quic.Config{
		MaxIdleTimeout:  0, // Pas de timeout idle
		KeepAlivePeriod: 0, // Pas de keepalive actif
	}

	// Créer serveur HTTP3/QUIC
	s.quicServer = &http3.Server{
		Addr:       s.addr,
		Handler:    s.httpRouter,
		TLSConfig:  tlsConfig,
		QUICConfig: quicConfig,
	}

	s.logger.Info("Server started", "addr", s.addr, "services", len(s.services))

	// Démarrer listener (bloquant)
	if err := s.quicServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}

// Stop arrête proprement le serveur
func (s *Server) Stop(ctx context.Context) error {
	s.logger.Info("Stopping server")

	if s.quicServer != nil {
		if err := s.quicServer.Close(); err != nil {
			return fmt.Errorf("failed to stop server: %w", err)
		}
	}

	s.logger.Info("Server stopped")
	return nil
}

// generateTLSConfig génère certificat TLS autosigné pour développement
func (s *Server) generateTLSConfig() (*tls.Config, error) {
	s.logger.Info("Generating self-signed TLS certificate")
	return NewDevelopmentTLSConfig()
}
