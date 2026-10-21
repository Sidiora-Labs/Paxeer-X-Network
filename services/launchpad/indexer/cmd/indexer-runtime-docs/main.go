// Command indexer-runtime-docs renders the sha256-bound runtime documents of a
// NODE_ENV=production indexer (receipt.json, delivery.json, lifecycle.json)
// from the repository's deployment manifest, reviewed catalog and compiler
// artifacts, anchored at the issued start height:
//
//	indexer-runtime-docs -repo /root/mk-falcon -start-height 33963716 \
//	    -anchor-rpc https://api2.mainnet-beta.paxeer.network -out ./docs
//
// The anchor is block start-height-1, identified by the node-reported hash
// (chain 125 is CometBFT-backed). Each file's sha256 is printed for the
// matching *_SHA256 variable.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/app"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/receipt"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/runtimedocs"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	"github.com/ethereum/go-ethereum/common"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "indexer-runtime-docs:", err)
		os.Exit(1)
	}
}

func run() error {
	repo := flag.String("repo", ".", "repository root")
	start := flag.Uint64("start-height", 0, "first block the fleet ingests (anchor = start-height-1)")
	anchorRPC := flag.String("anchor-rpc", "", "JSON-RPC endpoint that still serves the anchor block (archive)")
	poolRuntime := flag.String("pool-runtime", "0x6302ff80f4168c2af23afbf3117c613edd66d288d120eee825f9ecadd43a392f", "keccak256 of the runtime code every pool holds (SurfaceRuntime after v6.12.0)")
	out := flag.String("out", ".", "output directory")
	flag.Parse()
	manifest := deployment.Current()
	if *start <= manifest.ActivationHeight || *anchorRPC == "" {
		return errors.New("-start-height above the activation height and -anchor-rpc are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	anchor, err := reportedBlockHash(ctx, *anchorRPC, *start-1)
	if err != nil {
		return err
	}
	reviewed, err := os.ReadFile(filepath.Join(*repo, "protocol", "catalog", "reviewed.json"))
	if err != nil {
		return err
	}
	artifact, err := os.ReadFile(filepath.Join(*repo, "contracts", "artifacts", "contracts", "core", "SidioraERC20.sol", "SidioraERC20.json"))
	if err != nil {
		return err
	}
	var compiled struct {
		DeployedBytecode string `json:"deployedBytecode"`
	}
	if err = json.Unmarshal(artifact, &compiled); err != nil {
		return err
	}
	code, err := hex.DecodeString(strings.TrimPrefix(compiled.DeployedBytecode, "0x"))
	if err != nil {
		return err
	}
	docs, err := app.RenderRuntimeDocuments(runtimedocs.Inputs{Manifest: manifest, ReviewedCatalog: reviewed, ERC20RuntimeCode: code, PoolRuntimeHash: common.HexToHash(*poolRuntime), Anchor: receipt.BlockAnchor{Height: *start - 1, Hash: anchor}}, app.DefaultDeliveryParameters())
	if err != nil {
		return err
	}
	names := make([]string, 0, len(docs))
	for name := range docs {
		names = append(names, name)
	}
	sort.Strings(names)
	if err = os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	for _, name := range names {
		if err = os.WriteFile(filepath.Join(*out, name), docs[name].Bytes, 0o644); err != nil {
			return err
		}
		fmt.Printf("%s %s %d\n", docs[name].SHA256, name, len(docs[name].Bytes))
	}
	fmt.Printf("anchor %d %s (start height %d)\n", *start-1, strings.ToLower(anchor.Hex()), *start)
	return nil
}

func reportedBlockHash(ctx context.Context, url string, height uint64) (common.Hash, error) {
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "eth_getBlockByNumber", "params": []any{fmt.Sprintf("0x%x", height), false}})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return common.Hash{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return common.Hash{}, err
	}
	defer response.Body.Close()
	var envelope struct {
		Result *struct {
			Number string      `json:"number"`
			Hash   common.Hash `json:"hash"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err = json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return common.Hash{}, err
	}
	if envelope.Error != nil || envelope.Result == nil || envelope.Result.Number != fmt.Sprintf("0x%x", height) || envelope.Result.Hash == (common.Hash{}) {
		return common.Hash{}, fmt.Errorf("anchor block %d unavailable: %+v", height, envelope.Error)
	}
	return envelope.Result.Hash, nil
}
