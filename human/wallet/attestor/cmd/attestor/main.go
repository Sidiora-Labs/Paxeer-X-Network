package main

import (
	"context"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/audit"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/auth/agent"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/auth/jwt"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/backup"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/config"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/health"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/lxwire"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/policy"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/policy/lx"
	nativepolicy "github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/policy/native"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/replica"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/server"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/store"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/transport"
)

type listening struct {
	API  string
	Peer string
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if len(os.Args) > 1 && os.Args[1] == "restore" {
		if err := restore(os.Args[2:], os.Getenv, os.Stdout); err != nil {
			log.Fatal(err)
		}
		return
	}
	if err := run(ctx, os.Getenv, nil); err != nil {
		log.Fatal(err)
	}
}

func restore(args []string, getenv func(string) string, out io.Writer) error {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	fs.SetOutput(out)
	snapshot := fs.String("snapshot", "", "path of the snapshot file to restore")
	digest := fs.String("sha256", "", "expected SHA-256 digest of the snapshot file, 64 hex characters")
	dataDir := fs.String("data-dir", "", "empty directory to restore into, defaulting to "+config.EnvDataDir)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *snapshot == "" || *digest == "" || fs.NArg() != 0 {
		return errors.New("attestor restore: -snapshot and -sha256 are required and no other arguments are accepted")
	}
	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}
	if cfg.BackupKey == nil {
		return fmt.Errorf("attestor restore: %s is required", config.EnvBackupKeyFile)
	}
	target := cfg.DataDir
	if *dataDir != "" {
		target = *dataDir
	}
	res, err := backup.RestoreFile(*snapshot, *digest, cfg.NodeID, cfg.BackupKey, target, cfg.NodeKey)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "restored %s (sha256 %s) for node %s: %d shares, audit sequence %d\n", res.Name, hex.EncodeToString(res.SHA256[:]), cfg.NodeID, res.Shares, res.AuditSequence)
	return err
}

func certPin(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return "", errors.New("certificate file holds no PEM block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", err
	}
	pin := transport.SPKIHash(cert)
	return hex.EncodeToString(pin[:]), nil
}

func policies(cfg *config.Config, st *store.Store) (*policy.Policy, *policy.SpendLedger, *lx.Evaluator, error) {
	var doc *policy.Document
	if cfg.PolicyFile != "" {
		var err error
		if doc, err = policy.LoadFile(cfg.PolicyFile); err != nil {
			return nil, nil, nil, err
		}
	}
	engine := policy.New(doc)
	ledger, err := policy.NewSpendLedger(st, time.Now)
	if err != nil {
		return nil, nil, nil, err
	}
	if cfg.KernelPolicy == "" {
		return engine, ledger, nil, nil
	}
	kdoc, err := lx.LoadFile(cfg.KernelPolicy)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("attestor: %s: %w", config.EnvKernelPolicy, err)
	}
	chain, err := lx.NewChain(cfg.RPCURL, &http.Client{Timeout: lx.DefaultRPCWait})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("attestor: %s: %w", config.EnvRPCURL, err)
	}
	kernel, err := lx.New(engine, kdoc, chain)
	if err != nil {
		return nil, nil, nil, err
	}
	return engine, ledger, kernel, nil
}

