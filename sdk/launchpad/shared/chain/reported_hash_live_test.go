package chain

import (
	"context"
	"errors"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/protocol/bindings"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/chain/profile"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/chain/readpolicy"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/capabilities"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Live chain-125 facts. Block 33963716 is the creation block of pool
// livePool; the node reports livePoolCreationHash for it while go-ethereum
// recomputes livePoolCreationRecomputed from the same header.
var (
	livePoolCreationHeight     = uint64(33963716)
	livePoolCreationHash       = common.HexToHash("0x81a18d822e327fbcbfe2be91b7f666bfa043016cb0e3f094c5bbabe91f9d192b")
	livePoolCreationRecomputed = common.HexToHash("0xe83bd786ee2456fe683b1cc6a7c4b9755783d09071d189ec20dc5a565c6f6292")
	livePool                   = common.HexToAddress("0xCE20Da50F92D21d8BeE497910c782E9Aa25b6263")
	livePoolProxyRuntimeHash   = common.HexToHash("0xa9b304d6d0aa562f283b45978b84ced8840a9331f8ac946742670b1b02a2872d")
)

// liveEndpoints returns the chain-125 RPC (PAXEER_RPC_URL, e.g.
// https://api5.mainnet-beta.paxeer.network) and archive
// (PAXEER_ARCHIVE_RPC_URL, e.g. https://api2.mainnet-beta.paxeer.network).
func liveEndpoints(t *testing.T) (string, string) {
	t.Helper()
	primary, archive := os.Getenv("PAXEER_RPC_URL"), os.Getenv("PAXEER_ARCHIVE_RPC_URL")
	if primary == "" || archive == "" {
		t.Skip("PAXEER_RPC_URL and PAXEER_ARCHIVE_RPC_URL not set")
	}
	return primary, archive
}

// TestLivePaxeerPinnedReadsUseReportedHash proves against the live chain that
// go-ethereum's recomputed header hash is not the block identity, that EIP-1898
// by that hash names no block, and that Pin/ReadPinned/LogsBounded/anchor
// probing work from the node-reported hash.
func TestLivePaxeerPinnedReadsUseReportedHash(t *testing.T) {
	primary, archive := liveEndpoints(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	raw, err := ethclient.DialContext(ctx, primary)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()

	head, err := raw.BlockNumber(ctx)
	if err != nil {
		t.Fatal(err)
	}
	height := head - 3
	reported, err := reportedBlockByNumber(ctx, raw, new(big.Int).SetUint64(height))
	if err != nil {
		t.Fatal(err)
	}
	recomputed := reported.Header.Hash()
	t.Logf("block %d reported %s recomputed %s", height, reported.Hash.Hex(), recomputed.Hex())
	if recomputed == reported.Hash {
		t.Fatal("expected Header.Hash() to differ from the node-reported hash on chain 125")
	}
	byHash, err := raw.HeaderByHash(ctx, recomputed)
	if err == nil && byHash != nil {
		t.Fatal("node serves a block by the recomputed header hash", byHash.Number)
	}
	registry := common.HexToAddress(deployment.Current().Roles["PoolRegistry"].Address)
	registryABI, err := bindings.PoolRegistryMetaData.GetAbi()
	if err != nil {
		t.Fatal(err)
	}
	isRegistered, err := registryABI.Pack("isRegisteredPool", livePool)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.CallContractAtHash(ctx, ethereum.CallMsg{To: &registry, Data: isRegistered}, recomputed); err == nil {
		t.Fatal("EIP-1898 call by the recomputed header hash unexpectedly succeeded")
	} else {
		t.Logf("EIP-1898 by recomputed hash fails as expected: %v", err)
	}

	creation, err := ethclient.DialContext(ctx, archive)
	if err != nil {
		t.Fatal(err)
	}
	defer creation.Close()
	anchorBlock, err := reportedBlockByNumber(ctx, creation, new(big.Int).SetUint64(livePoolCreationHeight))
	if err != nil {
		t.Fatal(err)
	}
	if anchorBlock.Hash != livePoolCreationHash || anchorBlock.Header.Hash() != livePoolCreationRecomputed {
		t.Fatal("creation block identity changed", anchorBlock.Hash.Hex(), anchorBlock.Header.Hash().Hex())
	}

	cfg := VerifiedConfig{Profile: profile.Paxeer("live-reported-hash", deployment.Current().DeploymentVersion), AnchorHeight: livePoolCreationHeight, AnchorHash: livePoolCreationRecomputed}
	if c, err := NewVerifiedClient(ctx, primary, "", cfg); !errors.Is(err, ErrRecomputedAnchor) {
		if c != nil {
			c.Close()
		}
		t.Fatal("recomputed anchor not rejected as such", err)
	}
	cfg.AnchorHash = livePoolCreationHash
	// The archive answers in 1-6 s, beyond readpolicy.DefaultDeadline, so the
	// verified client pairs the primary with itself; the archive is consulted
	// directly above for the creation block identity.
	client, err := NewVerifiedClient(ctx, primary, primary, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	for id, a := range client.EndpointAttestations() {
		if a.State != "verified" || a.AnchorHash != livePoolCreationHash {
			t.Fatal("endpoint not verified against the reported anchor", id, a)
		}
	}

	pin, err := client.Pin(ctx, height)
	if err != nil {
		t.Fatal(err)
	}
	if pin.Hash != reported.Hash || pin.Hash == recomputed {
		t.Fatal("pin does not carry the node-reported hash", pin.Hash.Hex())
	}
	out, err := client.ReadPinned(ctx, pin, []ethereum.CallMsg{{To: &registry, Data: isRegistered}})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := registryABI.Unpack("isRegisteredPool", out[0])
	if err != nil || len(decoded) != 1 || decoded[0] != true {
		t.Fatal("pinned registry read", decoded, err)
	}
	t.Logf("ReadPinned at %d/%s: isRegisteredPool(%s)=%v", pin.Number, pin.Hash.Hex(), livePool.Hex(), decoded[0])

	anchorPin, err := client.Pin(ctx, livePoolCreationHeight)
	if err != nil {
		t.Fatal(err)
	}
	if anchorPin.Hash != livePoolCreationHash {
		t.Fatal("archive pin hash", anchorPin.Hash.Hex())
	}
	logs, err := client.LogsBounded(ctx, ethereum.FilterQuery{FromBlock: new(big.Int).SetUint64(livePoolCreationHeight), ToBlock: new(big.Int).SetUint64(livePoolCreationHeight)}, readpolicy.MaxRange)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) == 0 {
		t.Fatal("creation block logs missing")
	}
	for _, l := range logs {
		if l.BlockHash != livePoolCreationHash {
			t.Fatal("log block identity", l.BlockHash.Hex())
		}
	}
	t.Logf("LogsBounded(%d): %d logs, all at reported hash", livePoolCreationHeight, len(logs))

	submitted, err := readpolicy.Submitted(logs[0].TxHash, cfg.Profile.DeploymentVersion)
	if err != nil {
		t.Fatal(err)
	}
	state, status, err := client.ReceiptState(ctx, submitted)
	if err != nil {
		t.Fatal(err)
	}
	if state.BlockHash == nil || *state.BlockHash != livePoolCreationHash || status.Receipt == nil || status.Receipt.BlockHash != livePoolCreationHash.Hex() {
		t.Fatal("receipt not bound to the reported block hash", state, status)
	}
	t.Logf("ReceiptState(%s): %s at %s", logs[0].TxHash.Hex(), state.State, state.BlockHash.Hex())
}

// TestLivePaxeerAttestationAndHeadUseReportedHash runs the read adapter's
// head observation and its pinned deployment attestation against the live
// chain. The attestation runs through atPinned/pinnedStateReader exactly as
// ReadAdapter.attest does, under an explicit budget: its ~54 sequential RPCs do
// not fit readVerified's DefaultDeadline/2 attempt window at this network
// distance (see the report), which is independent of block identity.
func TestLivePaxeerAttestationAndHeadUseReportedHash(t *testing.T) {
	primary, _ := liveEndpoints(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	raw, err := ethclient.DialContext(ctx, primary)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	anchor, err := reportedBlockByNumber(ctx, raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	manifest := deployment.Current()
	client, err := NewVerifiedClient(ctx, primary, primary, VerifiedConfig{Profile: profile.Paxeer("live-reported-hash", manifest.DeploymentVersion), AnchorHeight: anchor.Header.Number.Uint64(), AnchorHash: anchor.Hash})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	adapter := &ReadAdapter{client: client, manifest: manifest, headPermit: make(chan struct{}, 1), historicalPermit: make(chan struct{}, 1)}
	observed, err := adapter.ObserveHead(ctx)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := reportedBlockByNumber(ctx, raw, new(big.Int).SetUint64(observed.Height-1))
	if err != nil {
		t.Fatal(err)
	}
	current, err := reportedBlockByNumber(ctx, raw, new(big.Int).SetUint64(observed.Height))
	if err != nil {
		t.Fatal(err)
	}
	if observed.Hash != current.Hash || observed.Hash == current.Header.Hash() || observed.ParentHash != parent.Hash {
		t.Fatal("observed head is not the node-reported identity", observed)
	}
	pin, err := adapter.Pin(ctx, observed.Height)
	if err != nil || pin.Hash != observed.Hash {
		t.Fatal("head pin", pin, err)
	}
	var att deployment.ActivationAttestation
	err = client.atPinned(ctx, client.primary, pin, func(height *big.Int, header *types.Header) error {
		var err error
		att, err = deployment.Attest(ctx, pinnedStateReader{e: client.primary, hash: pin.Hash, height: height, header: header}, manifest, pin.Hash, deployment.Policy{Now: time.Now().UTC(), MaxAge: time.Minute, Timeout: time.Minute})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if !att.Eligible || att.BlockNumber != pin.Number || !strings.EqualFold(att.BlockHash, pin.Hash.Hex()) {
		t.Fatal("attestation not bound to the reported pin", att.Eligible, att.BlockNumber, att.BlockHash, att.Issues)
	}
	family, err := capabilities.AssessFamily(capabilities.PlainQuote, manifest, att, deployment.Policy{Now: time.Now().UTC(), MaxAge: time.Minute, Timeout: time.Minute})
	if err != nil || !family.Supported {
		t.Fatal("plain quote family", family, err)
	}
	t.Logf("head %d reported %s parent %s; attestation eligible at reported hash", observed.Height, observed.Hash.Hex(), observed.ParentHash.Hex())
}
