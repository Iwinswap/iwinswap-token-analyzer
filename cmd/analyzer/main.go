// main is the entry point for the iwinswap-token-analyzer application.
// It acts as the "composition root," responsible for loading configuration
// and assembling all the necessary components of the system.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	token "github.com/Iwinswap/iwinswap-erc20-token-system"
	subscriber "github.com/Iwinswap/iwinswap-eth-block-subscriber"
	clientmanager "github.com/Iwinswap/iwinswap-ethclient-manager"
	"github.com/Iwinswap/iwinswap-token-analyzer/bloom"
	"github.com/Iwinswap/iwinswap-token-analyzer/config"
	"github.com/Iwinswap/iwinswap-token-analyzer/erc20analyzer"
	"github.com/Iwinswap/iwinswap-token-analyzer/fork"
	"github.com/Iwinswap/iwinswap-token-analyzer/initializers"
	"github.com/Iwinswap/iwinswap-token-analyzer/internal/services"
	erc20_log_volume_analyzer "github.com/Iwinswap/iwinswap-token-analyzer/logs/erc20analyzer"
	logextractor "github.com/Iwinswap/iwinswap-token-analyzer/logs/extractor"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"
)

var (
	DefaultNewBlockEventerBuffer = 100
)

// AppErrorHandler provides structured, context-aware error handling.
type AppErrorHandler struct {
	logger  *slog.Logger
	chainID uint64
}

// Handle logs an error with the associated chain ID for context.
func (h *AppErrorHandler) Handle(err error) {
	h.logger.Error(
		"An error occurred in an analyzer component",
		"error", err,
		"chain_id", h.chainID,
	)
}

func main() {
	// 1. Setup logger and load configuration
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := loadConfig()
	if err != nil {
		logger.Error("Failed to load configuration", "error", err)
		os.Exit(1)
	}
	logger.Info("Configuration loaded successfully", "chains_found", len(cfg.Chains))

	// 2. Create main application context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 3. Initialize components for each enabled chain
	viewers := make(map[uint64]services.ERC20TokenViewer)
	for _, chainCfg := range cfg.Chains {
		if !chainCfg.IsEnabled {
			logger.Info("Skipping disabled chain", "name", chainCfg.Name, "chain_id", chainCfg.ChainID)
			continue
		}
		logger.Info("Setting up analyzer", "name", chainCfg.Name, "chain_id", chainCfg.ChainID)

		analyzer, err := setupERC20Analyzer(ctx, chainCfg, logger)
		if err != nil {
			logger.Error("Failed to setup analyzer for chain", "chain_id", chainCfg.ChainID, "error", err)
			os.Exit(1) // Exit if any chain fails to initialize.
		}
		viewers[chainCfg.ChainID] = analyzer
	}
	logger.Info("All analyzers started successfully.")

	// 4. Initialize and start the API Service
	apiService := services.NewERC20APIService(viewers)
	mux := http.NewServeMux()
	apiService.RegisterRoutes(mux)

	logger.Info("Registered API Endpoints:")
	for _, endpoint := range apiService.Endpoints() {
		logger.Info(fmt.Sprintf(" - http://%s%s", cfg.APIServer.Addr, endpoint))
	}

	server := &http.Server{
		Addr:    cfg.APIServer.Addr,
		Handler: mux,
	}

	go func() {
		logger.Info("Starting HTTP server", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("HTTP server failed", "error", err)
			cancel() // Trigger a shutdown of the whole application if the server fails
		}
	}()

	// 5. Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-quit:
		logger.Info("Shutdown signal received.")
	case <-ctx.Done():
		// This will trigger if another part of the app fails and cancels the context.
		logger.Info("Context cancelled, initiating shutdown.")
	}

	// The deferred cancel() will handle stopping all analyzer goroutines.
	// We now gracefully shut down the HTTP server.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	logger.Info("Shutting down HTTP server...")
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP server shutdown failed", "error", err)
	}

	logger.Info("Application shut down gracefully.")
}

