package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"tf-mirror/internal/common"

	"github.com/gorilla/mux"
)

// Server represents the HTTP server for the Terraform registry mirror
type Server struct {
	config     *common.ServerConfig
	logger     *common.Logger
	httpServer *http.Server
	router     *mux.Router
	metrics    *Metrics
}

// NewServer creates a new registry mirror server
func NewServer(config *common.ServerConfig, logger *common.Logger) *Server {
	server := &Server{
		config:  config,
		logger:  logger,
		metrics: NewMetrics(),
	}

	server.setupRoutes()
	return server
}

// setupRoutes configures the HTTP routes
func (s *Server) setupRoutes() {
	s.router = mux.NewRouter()

	// Health check endpoint
	s.router.HandleFunc("/health", s.handleHealth).Methods("GET")

	// Version endpoint
	s.router.HandleFunc("/version", s.handleVersion).Methods("GET")

	// Metrics endpoint
	s.router.HandleFunc("/metrics", s.handleMetrics).Methods("GET")

	// Static file serving for provider binaries
	s.router.PathPrefix("/").Handler(http.StripPrefix("/", http.FileServer(http.Dir(s.config.DataPath))))

	// Add middlewares
	s.router.Use(s.loggingMiddleware)
	s.router.Use(s.metricsMiddleware)
}

// Start starts the HTTP server
func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%d", s.config.ListenHost, s.config.ListenPort)

	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      s.router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	if s.config.EnableTLS {
		s.logger.Info("Starting HTTPS server on %s", addr)

		// Load TLS configuration
		cert, err := tls.LoadX509KeyPair(s.config.TLSCert, s.config.TLSKey)
		if err != nil {
			return fmt.Errorf("failed to load TLS certificate: %w", err)
		}

		s.httpServer.TLSConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
		}

		return s.httpServer.ListenAndServeTLS("", "")
	} else {
		s.logger.Info("Starting HTTP server on %s", addr)
		return s.httpServer.ListenAndServe()
	}
}

// Stop gracefully stops the HTTP server
func (s *Server) Stop(ctx context.Context) error {
	s.logger.Info("Shutting down server...")
	return s.httpServer.Shutdown(ctx)
}

// handleProviderList handles the /providers endpoint
func (s *Server) handleProviderList(w http.ResponseWriter, r *http.Request) {
	providers, err := s.scanProviders()
	if err != nil {
		s.logger.Error("Failed to scan providers: %v", err)
		s.writeErrorResponse(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	response := common.ProviderList{
		Providers: providers,
	}

	s.writeJSONResponse(w, response)
}

// handleHealth handles the /health endpoint
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	health := map[string]any{
		"status":    "healthy",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"version":   common.GetVersionString(),
	}

	// Check if data directory is accessible
	if _, err := os.Stat(s.config.DataPath); os.IsNotExist(err) {
		w.WriteHeader(http.StatusServiceUnavailable)
		health["status"] = "unhealthy"
		health["error"] = "data directory not accessible"
	}

	s.writeJSONResponse(w, health)
}

// handleVersion handles the /version endpoint
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	s.writeJSONResponse(w, common.GetVersionInfo())
}

// scanProviders scans the data directory for available providers
func (s *Server) scanProviders() ([]common.ProviderListItem, error) {
	var providers []common.ProviderListItem
	providerMap := make(map[string]bool)

	err := filepath.Walk(s.config.DataPath+"/registry.terraform.io", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		if !info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(s.config.DataPath+"/registry.terraform.io", path)
		if err != nil {
			return nil
		}

		parts := strings.Split(filepath.Clean(relPath), string(filepath.Separator))
		if len(parts) >= 2 && parts[0] != "." {
			namespace := parts[0]
			name := parts[1]
			providerKey := fmt.Sprintf("%s/%s", namespace, name)

			if !providerMap[providerKey] {
				providers = append(providers, common.ProviderListItem{
					Namespace: namespace,
					Name:      name,
				})
				providerMap[providerKey] = true
			}
		}

		return nil
	})

	return providers, err
}

// writeJSONResponse writes a JSON response
func (s *Server) writeJSONResponse(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.logger.Error("Failed to encode JSON response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// writeErrorResponse writes an error response
func (s *Server) writeErrorResponse(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	errorResponse := common.ErrorResponse{
		Errors: []common.ErrorDetail{
			{
				Status: strconv.Itoa(statusCode),
				Detail: message,
			},
		},
	}

	json.NewEncoder(w).Encode(errorResponse)
}

// loggingMiddleware logs HTTP requests
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create a response writer wrapper to capture status code
		wrapped := &responseWriterWrapper{ResponseWriter: w}

		next.ServeHTTP(wrapped, r)

		duration := time.Since(start)
		s.logger.Info("%s %s %d %v %s", r.Method, r.RequestURI, wrapped.statusCode, duration, r.RemoteAddr)
	})
}

// responseWriterWrapper wraps http.ResponseWriter to capture status code
type responseWriterWrapper struct {
	http.ResponseWriter
	statusCode int
}

func (w *responseWriterWrapper) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *responseWriterWrapper) Write(data []byte) (int, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	return w.ResponseWriter.Write(data)
}
