package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/naicoi92/forward-auth-redis/internal/auth"
	"github.com/naicoi92/forward-auth-redis/internal/config"
	"github.com/naicoi92/forward-auth-redis/internal/cookiex"
	"github.com/naicoi92/forward-auth-redis/internal/httpapi"
	"github.com/naicoi92/forward-auth-redis/internal/redisx"
	"github.com/naicoi92/forward-auth-redis/internal/store"
	"github.com/naicoi92/forward-auth-redis/internal/webui"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	redisClients, err := redisx.New(cfg)
	if err != nil {
		return fmt.Errorf("init redis: %w", err)
	}
	defer redisClients.Close()

	totpStore := store.NewTOTPStore(redisClients.Reader)
	sessionStore := store.NewSessionStore(redisClients.Writer, redisClients.Reader, cfg)
	loginGuard := store.NewLoginGuard(redisClients.Writer, cfg)
	jwt := auth.NewJWT(cfg.JWTSecret)
	svc := auth.NewService(cfg, totpStore, sessionStore, loginGuard, jwt)
	cookie := cookiex.New(cfg)
	templates, err := webui.Load()
	if err != nil {
		return fmt.Errorf("load templates: %w", err)
	}

	handler := httpapi.New(cfg, svc, cookie, redisClients, templates)

	srv := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      handler.Router(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("starting server", "addr", cfg.ListenAddr, "base_path", cfg.BasePath)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	case sig := <-sigCh:
		slog.Info("shutting down", "signal", sig.String())
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		return nil
	}
}
