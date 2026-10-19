package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"syscall"
	"time"

	"github.com/sei-protocol/seictl/sidecar/engine"
	"github.com/sei-protocol/seictl/sidecar/rpc"
	"github.com/sei-protocol/seictl/sidecar/server"
	"github.com/sei-protocol/seictl/sidecar/tasks"
	"github.com/sei-protocol/seilog"
	"github.com/urfave/cli/v3"
)

var serveLog = seilog.NewLogger("seictl", "serve")

var serveCmd = cli.Command{
	Name:  "serve",
	Usage: "Start the sidecar task executor and HTTP API",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "port",
			Sources: cli.EnvVars("SEI_SIDECAR_PORT"),
			Value:   "7777",
			Usage:   "Port for the sidecar HTTP API",
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		defer func() { _ = seilog.Close() }()

		ctx, stop := signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)
		defer stop()

		homeDir := destinations.home
		if homeDir == "" {
			homeDir = "/sei"
		}
		port := cmd.String("port")
		chainID := os.Getenv("SEI_CHAIN_ID")
		genesisBucket := os.Getenv("SEI_GENESIS_BUCKET")
		genesisRegion := os.Getenv("SEI_GENESIS_REGION")
		snapshotBucket := os.Getenv("SEI_SNAPSHOT_BUCKET")
		snapshotRegion := os.Getenv("SEI_SNAPSHOT_REGION")

		podName := os.Getenv("HOSTNAME")
		if podName == "" {
			if h, err := os.Hostname(); err == nil {
				podName = h
			}
		}
		if podName == "" {
			podName = "unknown"
		}

		for _, kv := range []struct{ name, val string }{
			{"SEI_CHAIN_ID", chainID},
			{"SEI_GENESIS_BUCKET", genesisBucket},
			{"SEI_GENESIS_REGION", genesisRegion},
			{"SEI_SNAPSHOT_BUCKET", snapshotBucket},
			{"SEI_SNAPSHOT_REGION", snapshotRegion},
		} {
			if kv.val == "" {
				return fmt.Errorf("required environment variable %s is not set", kv.name)
			}
		}

		var snapshotUploadInterval time.Duration
		if raw := os.Getenv("SEI_SNAPSHOT_UPLOAD_INTERVAL"); raw != "" {
			parsed, err := time.ParseDuration(raw)
			if err != nil {
				return fmt.Errorf("invalid SEI_SNAPSHOT_UPLOAD_INTERVAL %q: %w", raw, err)
			}
			snapshotUploadInterval = parsed
		}

		var snapshotUploadTimeout time.Duration
		if raw := os.Getenv("SEI_SNAPSHOT_UPLOAD_TIMEOUT"); raw != "" {
			parsed, err := time.ParseDuration(raw)
			if err != nil {
				return fmt.Errorf("invalid SEI_SNAPSHOT_UPLOAD_TIMEOUT %q: %w", raw, err)
			}
			snapshotUploadTimeout = parsed
		}

		execCfg, err := buildExecutionConfig(homeDir)
		if err != nil {
			return err
		}

		if err := tasks.EnsureDefaultConfig(homeDir); err != nil {
			return fmt.Errorf("home directory init failed: %w", err)
		}

		store, err := engine.NewSQLiteStore(filepath.Join(homeDir, "sidecar.db"))
		if err != nil {
			return fmt.Errorf("open result store: %w", err)
		}
		// The store also backs the pre-broadcast idempotency marker for
		// sign-tx handlers (must be set before the handlers copy execCfg).
		execCfg.Checkpointer = store

		snapshotRestorer, err := tasks.NewSnapshotRestorer(homeDir, snapshotBucket, snapshotRegion, chainID, nil, nil)
		if err != nil {
			return fmt.Errorf("creating snapshot restorer: %w", err)
		}

		snapshotUploader, err := tasks.NewSnapshotUploader(homeDir, snapshotBucket, snapshotRegion, chainID, snapshotUploadInterval, nil)
		if err != nil {
			return fmt.Errorf("creating snapshot uploader: %w", err)
		}
		snapshotUploader.EmitStartupMetrics()

		handlers := map[engine.TaskType]engine.TaskHandler{
			engine.TaskSnapshotRestore:          snapshotRestorer.Handler(),
			engine.TaskConfigPatch:              tasks.NewConfigPatcher(homeDir).Handler(),
			engine.TaskConfigApply:              tasks.NewConfigApplier(homeDir).Handler(),
			engine.TaskConfigValidate:           tasks.NewConfigValidator(homeDir).Handler(),
			engine.TaskConfigReload:             tasks.NewConfigReloader(homeDir).Handler(),
			engine.TaskMarkReady:                tasks.MarkReadyHandler(),
			engine.TaskMarkNotReady:             tasks.NewMarkNotReadier(store).Handler(),
			engine.TaskRestartSeid:              tasks.NewRestartSeider().Handler(),
			engine.TaskStopSeid:                 tasks.NewStopSeider().Handler(),
			engine.TaskResetData:                tasks.NewResetDataer(homeDir).Handler(),
			engine.TaskConfigureGenesis:         tasks.NewGenesisFetcher(homeDir, chainID, genesisBucket, genesisRegion, nil).Handler(),
			engine.TaskConfigureStateSync:       tasks.NewStateSyncConfigurer(homeDir, nil).Handler(),
			engine.TaskSnapshotUpload:           snapshotUploader.Handler(),
			engine.TaskSnapshotUploadOnce:       snapshotUploader.OnceHandler(snapshotUploadTimeout),
			engine.TaskResultExport:             tasks.NewResultExporter(homeDir, chainID, podName, nil).Handler(),
			engine.TaskAwaitCondition:           tasks.NewConditionWaiter(nil).Handler(),
			engine.TaskGenerateIdentity:         tasks.NewIdentityGenerator(homeDir).Handler(),
			engine.TaskGenerateGentx:            tasks.NewGentxGenerator(homeDir).Handler(),
			engine.TaskUploadGenesisArtifacts:   tasks.NewGenesisArtifactUploader(homeDir, genesisBucket, genesisRegion, chainID, nil).Handler(),
			engine.TaskAssembleAndUploadGenesis: tasks.NewGenesisAssembler(homeDir, genesisBucket, genesisRegion, chainID, nil, nil).Handler(),
			engine.TaskSetGenesisPeers:          tasks.NewGenesisPeersSetter(homeDir, genesisBucket, genesisRegion, chainID, nil).Handler(),
			engine.TaskGovVote:                  tasks.NewGovVoter(execCfg).Handler(),
			engine.TaskGovSoftwareUpgrade:       tasks.NewGovSoftwareUpgrader(execCfg).Handler(),
			engine.TaskGovParamChange:           tasks.NewGovParamChanger(execCfg).Handler(),
			engine.TaskEvmLogicalDigest:         tasks.NewEvmLogicalDigester(nil).Handler(),
		}

		eng := engine.NewEngine(ctx, handlers, store)
		eng.Config = execCfg
		// Rehydrate after Config is installed so sign-tx handlers see
		// the full dep set via the goroutine-spawn happens-before edge.
		eng.RehydrateStaleTasks()

		authnMode, err := server.AuthnMode()
		if err != nil {
			return err
		}
		bindAddr := server.BindAddress(port, authnMode)
		logArgs := []any{"authnMode", authnMode, "bind", bindAddr}
		if authnMode == server.AuthnModeTrustedHeader {
			logArgs = append(logArgs, "bypassPaths", server.BypassPaths())
		}
		serveLog.Info("sidecar HTTP", logArgs...)
		srv := server.NewServer(bindAddr, eng, homeDir, authnMode)
		srvErr := srv.ListenAndServe(ctx)

		if closeErr := store.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "warn: result store close: %v\n", closeErr)
		}

		if srvErr != nil && !errors.Is(srvErr, context.Canceled) {
			return fmt.Errorf("server error: %w", srvErr)
		}
		return nil
	},
}

