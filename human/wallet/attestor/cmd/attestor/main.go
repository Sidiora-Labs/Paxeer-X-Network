package main

import (
	"context"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/audit"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/auth/jwt"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/config"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/health"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/lxwire"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/policy"
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
	if err := run(ctx, os.Getenv, nil); err != nil {
		log.Fatal(err)
	}
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

	var doc *policy.Document
	if cfg.PolicyFile != "" {
		if doc, err = policy.LoadFile(cfg.PolicyFile); err != nil {
			return err
		}
	}
	var tokens *jwt.TokenVerifier
	if cfg.JWKSURL != "" {
		tokens, err = jwt.NewTokenVerifier(jwt.Config{
			JWKSURL:    cfg.JWKSURL,
			Issuer:     cfg.JWTIssuer,
			Audience:   cfg.JWTAudience,
			HTTPClient: &http.Client{Timeout: 10 * time.Second},
		})
		if err != nil {
			return err
		}
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
	if replicaCfg != nil {
		shipper, err := replica.New(*replicaCfg, log.Default())
		if err != nil {
			return err
		}
		replicaState = shipper.State
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

	srv, err := server.New(server.Options{
		NodeID:       cfg.NodeID,
		Region:       cfg.Region,
		ChainID:      cfg.ChainID,
		Ceremony:     cfg.Ceremony,
		Participants: participants,
		Store:        st,
		Audit:        auditLog,
		Transport:    tr,
		Policy:       policy.New(doc),
		Ledger:       policy.NewMemoryLedger(time.Now),
		Tokens:       tokens,
		Activities:   registry,
		PeerProbe:    server.TCPPeerProbe(probe),
		Replica:      replicaState,
	})
	if err != nil {
		return err
	}
	tlsCfg, err := server.APITLSConfig(cfg.TLSCertFile, cfg.TLSKeyFile, cfg.OperatorCAFile)
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
