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
	"sync"
	"syscall"
	"time"

	"github.com/cliffyan/mcp-gateway/internal/adapter"
	"github.com/cliffyan/mcp-gateway/internal/checker"
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
	checkMode := flag.Bool("check", false, "Check MCP servers status and list tools")
	outputFormat := flag.String("output", "text", "Output format for check: text, json")
	checkTimeout := flag.Duration("timeout", 30*time.Second, "Timeout for each MCP server check")
	flag.Parse()

	if *showVersion {
		fmt.Printf("mcp-gateway version %s\n", version)
		os.Exit(0)
	}

	// Check mode: validate configuration and list tools
	if *checkMode {
		if err := checker.RunCheck(*configPath, *checkTimeout, *outputFormat); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
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
		"upstreams", len(cfg.Upstreams),
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
	var wg sync.WaitGroup
	errCh := make(chan error, len(cfg.Upstreams))

	// Start each upstream instance
	for _, upstreamCfg := range cfg.Upstreams {
		wg.Add(1)
		go func(ucfg config.UpstreamInstanceConfig) {
			defer wg.Done()
			if err := runUpstream(ctx, ucfg, cfg.Logging, logger); err != nil {
				errCh <- fmt.Errorf("[%s] %w", ucfg.Name, err)
			}
		}(upstreamCfg)
	}

	// Wait for all upstreams to complete
	wg.Wait()
	close(errCh)

	// Collect errors
	var errors []error
	for err := range errCh {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		return fmt.Errorf("upstream errors: %v", errors)
	}

	return nil
}

func runUpstream(ctx context.Context, ucfg config.UpstreamInstanceConfig, logCfg config.LoggingConfig, logger *slog.Logger) error {
	upstreamLogger := logger.With("upstream", ucfg.Name)

	upstreamLogger.Info("starting upstream instance",
		"endpoint", ucfg.Endpoint,
		"servers", ucfg.EnabledServers(),
	)

	// Set verbose mode
	if logCfg.Verbose {
		upstreamLogger.Info("verbose logging enabled")
	}

	// Create router for this upstream
	r := router.NewRouter(upstreamLogger)
	r.SetVerbose(logCfg.Verbose)

	// Create adapter factory
	factory := adapter.NewFactory()

	// Create and start adapters for this upstream
	for name, serverCfg := range ucfg.MCPServers {
		if serverCfg.Disabled {
			upstreamLogger.Info("skipping disabled server", "name", name)
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
		upstreamLogger.Info("started adapter", "name", name, "type", serverCfg.Type)
	}

	// Initialize all adapters (MCP handshake + list tools)
	upstreamLogger.Info("initializing adapters...")
	if err := r.InitializeAll(ctx); err != nil {
		upstreamLogger.Warn("some adapters failed to initialize", "error", err)
		// Continue anyway, some adapters may have succeeded
	}

	upstreamLogger.Info("initialization complete",
		"adapters", r.AdapterCount(),
		"tools", r.ToolCount(),
	)

	// Create message handler
	handler := func(ctx context.Context, msg *protocol.Message) (*protocol.Message, error) {
		return r.Handle(ctx, msg)
	}

	// Create upstream client
	client := upstream.NewClient(ucfg, handler, upstreamLogger)

	// Run upstream client (blocks until context cancelled)
	return client.Run(ctx)
}
