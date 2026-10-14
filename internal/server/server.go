package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/wishmatic/go-mcp/internal/auth"
	"github.com/wishmatic/go-mcp/internal/config"
	mcpServer "github.com/wishmatic/go-mcp/internal/mcp"
	"go.uber.org/zap"
)

const writeTimeout = 60 * time.Second

type Server struct {
	cfg    config.Config
	log    *zap.Logger
	router *chi.Mux
	http   *http.Server
}

func New(cfg config.Config, log *zap.Logger) (*Server, error) {
	if cfg.APIKey == "" {
		return nil, auth.ErrNoAPIKey
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	mcpSrv, err := mcpServer.New(mcpServer.Deps{Log: log})
	if err != nil {
		return nil, fmt.Errorf("build mcp server: %w", err)
	}

	router := chi.NewRouter()

	router.Use(middleware.RequestID)
	router.Use(middleware.ClientIPFromRemoteAddr)
	router.Use(middleware.Recoverer)
	router.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodOptions},
		AllowedHeaders:   []string{"Accept", "Content-Type", "Authorization"},
		ExposedHeaders:   []string{},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return mcpSrv
	}, nil)

	router.Mount("/mcp", auth.Middleware(log, cfg.APIKey)(handler))

	router.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	return &Server{
		cfg:    cfg,
		log:    log,
		router: router,
		http: &http.Server{
			Addr:              cfg.Addr(),
			Handler:           router,
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      writeTimeout,
			IdleTimeout:       60 * time.Second,
		},
	}, nil
}

func (s *Server) Run() error {
	s.log.Info("server listening", zap.String("addr", s.cfg.Addr()))

	if err := s.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}