// buildExecutionConfig assembles the engine's runtime dependencies:
// keyring (opened from SEI_KEYRING_BACKEND, or nil) and RPC client
// (pointed at the local seid). Sign-tx tasks consume both; tasks that
// don't need them ignore the fields.
func buildExecutionConfig(homeDir string) (engine.ExecutionConfig, error) {
	// Wipe passphrase before any branching so every return path leaves
	// /proc/<pid>/environ clean.
	passphrase := os.Getenv("SEI_KEYRING_PASSPHRASE")
	_ = os.Unsetenv("SEI_KEYRING_PASSPHRASE")

	rpcClient := rpc.NewClient(rpc.DefaultEndpoint, nil)

	backend := os.Getenv("SEI_KEYRING_BACKEND")
	if backend == "" {
		return engine.ExecutionConfig{RPC: rpcClient}, nil
	}

	if !slices.Contains(server.AllowedBackends, backend) {
		return engine.ExecutionConfig{}, fmt.Errorf(
			"unsupported SEI_KEYRING_BACKEND %q (allowed: test|file|os)", backend)
	}

	dir := os.Getenv("SEI_KEYRING_DIR")
	if dir == "" {
		dir = filepath.Join(homeDir, "keyring-file")
	}

	if backend == server.BackendFile && passphrase == "" {
		return engine.ExecutionConfig{}, fmt.Errorf(
			"SEI_KEYRING_PASSPHRASE required when SEI_KEYRING_BACKEND=file")
	}

	kr, err := server.OpenKeyring(backend, dir, passphrase)
	if err != nil {
		// Don't %w-wrap: OpenKeyring redacted err.Error(), but a typed
		// field in the underlying SDK chain could resurface the secret.
		return engine.ExecutionConfig{}, err
	}

	if err := server.SmokeTestKeyring(kr); err != nil {
		return engine.ExecutionConfig{}, err
	}

	serveLog.Info("keyring opened", "backend", backend, "dir", dir)
	return engine.ExecutionConfig{Keyring: kr, RPC: rpcClient}, nil
}
