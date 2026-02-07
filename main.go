package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/UnitVectorY-Labs/kuberollouttrigger/internal/config"
	"github.com/UnitVectorY-Labs/kuberollouttrigger/internal/k8s"
	"github.com/UnitVectorY-Labs/kuberollouttrigger/internal/oidc"
	"github.com/UnitVectorY-Labs/kuberollouttrigger/internal/payload"
	"github.com/UnitVectorY-Labs/kuberollouttrigger/internal/valkey"
	"github.com/UnitVectorY-Labs/kuberollouttrigger/internal/web"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <web|worker> [flags]\n", os.Args[0])
		os.Exit(1)
	}

	subcommand := os.Args[1]
	args := os.Args[2:]

	switch subcommand {
	case "web":
		if err := runWeb(args); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "worker":
		if err := runWorker(args); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown command %q. Usage: %s <web|worker> [flags]\n", subcommand, os.Args[0])
		os.Exit(1)
	}
}

func runWeb(args []string) error {
	cfg, err := config.ParseWebConfig(args)
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: config.ParseLogLevel(cfg.LogLevel),
	}))
	cfg.LogSummary(logger)

	if cfg.DevMode {
		logger.Warn("DEV MODE ENABLED: OIDC signature verification is disabled. Do not use in production.")
	}

	// Initialize OIDC validator
	validator := oidc.NewValidator(cfg.GithubOIDCAudience, cfg.GithubAllowedOrg, cfg.DevMode, logger)

	// Initialize Valkey publisher
	publisher := valkey.NewPublisher(cfg.CommonConfig.NewRedisOptions(), cfg.ValkeyChannel, logger)
	defer publisher.Close()

	// Test Valkey connectivity
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := publisher.Ping(ctx); err != nil {
		return fmt.Errorf("failed to connect to Valkey at %s: %w", cfg.ValkeyAddr, err)
	}
	logger.Info("connected to Valkey", "addr", cfg.ValkeyAddr)

	// Initialize web server
	server := web.NewServer(validator, publisher, cfg.AllowedImagePrefix, logger)
	httpServer := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      server.Handler(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		logger.Info("shutting down web server")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		httpServer.Shutdown(shutdownCtx)
	}()

	logger.Info("starting web server", "addr", cfg.ListenAddr)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("HTTP server error: %w", err)
	}

	return nil
}

func runWorker(args []string) error {
	cfg, err := config.ParseWorkerConfig(args)
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: config.ParseLogLevel(cfg.LogLevel),
	}))
	cfg.LogSummary(logger)

	// Initialize Kubernetes restarter
	restarter, err := k8s.NewRestarter(cfg.Kubeconfig, logger)
	if err != nil {
		return fmt.Errorf("failed to initialize Kubernetes client: %w", err)
	}

	// Initialize Valkey subscriber
	subscriber := valkey.NewSubscriber(cfg.CommonConfig.NewRedisOptions(), cfg.ValkeyChannel, logger)
	defer subscriber.Close()

	// Test Valkey connectivity
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pingCancel()
	if err := subscriber.Ping(pingCtx); err != nil {
		return fmt.Errorf("failed to connect to Valkey at %s: %w", cfg.ValkeyAddr, err)
	}
	logger.Info("connected to Valkey", "addr", cfg.ValkeyAddr)

	// Context with cancellation for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		logger.Info("shutting down worker")
		cancel()
	}()

	var messageCount int64
	handler := func(ctx context.Context, message string) {
		messageCount++
		logger.Info("received message", "message_count", messageCount)

		evt, err := payload.ParseAndValidate([]byte(message), cfg.AllowedImagePrefix)
		if err != nil {
			logger.Error("invalid message payload, skipping", "error", err.Error())
			return
		}

		imageRef := evt.ImageRef()
		logger.Info("processing event", "image_ref", imageRef)

		matches, err := restarter.FindMatchingDeployments(ctx, imageRef)
		if err != nil {
			logger.Error("failed to find matching deployments", "error", err)
			return
		}

		if len(matches) == 0 {
			logger.Info("no matching deployments found", "image_ref", imageRef)
			return
		}

		for _, m := range matches {
			logger.Info("found matching deployment",
				"namespace", m.Namespace,
				"deployment", m.Name,
				"containers", strings.Join(m.ContainerNames, ","),
				"image_ref", imageRef,
			)
			if err := restarter.RestartDeployment(ctx, m.Namespace, m.Name); err != nil {
				logger.Error("failed to restart deployment",
					"namespace", m.Namespace,
					"deployment", m.Name,
					"error", err,
				)
			}
		}
	}

	logger.Info("starting worker, subscribing to Valkey channel", "channel", cfg.ValkeyChannel)

	// Retry loop for subscriber
	for {
		err := subscriber.Subscribe(ctx, handler)
		if ctx.Err() != nil {
			// Context cancelled, exit gracefully
			return nil
		}
		if err != nil {
			logger.Error("Valkey subscription error, retrying in 5s", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(5 * time.Second):
		}
	}
}

