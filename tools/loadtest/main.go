package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/metric"
	"golang.org/x/time/rate"

	"github.com/paxeer-network/pax-load/config"
	"github.com/paxeer-network/pax-load/generator"
	"github.com/paxeer-network/pax-load/sender"
	"github.com/paxeer-network/pax-load/stats"
	"github.com/paxeer-network/pax-load/utils"
	"github.com/paxeer-network/pax-load/utils/service"
)

var (
	configFile string
)

var rootCmd = &cobra.Command{
	Use:   "paxload",
	Short: "Pax Chain Load Test v2",
	Long: `A load test generator for Pax Chain.

Supports both contract and non-contract scenarios with factory
and weighted scenario selection mechanisms. Features sharded sending
to multiple endpoints with account pooling management.

Use --dry-run to test configuration and view transaction details
without actually sending requests or deploying contracts.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := runLoadTest(context.Background(), cmd, args); err != nil {
			log.Fatal(err)
		}
	},
}

func init() {
	rootCmd.Flags().StringVarP(&configFile, "config", "c", "", "Path to configuration file (required)")
	rootCmd.Flags().DurationP("stats-interval", "s", 0, "Interval for logging statistics")
	rootCmd.Flags().IntP("buffer-size", "b", 0, "Buffer size per worker")
	rootCmd.Flags().Float64P("tps", "t", 0, "Transactions per second (0 = no limit)")
	rootCmd.Flags().Bool("dry-run", false, "Mock deployment and requests")
	rootCmd.Flags().Bool("debug", false, "Log each request")
	rootCmd.Flags().Bool("track-receipts", false, "Track receipts")
	rootCmd.Flags().Bool("track-blocks", false, "Track blocks")
	rootCmd.Flags().Bool("prewarm", false, "Prewarm accounts with self-transactions")
	rootCmd.Flags().Bool("track-user-latency", false, "Track user latency")
	rootCmd.Flags().IntP("workers", "w", 0, "Number of workers")
	rootCmd.Flags().IntP("nodes", "n", 0, "Number of nodes/endpoints to use (0 = use all)")
	rootCmd.Flags().String("metricsListenAddr", "0.0.0.0:9090", "The ip:port on which to export prometheus metrics.")
	rootCmd.Flags().Bool("ramp-up", false, "Ramp up loadtest")
	rootCmd.Flags().String("report-path", "", "Path to save the report")
	rootCmd.Flags().String("txs-dir", "", "Path to save the transactions")
	rootCmd.Flags().Uint64("target-gas", 10_000_000, "Target gas per block")
	rootCmd.Flags().Int("num-blocks-to-write", 100, "Number of blocks to write")

	// Initialize Viper with proper error handling
	if err := config.InitializeViper(rootCmd); err != nil {
		log.Fatalf("Failed to initialize configuration: %v", err)
	}

	if err := rootCmd.MarkFlagRequired("config"); err != nil {
		log.Fatal(err)
	}
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		_, err := fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		if err != nil {
			log.Fatal(err)
		}
		os.Exit(1)
	}
}

func runLoadTest(ctx context.Context, cmd *cobra.Command, args []string) error {
	// Parse the config file into a config.LoadConfig struct
	cfg, err := loadConfig(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Load settings into Viper
	if err := config.LoadSettings(cfg.Settings); err != nil {
		return fmt.Errorf("failed to load config file: %w", err)
	}

	// Get resolved settings from the config package
	settings := config.ResolveSettings()

	// Handle --nodes flag to limit number of endpoints
	nodes, _ := cmd.Flags().GetInt("nodes")
	if nodes > 0 && nodes < len(cfg.Endpoints) {
		log.Printf("🔧 Limiting endpoints from %d to %d nodes", len(cfg.Endpoints), nodes)
		cfg.Endpoints = cfg.Endpoints[:nodes]
	}

	log.Printf("🚀 Starting Pax Chain Load Test v2")
	log.Printf("📁 Config file: %s", configFile)
	log.Printf("🎯 Endpoints: %d", len(cfg.Endpoints))
	log.Printf("👥 Workers per endpoint: %d", settings.Workers)
	log.Printf("🔧 Total workers: %d", len(cfg.Endpoints)*settings.Workers)
	log.Printf("📊 Scenarios: %d", len(cfg.Scenarios))
	log.Printf("⏱️  Stats interval: %v", settings.StatsInterval.ToDuration())
	log.Printf("📦 Buffer size per worker: %d", settings.BufferSize)
	if settings.TPS > 0 {
		log.Printf("📈 Transactions per second: %.2f", settings.TPS)
	}
	if settings.DryRun {
		log.Printf("📝 Dry run: enabled")
	}
	if settings.TrackReceipts {
		log.Printf("📝 Track receipts: enabled")
	}
	if settings.TrackBlocks {
		log.Printf("📝 Track blocks: enabled")
	}
	if settings.Prewarm {
		log.Printf("📝 Prewarm: enabled")
	}
	if settings.TrackUserLatency {
		log.Printf("📝 Track user latency: enabled")
	}

	// Enable mock deployment in dry-run mode
	if settings.DryRun {
		cfg.MockDeploy = true
	}

	listenAddr := cmd.Flag("metricsListenAddr").Value.String()
	log.Printf("serving metrics at %s/metrics", listenAddr)

	if err := exportPrometheusMetrics(ctx, listenAddr); err != nil {
		return err
	}

	// Create statistics collector and logger
	collector := stats.NewCollector()
	logger := stats.NewLogger(collector, settings.StatsInterval.ToDuration(), settings.ReportPath, settings.Debug)
	var ramper *sender.Ramper

	err = service.Run(ctx, func(ctx context.Context, s service.Scope) error {
		// Create the generator from the config struct
		gen, err := generator.NewConfigBasedGenerator(cfg)
		if err != nil {
			return fmt.Errorf("failed to create generator: %w", err)
		}

		// Create shared rate limiter for all workers if TPS is specified
		var sharedLimiter *rate.Limiter
		if settings.TPS > 0 {
			sharedLimiter = rate.NewLimiter(rate.Limit(settings.TPS), 1)
			log.Printf("📈 Rate limiting enabled: %.2f TPS shared across all workers", settings.TPS)
		} else {
			// No rate limiting
			sharedLimiter = rate.NewLimiter(rate.Inf, 1)
		}

		// Create and start block collector if endpoints are available
		var blockCollector *stats.BlockCollector
		if len(cfg.Endpoints) > 0 && settings.TrackBlocks {
			blockCollector = stats.NewBlockCollector(cfg.PaxChainID)
			collector.SetBlockCollector(blockCollector)
			s.SpawnBgNamed("block collector", func() error {
				return blockCollector.Run(ctx, cfg.Endpoints[0])
			})
		}

		if settings.RampUp {
			ramperBlockCollector := stats.NewBlockCollector(cfg.PaxChainID)
			s.SpawnBgNamed("ramper block collector", func() error {
				return ramperBlockCollector.Run(ctx, cfg.Endpoints[0])
			})

			ramper = sender.NewRamper(
				sender.NewRampCurveStep(100, 100, 120*time.Second, 30*time.Second),
				ramperBlockCollector,
				sharedLimiter,
			)
			s.SpawnBgNamed("ramper", func() error { return ramper.Run(ctx) })
		}

		// Create and start user latency tracker if endpoints are available
		if len(cfg.Endpoints) > 0 && settings.TrackUserLatency {
			userLatencyTracker := stats.NewUserLatencyTracker(settings.StatsInterval.ToDuration())
			s.SpawnBgNamed("user latency tracker", func() error {
				return userLatencyTracker.Run(ctx, cfg.Endpoints[0])
			})
		}

		// Create the sender from the config struct
		snd, err := sender.NewShardedSender(cfg, settings.BufferSize, settings.Workers, sharedLimiter)
		if err != nil {
			return fmt.Errorf("failed to create sender: %w", err)
		}

		// Enable dry-run mode in sender if specified
		if settings.DryRun {
			snd.SetDryRun(true)
		}
		if settings.Debug {
			snd.SetDebug(true)
		}
		if settings.TrackReceipts {
			snd.SetTrackReceipts(true)
		}
		if settings.TrackBlocks {
			snd.SetTrackBlocks(true)
		}

		// Set statistics collector for sender and its workers
		snd.SetStatsCollector(collector, logger)

		// Create dispatcher
		var dispatcher *sender.Dispatcher
		if settings.TxsDir != "" {
			// get latest height
			ethclient, err := ethclient.Dial(cfg.Endpoints[0])
			if err != nil {
				return fmt.Errorf("failed to create ethclient: %w", err)
			}
			latestHeight, err := ethclient.BlockNumber(ctx)
			if err != nil {
				return fmt.Errorf("failed to get latest height: %w", err)
			}
			numBlocksToWrite := settings.NumBlocksToWrite
			writerHeight := latestHeight + 10 // some buffer
			log.Printf("🔍 Latest height: %d, writer start height: %d", latestHeight, writerHeight)
			writer := sender.NewTxsWriter(settings.TargetGas, settings.TxsDir, writerHeight, uint64(numBlocksToWrite))
			dispatcher = sender.NewDispatcher(gen, writer)
		} else {
			dispatcher = sender.NewDispatcher(gen, snd)
		}

		// Set statistics collector for dispatcher
		dispatcher.SetStatsCollector(collector)

		// Set up prewarming if enabled
		if settings.Prewarm {
			log.Printf("🔥 Creating prewarm generator...")
			prewarmGen := generator.NewPrewarmGenerator(cfg, gen)
			dispatcher.SetPrewarmGenerator(prewarmGen)
			log.Printf("✅ Prewarm generator ready")
			log.Printf("📝 Prewarm mode: Accounts will be prewarmed")
		}

		if settings.TxsDir == "" {
			// Start the sender (starts all workers)
			s.SpawnBgNamed("sender", func() error { return snd.Run(ctx) })
			log.Printf("✅ Connected to %d endpoints", snd.GetNumShards())
		}
		// Perform prewarming if enabled (before starting logger to avoid logging prewarm transactions)
		if settings.Prewarm {
			if err := dispatcher.Prewarm(ctx); err != nil {
				return fmt.Errorf("failed to prewarm accounts: %w", err)
			}
		}

		// Start logger (after prewarming to capture only main load test metrics)
		s.SpawnBgNamed("logger", func() error { return logger.Run(ctx) })
		log.Printf("✅ Started statistics logger")

		// Start dispatcher for main load test
		s.SpawnBgNamed("dispatcher", func() error { return dispatcher.Run(ctx) })
		log.Printf("✅ Started dispatcher")

		// Set up signal handling for graceful shutdown
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

		log.Printf("📈 Logging statistics every %v (Press Ctrl+C to stop)", settings.StatsInterval.ToDuration())
		if settings.DryRun {
			log.Printf("📝 Dry-run mode: Simulating requests without sending")
		}
		if settings.Debug {
			log.Printf("🐛 Debug mode: Each transaction will be logged")
		}
		if settings.TrackReceipts {
			log.Printf("📝 Track receipts mode: Receipts will be tracked")
		}
		if settings.TrackBlocks {
			log.Printf("📝 Track blocks mode: Block data will be collected")
		}
		if settings.TrackUserLatency {
			log.Printf("📝 Track user latency mode: User latency will be tracked")
		}
		log.Print(strings.Repeat("=", 60))

		// Main loop - wait for shutdown signal
		if _, err := utils.Recv(ctx, sigChan); err != nil {
			return err
		}
		log.Print("\n🛑 Received shutdown signal, stopping gracefully...")
		return nil
	})
	// Print final statistics
	logger.LogFinalStats()
	if settings.RampUp && ramper != nil {
		ramper.LogFinalStats()
	}
	log.Printf("👋 Shutdown complete")
	return err
}

func exportPrometheusMetrics(ctx context.Context, listenAddr string) error {
	metricsExporter, err := prometheus.New(prometheus.WithNamespace("paxload"))
	if err != nil {
		return fmt.Errorf("failed to create Prometheus exporter: %w", err)
	}
	otel.SetMeterProvider(metric.NewMeterProvider(metric.WithReader(metricsExporter)))
	go func() {
		defer func() { _ = metricsExporter.Shutdown(ctx) }()
		http.Handle("/metrics", promhttp.Handler())
		err := http.ListenAndServe(listenAddr, nil)
		if err != nil {
			log.Printf("failed to serve metrics: %v", err)
			return
		}
	}()
	return nil
}

// loadConfig reads and parses the configuration file
func loadConfig(filename string) (*config.LoadConfig, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg config.LoadConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config json: %w", err)
	}

	// Validate configuration
	if len(cfg.Endpoints) == 0 {
		return nil, fmt.Errorf("no endpoints specified in config")
	}

	if len(cfg.Scenarios) == 0 {
		return nil, fmt.Errorf("no scenarios specified in config")
	}

	return &cfg, nil
}