func setupERC20Analyzer(ctx context.Context, cfg config.ChainConfig, logger *slog.Logger) (services.ERC20TokenViewer, error) {
	// --- Bottom-up Dependency Initialization ---

	// 1. Client Manager
	clientManager, err := clientmanager.NewClientManager(ctx, cfg.ClientManager.RPCURLs, &clientmanager.ClientManagerConfig{
		ClientConfig: &clientmanager.ClientConfig{MaxConcurrentETHCalls: cfg.ClientManager.MaxConcurrentRequests, Logger: logger},
		Logger:       logger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create client manager: %w", err)
	}

	// 2. Block Subscriber
	newBlockEventer := make(chan *types.Block, DefaultNewBlockEventerBuffer)
	subscriber.NewBlockSubscriber(ctx, newBlockEventer, getHealthyClientsForSubscriber(clientManager), &subscriber.SubscriberConfig{Logger: logger})

	// 3. Log Extractor
	blockLogsExtractor, err := logextractor.NewLiveExtractor(bloom.ERC20TransferLikelyInBloom, getFiltererForLogsExtractor(clientManager))
	if err != nil {
		return nil, fmt.Errorf("failed to create log extractor: %w", err)
	}

	// 4. Token Holder Analyzer (Volume based)
	volumeAnalyzerCfg := cfg.ERC20Analyzer.VolumeAnalyzer
	tokenHolderAnalyzer := erc20_log_volume_analyzer.NewVolumeAnalyzer(ctx, volumeAnalyzerCfg.ExpiryCheckFrequency, volumeAnalyzerCfg.RecordStaleDuration)

	// 5. Fee and Gas Requester (Fork based)
	requesterCfg := cfg.FeeAndGasRequester
	feeAndGasRequester, err := fork.NewERC20FeeAndGasRequester(requesterCfg.ForkRPCURL, requesterCfg.MaxConcurrentRequests)
	if err != nil {
		return nil, fmt.Errorf("failed to create fee and gas requester: %w", err)
	}

	// 6. Token Initializer
	tokenInitializer, err := initializers.NewERC20Initializer(clientManager.GetClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create token initializer: %w", err)
	}

	// 7. Assemble the final Analyzer Config
	analyzerCfg := erc20analyzer.Config{
		NewBlockEventer:          newBlockEventer,
		TokenStore:               token.NewTokenSystem(), // Replace with your real DB-backed store
		TokenInitializer:         tokenInitializer,
		BlockExtractor:           blockLogsExtractor,
		TokenHolderAnalyzer:      tokenHolderAnalyzer,
		FeeAndGasRequester:       feeAndGasRequester,
		FeeAndGasUpdateFrequency: cfg.ERC20Analyzer.FeeAndGasUpdateFrequency,
		MinTokenUpdateInterval:   cfg.ERC20Analyzer.MinTokenUpdateInterval,
		ErrorHandler:             (&AppErrorHandler{logger: logger, chainID: cfg.ChainID}).Handle,
	}

	// 8. Create the final Analyzer instance. Its background services are started immediately.
	return erc20analyzer.NewAnalyzer(ctx, analyzerCfg)
}

func loadConfig() (*config.Config, error) {
	configPath := flag.String("config", "config.yaml", "Path to the configuration file.")
	flag.Parse()
	log.Printf("Loading configuration from: %s", *configPath)
	return config.LoadConfig(*configPath)
}

// --- Helper Functions ---

func getHealthyClientsForSubscriber(clientManager *clientmanager.ClientManager) func() []subscriber.ETHClient {
	return func() []subscriber.ETHClient {
		clients := []subscriber.ETHClient{}
		known := map[subscriber.ETHClient]struct{}{}
		for {
			client, err := clientManager.GetClient()
			if err != nil {
				break
			}
			if _, exists := known[client]; exists {
				break
			}
			known[client] = struct{}{}
		}
		for c := range known {
			clients = append(clients, c)
		}
		return clients
	}
}

func getFiltererForLogsExtractor(clientManager *clientmanager.ClientManager) func() (func(context.Context, ethereum.FilterQuery) ([]types.Log, error), error) {
	return func() (func(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error), error) {
		client, err := clientManager.GetClient()
		if err != nil {
			return nil, err
		}
		return client.FilterLogs, nil
	}
}
