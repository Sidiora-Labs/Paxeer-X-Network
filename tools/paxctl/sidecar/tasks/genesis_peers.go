package tasks

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/sei-protocol/seictl/sidecar/engine"
	seis3 "github.com/sei-protocol/seictl/sidecar/s3"
	"github.com/sei-protocol/seilog"
)

var genesisPeersLog = seilog.NewLogger("seictl", "task", "set-genesis-peers")

const (
	ed25519PrivKeyLen   = 64
	ed25519PubKeyOffset = 32
	cometbftAddressLen  = 20
)

// SetGenesisPeersRequest holds the typed parameters for the set-genesis-peers task.
// S3 coordinates are derived from the sidecar's environment.
type SetGenesisPeersRequest struct{}

// GenesisPeersSetter downloads a peers.json file produced by the genesis
// assembler and writes the entries into config.toml as persistent_peers,
// filtering out the current node's own entry.
type GenesisPeersSetter struct {
	homeDir         string
	bucket          string
	region          string
	chainID         string
	s3ClientFactory S3ClientFactory
}

// NewGenesisPeersSetter creates a setter targeting the given home directory.
func NewGenesisPeersSetter(homeDir, bucket, region, chainID string, s3Factory S3ClientFactory) *GenesisPeersSetter {
	if s3Factory == nil {
		s3Factory = DefaultS3ClientFactory
	}
	return &GenesisPeersSetter{
		homeDir:         homeDir,
		bucket:          bucket,
		region:          region,
		chainID:         chainID,
		s3ClientFactory: s3Factory,
	}
}

// Handler returns an engine.TaskHandler for the set-genesis-peers task.
// The peers.json key is derived from the chain ID: {chainID}/peers.json.
func (g *GenesisPeersSetter) Handler() engine.TaskHandler {
	return engine.TypedHandler(func(ctx context.Context, _ SetGenesisPeersRequest) error {
		key := g.chainID + "/peers.json"

		s3Client, err := g.s3ClientFactory(ctx, g.region)
		if err != nil {
			return fmt.Errorf("set-genesis-peers: building S3 client: %w", err)
		}

		output, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(g.bucket),
			Key:    aws.String(key),
		})
		if err != nil {
			return seis3.ClassifyS3Error("set-genesis-peers", g.bucket, key, g.region, err)
		}
		data, err := io.ReadAll(output.Body)
		_ = output.Body.Close()
		if err != nil {
			return fmt.Errorf("set-genesis-peers: reading peers.json: %w", err)
		}

		var allPeers []string
		if err := json.Unmarshal(data, &allPeers); err != nil {
			return fmt.Errorf("set-genesis-peers: parsing peers.json: %w", err)
		}

		selfID, err := readLocalNodeID(g.homeDir)
		if err != nil {
			return err
		}

		var filtered []string
		for _, peer := range allPeers {
			if !strings.HasPrefix(peer, selfID+"@") {
				filtered = append(filtered, peer)
			}
		}

		genesisPeersLog.Info("applying genesis peers",
			"total", len(allPeers), "self", selfID, "peers", len(filtered))

		return writePeersToConfig(g.homeDir, filtered)
	})
}

// readLocalNodeID derives the Tendermint node ID from the Ed25519 key in
// node_key.json. The ID is hex(SHA256(pubkey)[:20]), matching CometBFT's
// p2p.PubKeyToID derivation.
func readLocalNodeID(homeDir string) (string, error) {
	path := filepath.Join(homeDir, "config", "node_key.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("set-genesis-peers: reading node_key.json: %w", err)
	}

	var keyFile struct {
		PrivKey struct {
			Value string `json:"value"`
		} `json:"priv_key"`
	}
	if err := json.Unmarshal(data, &keyFile); err != nil {
		return "", fmt.Errorf("set-genesis-peers: parsing node_key.json: %w", err)
	}

	keyBytes, err := base64.StdEncoding.DecodeString(keyFile.PrivKey.Value)
	if err != nil || len(keyBytes) != ed25519PrivKeyLen {
		return "", fmt.Errorf("set-genesis-peers: invalid Ed25519 key in node_key.json")
	}

	pubKey := keyBytes[ed25519PubKeyOffset:]
	hash := sha256.Sum256(pubKey)
	return hex.EncodeToString(hash[:cometbftAddressLen]), nil
}