func run(ctx context.Context, getenv func(string) string, ready func(listening)) error {
	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}
	if cfg.TLSCertFile == "" || cfg.TLSKeyFile == "" || cfg.TLSCAFile == "" || cfg.OperatorCAFile == "" {
		return errors.New("attestor: TLS certificate, key, peer CA and operator CA files are required")
	}
	selfPin, err := certPin(cfg.TLSCertFile)
	if err != nil {
		return fmt.Errorf("attestor: own certificate: %w", err)
	}
	peers := []transport.Peer{{ID: cfg.NodeID, Address: cfg.PeerListenAddr, SPKISHA256: selfPin}}
	participants := []string{cfg.NodeID}
	probe := make(map[string]string, len(cfg.Peers))
	for _, p := range cfg.Peers {
		pin, ok := cfg.PeerPins[p.ID]
		if !ok {
			return fmt.Errorf("attestor: %s has no pin for peer %s", config.EnvPeerPins, p.ID)
		}
		peers = append(peers, transport.Peer{ID: p.ID, Address: p.Addr, SPKISHA256: pin})
		participants = append(participants, p.ID)
		probe[p.ID] = p.Addr
	}

	st, err := store.Open(cfg.DataDir, cfg.NodeKey)
	if err != nil {
		return err
	}
	defer st.Close()
	auditLog, err := audit.Open(filepath.Join(cfg.DataDir, "audit"))
	if err != nil {
		return err
	}
	defer auditLog.Close()

	engine, ledger, kernel, err := policies(cfg, st)
	if err != nil {
		return err
	}
	var tokens *jwt.TokenVerifier
	if cfg.JWKSURL != "" {
		tokens, err = jwt.NewTokenVerifier(jwt.Config{
			JWKSURL:    cfg.JWKSURL,
			Issuer:     cfg.JWTIssuer,
			Audience:   cfg.JWTAudience,
			HTTPClient: &http.Client{Timeout: 10 * time.Second},
			MaxAge:     cfg.JWTMaxAge,
			Replay:     st,
		})
		if err != nil {
			return err
		}
	}
	authority, err := agent.NewAuthority(agent.AuthorityConfig{PublicKeyFile: cfg.AuthorityPublicKey, Issuer: cfg.AuthorityIssuer, Tenant: cfg.AuthorityTenant, Store: st, ChainID: cfg.ChainID})
	if err != nil {
		return fmt.Errorf("attestor: pinned custody producer authority: %w", err)
	}
	agents, err := agent.NewAgentVerifier(agent.Config{Principals: authority, Nonces: st, MaxExpiry: cfg.AgentMaxExpiry})
	if err != nil {
		return err
	}
	inventoryPins := map[string]string{cfg.NodeID: selfPin}
	for id, pin := range cfg.PeerPins {
		inventoryPins[id] = pin
	}
	inventory, err := server.NewInventory(cfg.InventoryFile, cfg.InventoryPublicKey, cfg.AuthorityIssuer, cfg.AuthorityTenant, st, inventoryPins)
	if err != nil {
		return fmt.Errorf("attestor: approved wallet inventory: %w", err)
	}
	clients, err := server.LoadClientAuthorities(cfg.TLSCAFile, cfg.OperatorCAFile)
	if err != nil {
		return err
	}
	activityTypes := make([]lxwire.ActivityType, 0, len(cfg.ActivityTypes))
	for _, t := range cfg.ActivityTypes {
		activityTypes = append(activityTypes, lxwire.ActivityType(t))
	}
	registry, err := lxwire.NewRegistry(activityTypes...)
	if err != nil {
		return fmt.Errorf("attestor: %s: %w", config.EnvActivityTypes, err)
	}

	replicaCfg, err := replica.LoadConfig(getenv, cfg.BackupDir, cfg.DataDir)
	if err != nil {
		return err
	}
	var replicaState func() health.ReplicaState
	var shipped backup.Shipped
	if replicaCfg != nil {
		shipper, err := replica.New(*replicaCfg, log.Default())
		if err != nil {
			return err
		}
		replicaState = shipper.State
		shipped = shipper.Ledger()
		shipCtx, cancelShip := context.WithCancel(ctx)
		shipDone := make(chan struct{})
		go func() {
			defer close(shipDone)
			shipper.Run(shipCtx)
		}()
		defer func() {
			cancelShip()
			<-shipDone
		}()
	}

	var snapshots *backup.Writer
	if cfg.BackupDir != "" {
		snapshots, err = backup.New(backup.Config{
			NodeID:    cfg.NodeID,
			Dir:       cfg.BackupDir,
			BackupKey: cfg.BackupKey,
			Retain:    cfg.SnapshotRetain,
			Store:     st,
			Audit:     auditLog,
			Shipped:   shipped,
			Logger:    log.Default(),
		})
		if err != nil {
			return err
		}
		snapCtx, cancelSnap := context.WithCancel(ctx)
		snapDone := make(chan struct{})
		go func() {
			defer close(snapDone)
			snapshots.Run(snapCtx, cfg.SnapshotInterval)
		}()
		defer func() {
			cancelSnap()
			<-snapDone
		}()
	}

	tr, err := transport.New(transport.Config{
		SelfID:         cfg.NodeID,
		ListenAddr:     cfg.PeerListenAddr,
		CertFile:       cfg.TLSCertFile,
		KeyFile:        cfg.TLSKeyFile,
		CAFile:         cfg.TLSCAFile,
		Peers:          peers,
		OperatorCAFile: cfg.OperatorCAFile,
	})
	if err != nil {
		return err
	}
	defer tr.Close()

	var nativeDoc *nativepolicy.Document
	if cfg.NativePolicy != "" {
		nativeDoc, err = nativepolicy.LoadFile(cfg.NativePolicy)
		if err != nil {
			return fmt.Errorf("attestor: %s: %w", config.EnvNativePolicy, err)
		}
	}
	srv, err := server.New(server.Options{
		NodeID:       cfg.NodeID,
		Region:       cfg.Region,
		ChainID:      cfg.ChainID,
		Ceremony:     cfg.Ceremony,
		Participants: participants,
		Store:        st,
		Audit:        auditLog,
		Transport:    tr,
		Policy:       engine,
		Kernel:       kernel,
		NativePolicy: nativeDoc,
		Ledger:       ledger,
		Clients:      clients,
		Tokens:       tokens,
		Agents:       agents,
		Authority:    authority, Inventory: inventory,
		Activities: registry,
		PeerProbe:  server.TCPPeerProbe(probe),
		Replica:    replicaState,
		Snapshots:  snapshots,
	})
	if err != nil {
		return err
	}
	tlsCfg, err := server.APITLSConfig(cfg.TLSCertFile, cfg.TLSKeyFile, clients)
	if err != nil {
		return err
	}

	peerListener, err := net.Listen("tcp", cfg.PeerListenAddr)
	if err != nil {
		return err
	}
	apiListener, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		peerListener.Close()
		return err
	}
	peerErr := make(chan error, 1)
	go func() { peerErr <- tr.Serve(peerListener) }()
	api := srv.Serve(apiListener, tlsCfg)
	if ready != nil {
		ready(listening{API: apiListener.Addr().String(), Peer: peerListener.Addr().String()})
	}
	select {
	case <-ctx.Done():
	case err := <-peerErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			_ = api.Close()
			return err
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return api.Shutdown(shutdownCtx)
}
