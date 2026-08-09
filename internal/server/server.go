package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"grok-web-to-api/internal/config"
	"grok-web-to-api/internal/grok"
	"grok-web-to-api/internal/openai"

	"go.uber.org/zap"
)

type Server struct {
	cfg    *config.Config
	log    *zap.Logger
	client *grok.Client
	http   *http.Server

	// naive in-memory rate limit (optional)
	rlMu     sync.Mutex
	rlWindow time.Time
	rlCount  int
}

func New(cfg *config.Config, log *zap.Logger, client *grok.Client) *Server {
	return &Server{cfg: cfg, log: log, client: client}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	h := openai.NewHandler(s.client, s.log)

	mux.HandleFunc("/health", s.health)
	mux.HandleFunc("/openai/v1/models", h.Models)
	mux.HandleFunc("/v1/models", h.Models)
	mux.HandleFunc("/openai/v1/chat/completions", h.ChatCompletions)
	mux.HandleFunc("/v1/chat/completions", h.ChatCompletions)
	mux.HandleFunc("/openai/v1/images/generations", h.ImageGenerations)
	mux.HandleFunc("/v1/images/generations", h.ImageGenerations)

	return s.withMiddleware(mux)
}

func (s *Server) withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// CORS
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		if s.cfg.RateLimit.Enabled {
			if !s.allow() {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": map[string]string{
						"message": "rate limit exceeded",
						"type":    "rate_limit_error",
					},
				})
				return
			}
		}

		s.log.Debug("request", zap.String("method", r.Method), zap.String("path", r.URL.Path))
		next.ServeHTTP(w, r)
	})
}

func (s *Server) allow() bool {
	s.rlMu.Lock()
	defer s.rlMu.Unlock()
	now := time.Now()
	window := time.Duration(s.cfg.RateLimit.WindowMs) * time.Millisecond
	if s.rlWindow.IsZero() || now.Sub(s.rlWindow) > window {
		s.rlWindow = now
		s.rlCount = 1
		return true
	}
	s.rlCount++
	return s.rlCount <= s.cfg.RateLimit.MaxRequests
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	_ = r
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":      "ok",
		"service":     "grok-web-to-api",
		"grok_ready":  s.client.IsHealthy(),
		"has_cookies": s.client.CookieStore().HasAuth(),
		"user_id":     s.client.UserID(),
		// REST session/modes + non-stream WS chat
		"reverse": "rest_ok_chat_ws_image_gen_wired",
	})
}

func (s *Server) Start() error {
	addr := ":" + s.cfg.Server.Port
	s.http = &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	s.log.Info("starting server", zap.String("addr", addr))
	return s.http.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.http == nil {
		return nil
	}
	s.log.Info("shutting down")
	return s.http.Shutdown(ctx)
}

func (s *Server) Addr() string {
	if s.http == nil {
		return fmt.Sprintf(":%s", s.cfg.Server.Port)
	}
	return s.http.Addr
}
