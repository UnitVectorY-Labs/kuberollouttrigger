package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
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

// Version is the application version, injected at build time via ldflags
var Version = "dev"

func main() {
	// Set the build version from the build info if not set by the build system
	if Version == "dev" || Version == "" {
		if bi, ok := debug.ReadBuildInfo(); ok {
			if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
				Version = bi.Main.Version
			}
		}
	}

	// Print the version on startup
	fmt.Printf("kuberollouttrigger version: %s\n", Version)

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

		imageRefs := evt.ImageRefs()
		logger.Info("processing event", "image", evt.Image, "tags", strings.Join(evt.Tags, ","), "image_refs_count", len(imageRefs))

		// Collect all matching deployments for any of the image references
		matchMap := make(map[string]k8s.MatchingDeployment)
		for _, imageRef := range imageRefs {
			matches, err := restarter.FindMatchingDeployments(ctx, imageRef)
			if err != nil {
				logger.Error("failed to find matching deployments", "image_ref", imageRef, "error", err)
				continue
			}

			// Add matches to the map (keyed by namespace/name to avoid duplicates)
			for _, m := range matches {
				key := m.Namespace + "/" + m.Name
				if existing, found := matchMap[key]; found {
					// Merge container names, avoiding duplicates
					containerSet := make(map[string]bool)
					for _, c := range existing.ContainerNames {
						containerSet[c] = true
					}
					for _, c := range m.ContainerNames {
						containerSet[c] = true
					}
					merged := make([]string, 0, len(containerSet))
					for c := range containerSet {
						merged = append(merged, c)
					}
					existing.ContainerNames = merged
					matchMap[key] = existing
				} else {
					matchMap[key] = m
				}
			}
		}

		if len(matchMap) == 0 {
			logger.Info("no matching deployments found", "image", evt.Image, "tags", strings.Join(evt.Tags, ","))
			return
		}

		for _, m := range matchMap {
			logger.Info("found matching deployment",
				"namespace", m.Namespace,
				"deployment", m.Name,
				"containers", strings.Join(m.ContainerNames, ","),
				"image", evt.Image,
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
