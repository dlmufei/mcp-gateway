// mcp-gateway is a high-performance MCP (Model Context Protocol) gateway
// that bridges multiple downstream MCP servers to an upstream WebSocket endpoint.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/cliffyan/mcp-gateway/internal/adapter"
	"github.com/cliffyan/mcp-gateway/internal/config"
	"github.com/cliffyan/mcp-gateway/internal/protocol"
	"github.com/cliffyan/mcp-gateway/internal/router"
	"github.com/cliffyan/mcp-gateway/internal/upstream"
)

var (
	version = "1.0.0"
)

func main() {
	// Parse flags
	configPath := flag.String("config", "", "Path to config file")
	showVersion := flag.Bool("version", false, "Show version")
	flag.Parse()

	if *showVersion {
		fmt.Printf("mcp-gateway version %s\n", version)
		os.Exit(0)
	}

	// Setup logger
	logLevel := slog.LevelInfo
	if os.Getenv("MCP_LOG_LEVEL") == "debug" {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Log startup info
	logger.Info("starting mcp-gateway",
		"version", version,
		"endpoint", cfg.Upstream.Endpoint,
		"servers", cfg.EnabledServers(),
	)

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logger.Info("received signal, shutting down", "signal", sig)
		cancel()
	}()

	// Run gateway
	if err := run(ctx, cfg, logger); err != nil {
		logger.Error("gateway error", "error", err)
		os.Exit(1)
	}

	logger.Info("mcp-gateway stopped")
}

func run(ctx context.Context, cfg *config.Config, logger *slog.Logger) error {
	// Create router
	r := router.NewRouter(logger)

	// Set verbose mode from config
	r.SetVerbose(cfg.Logging.Verbose)
	if cfg.Logging.Verbose {
		logger.Info("verbose logging enabled")
	}

	// Create adapter factory
	factory := adapter.NewFactory()

	// Create and start adapters
	for name, serverCfg := range cfg.MCPServers {
		if serverCfg.Disabled {
			logger.Info("skipping disabled server", "name", name)
			continue
		}

		a, err := factory.Create(name, serverCfg)
		if err != nil {
			return fmt.Errorf("create adapter %s: %w", name, err)
		}

		if err := a.Start(ctx); err != nil {
			return fmt.Errorf("start adapter %s: %w", name, err)
		}

		r.RegisterAdapter(name, a)
		logger.Info("started adapter", "name", name, "type", serverCfg.Type)
	}

	// Initialize all adapters (MCP handshake + list tools)
	logger.Info("initializing adapters...")
	if err := r.InitializeAll(ctx); err != nil {
		logger.Warn("some adapters failed to initialize", "error", err)
		// Continue anyway, some adapters may have succeeded
	}

	logger.Info("initialization complete",
		"adapters", r.AdapterCount(),
		"tools", r.ToolCount(),
	)

	// Create message handler
	handler := func(ctx context.Context, msg *protocol.Message) (*protocol.Message, error) {
		return r.Handle(ctx, msg)
	}

	// Create upstream client
	client := upstream.NewClient(cfg.Upstream, handler, logger)

	// Run upstream client (blocks until context cancelled)
	return client.Run(ctx)
}
