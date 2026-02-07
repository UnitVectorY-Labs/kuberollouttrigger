package web

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/UnitVectorY-Labs/kuberollouttrigger/internal/oidc"
	"github.com/UnitVectorY-Labs/kuberollouttrigger/internal/payload"
	"github.com/UnitVectorY-Labs/kuberollouttrigger/internal/valkey"
)

const maxPayloadSize = 1 << 20 // 1MB

// Server is the HTTP server for web mode.
type Server struct {
	validator    *oidc.Validator
	publisher    *valkey.Publisher
	imagePrefix  string
	logger       *slog.Logger
	publishCount atomic.Int64
}

// NewServer creates a new web mode HTTP server.
func NewServer(validator *oidc.Validator, publisher *valkey.Publisher, imagePrefix string, logger *slog.Logger) *Server {
	return &Server{
		validator:   validator,
		publisher:   publisher,
		imagePrefix: imagePrefix,
		logger:      logger,
	}
}

// Handler returns the HTTP handler with all routes configured.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /event", s.handleEvent)
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	return mux
}

func generateRequestID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(b)
}

func (s *Server) handleEvent(w http.ResponseWriter, r *http.Request) {
	requestID := generateRequestID()
	logger := s.logger.With("request_id", requestID)

	// Validate Content-Type
	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		logger.Warn("invalid content type", "content_type", ct)
		http.Error(w, "Content-Type must be application/json", http.StatusBadRequest)
		return
	}

	// Extract and validate Bearer token
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
		logger.Warn("missing or invalid authorization header")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	tokenString := strings.TrimPrefix(authHeader, "Bearer ")

	// Validate OIDC token
	claims, err := s.validator.ValidateToken(tokenString)
	if err != nil {
		logger.Warn("OIDC token validation failed", "error", err.Error())
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	logger.Info("authenticated request",
		"repository_owner", claims.RepositoryOwner,
		"repository", claims.Repository,
	)

	// Read and validate payload
	body, err := io.ReadAll(io.LimitReader(r.Body, maxPayloadSize))
	if err != nil {
		logger.Error("failed to read request body", "error", err)
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	evt, err := payload.ParseAndValidate(body, s.imagePrefix)
	if err != nil {
		logger.Warn("payload validation failed", "error", err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Serialize to minimal JSON for publishing
	jsonBytes, err := evt.ToJSON()
	if err != nil {
		logger.Error("failed to serialize event", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Publish to Valkey
	if err := s.publisher.Publish(r.Context(), string(jsonBytes)); err != nil {
		logger.Error("failed to publish to Valkey", "error", err)
		http.Error(w, "Service unavailable", http.StatusBadGateway)
		return
	}

	count := s.publishCount.Add(1)
	logger.Info("event published",
		"image", evt.Image,
		"tag", evt.Tag,
		"total_published", count,
	)

	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}
