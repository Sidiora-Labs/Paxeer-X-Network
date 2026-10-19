package tasks

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	vestingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/vesting/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/genutil"
	genutiltypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/genutil/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/sei-protocol/seictl/sidecar/engine"
	seis3 "github.com/sei-protocol/seictl/sidecar/s3"
	"github.com/sei-protocol/seictl/sidecar/wire"
	"github.com/sei-protocol/seilog"
)

var assembleLog = seilog.NewLogger("seictl", "task", "assemble-genesis")

const assembleMarkerFile = ".sei-sidecar-assemble-done"

// assembledGentxSubdir holds the assembler's downloaded gentxs, kept separate
// from config/gentx/ (which generate-gentx and upload-genesis-artifacts use) so
// the assembler can't collect its own gentx-<nodeID>.json twice.
const assembledGentxSubdir = "gentx-assembled"

// maxGentxBytes caps a single gentx download (a gentx is a few KB) so a wrong or
// oversized S3 object can't be read wholesale into memory.
const maxGentxBytes = 1 << 20 // 1 MiB

// GenesisAssembler downloads per-node gentx files from S3, calls
// genutil.GenAppStateFromConfig (the same function as seid collect-gentxs)
// to produce the final genesis.json, and uploads it back to S3 for all
// validators to download.
type GenesisAssembler struct {
	homeDir           string
	bucket            string
	region            string
	chainID           string
	s3ClientFactory   S3ClientFactory
	s3UploaderFactory seis3.UploaderFactory
}

// AssembleNodeEntry represents a single node in the "nodes" list param.
type AssembleNodeEntry struct {
	Name string `json:"name"`
}

// GenesisAccountEntry and GenesisAccountVesting are the wire contract, aliased
// here so handler code keeps writing the bare names. sidecar/client aliases the
// same definitions, so the payload this package unmarshals and the request the
// client builds are one type — their json tags cannot drift apart.
type (
	GenesisAccountEntry   = wire.GenesisAccountEntry
	GenesisAccountVesting = wire.GenesisAccountVesting
)

// AssembleGenesisResult is the task's structured result, emitted in-band over
// the trusted controller↔sidecar task-result channel. GenesisHash is the bare
// SHA-256 hex digest (no "sha256:" prefix) of the exact uploaded genesis.json
// bytes; the controller stamps status.genesisHash from it and plumbs it into
// followers' ConfigureGenesisTask.ExpectedGenesisHash.
type AssembleGenesisResult struct {
	GenesisHash string `json:"genesisHash"`
}

// AssembleGenesisRequest holds the typed parameters for the assemble-and-upload-genesis task.
// S3 bucket, region, and prefix are derived from the sidecar's environment.
//
// Overrides is a flat map of dotted-path keys into genesis.app_state to
// raw JSON values. The first dotted token is the cosmos module name (a key
// in app_state); subsequent tokens walk into that module's JSON tree. The
// leaf value is replaced verbatim with the supplied json.RawMessage. The
// controller enforces immutability of these keys post-bootstrap via CEL;
// the sidecar applies them once during the genesis ceremony.
type AssembleGenesisRequest struct {
	AccountBalance string                     `json:"accountBalance"`
	Namespace      string                     `json:"namespace"`
	Nodes          []AssembleNodeEntry        `json:"nodes"`
	Accounts       []GenesisAccountEntry      `json:"accounts,omitempty"`
	Overrides      map[string]json.RawMessage `json:"overrides,omitempty"`
}

// nodeNames returns the list of node name strings from the Nodes entries.
func (c AssembleGenesisRequest) nodeNames() []string {
	names := make([]string, len(c.Nodes))
	for i, n := range c.Nodes {
		names[i] = n.Name
	}
	return names
}

// NewGenesisAssembler creates an assembler targeting the given home directory.
func NewGenesisAssembler(homeDir, bucket, region, chainID string, s3Factory S3ClientFactory, uploaderFactory seis3.UploaderFactory) *GenesisAssembler {
	if s3Factory == nil {
		s3Factory = DefaultS3ClientFactory
	}
	if uploaderFactory == nil {
		uploaderFactory = seis3.DefaultUploaderFactory
	}
	return &GenesisAssembler{
		homeDir:           homeDir,
		bucket:            bucket,
		region:            region,
		chainID:           chainID,
		s3ClientFactory:   s3Factory,
		s3UploaderFactory: uploaderFactory,
	}
}

// Handler returns an engine.TaskHandler for the assemble-and-upload-genesis task type.
// S3 coordinates are derived from the sidecar's environment.
func (a *GenesisAssembler) Handler() engine.TaskHandler {
	return engine.TypedHandlerWithResult(func(ctx context.Context, cfg AssembleGenesisRequest) (*AssembleGenesisResult, error) {
		if markerExists(a.homeDir, assembleMarkerFile) {
			assembleLog.Debug("already completed, skipping")
			return nil, nil
		}

		if cfg.AccountBalance == "" {
			return nil, fmt.Errorf("assemble-genesis: missing required param 'accountBalance'")
		}
		if cfg.Namespace == "" {
			return nil, fmt.Errorf("assemble-genesis: missing required param 'namespace'")
		}
		if len(cfg.Nodes) == 0 {
			return nil, fmt.Errorf("assemble-genesis: 'nodes' list is empty")
		}
		for i, n := range cfg.Nodes {
			if n.Name == "" {
				return nil, fmt.Errorf("assemble-genesis: nodes[%d] missing required field 'name'", i)
			}
		}

		nodes := cfg.nodeNames()

		if err := a.downloadGentxFiles(ctx, cfg, nodes); err != nil {
			return nil, err
		}

		if err := a.verifyAssembledGentxs(nodes); err != nil {
			return nil, err
		}

		if err := a.addMissingGenesisAccounts(cfg.AccountBalance); err != nil {
			return nil, err
		}

		if err := a.addExternalGenesisAccounts(cfg.Accounts); err != nil {
			return nil, err
		}

		if err := a.collectGentxs(); err != nil {
			return nil, err
		}

		if err := a.applyOverrides(cfg.Overrides); err != nil {
			return nil, err
		}

		if err := a.populateGenesisValidators(); err != nil {
			return nil, err
		}

		genesisHash, err := a.uploadGenesis(ctx, cfg)
		if err != nil {
			return nil, err
		}

		if err := a.uploadPeers(ctx, cfg, nodes); err != nil {
			return nil, err
		}

		if err := writeMarker(a.homeDir, assembleMarkerFile); err != nil {
			return nil, err
		}

		// Hand the hash to the controller over the trusted task-result
		// channel (GET /v0/tasks/{id}); never via shared S3.
		assembleLog.Info("genesis assembled and uploaded", "nodes", len(nodes), "genesisHash", genesisHash)
		return &AssembleGenesisResult{GenesisHash: genesisHash}, nil
	})
}

// assembledGentxDir is the isolated dir the assembler collects from; see
// assembledGentxSubdir.
func (a *GenesisAssembler) assembledGentxDir() string {
	return filepath.Join(a.homeDir, "config", assembledGentxSubdir)
}

// downloadGentxFiles wipes the assemble dir and refills it with exactly one
// gentx per node from S3, so collect reads precisely the downloaded set — never
// this node's own generate-gentx output or a leftover from a prior run.
func (a *GenesisAssembler) downloadGentxFiles(ctx context.Context, cfg AssembleGenesisRequest, nodes []string) error {
	s3Client, err := a.s3ClientFactory(ctx, a.region)
	if err != nil {
		return fmt.Errorf("assemble-genesis: building S3 client: %w", err)
	}

	gentxDir := a.assembledGentxDir()
	if err := os.RemoveAll(gentxDir); err != nil {
		return fmt.Errorf("assemble-genesis: clearing assemble dir: %w", err)
	}
	if err := os.MkdirAll(gentxDir, 0o755); err != nil {
		return fmt.Errorf("assemble-genesis: creating assemble dir: %w", err)
	}

	prefix := a.chainID + "/"

	for _, nodeName := range nodes {
		key := fmt.Sprintf("%s%s/gentx.json", prefix, nodeName)
		assembleLog.Info("downloading gentx", "node", nodeName, "key", key)

		output, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(a.bucket),
			Key:    aws.String(key),
		})
		if err != nil {
			return seis3.ClassifyS3Error("assemble-and-upload-genesis", a.bucket, key, a.region, err)
		}

		data, err := io.ReadAll(io.LimitReader(output.Body, maxGentxBytes+1))
		_ = output.Body.Close()
		if err != nil {
			return fmt.Errorf("assemble-genesis: reading %s: %w", key, err)
		}
		if len(data) > maxGentxBytes {
			return fmt.Errorf("assemble-genesis: gentx %s exceeds %d bytes", key, maxGentxBytes)
		}

		destPath := filepath.Join(gentxDir, fmt.Sprintf("gentx-%s.json", nodeName))
		if err := os.WriteFile(destPath, data, 0o644); err != nil {
			return fmt.Errorf("assemble-genesis: writing %s: %w", destPath, err)
		}
	}

	assembleLog.Info("all gentx files downloaded", "count", len(nodes))
	return nil
}

// verifyAssembledGentxs requires exactly one MsgCreateValidator per expected
// node, all for distinct validators, before genesis is mutated. Iterating node
// names also catches a missing gentx. gentxs are self-delegating, so a validator
// is identified equally by delegator, operator address, and consensus pubkey;
// any collision means it'd be created twice, which panics/aborts InitChain and
// wedges the chain. Rejecting here yields a clear error instead.
func (a *GenesisAssembler) verifyAssembledGentxs(nodes []string) error {
	_, txCfg := makeCodec()
	ensureBech32()

	gentxDir := a.assembledGentxDir()

	// Each map records identity -> node name, so a collision names both nodes.
	seenDelegator := make(map[string]string, len(nodes))
	seenValidator := make(map[string]string, len(nodes))
	seenPubKey := make(map[string]string, len(nodes))
	for _, nodeName := range nodes {
		data, err := os.ReadFile(filepath.Join(gentxDir, fmt.Sprintf("gentx-%s.json", nodeName)))
		if err != nil {
			return fmt.Errorf("assemble-genesis: reading gentx for node %s: %w", nodeName, err)
		}
		tx, err := txCfg.TxJSONDecoder()(data)
		if err != nil {
			return fmt.Errorf("assemble-genesis: decoding gentx for node %s: %w", nodeName, err)
		}
		msgs := tx.GetMsgs()
		if len(msgs) != 1 {
			return fmt.Errorf("assemble-genesis: gentx for node %s has %d messages, want exactly 1 MsgCreateValidator", nodeName, len(msgs))
		}
		msg, ok := msgs[0].(*stakingtypes.MsgCreateValidator)
		if !ok {
			return fmt.Errorf("assemble-genesis: gentx for node %s is not a MsgCreateValidator", nodeName)
		}

		// Guard nil so a malformed gentx can't panic before x/staking rejects it.
		pubKey := ""
		if msg.Pubkey != nil {
			pubKey = msg.Pubkey.String()
		}

		dedupe := func(kind, key string, seen map[string]string) error {
			if key == "" {
				return nil
			}
			if prev, dup := seen[key]; dup {
				return fmt.Errorf("assemble-genesis: duplicate %s %q (nodes %s and %s); "+
					"the same validator would be created twice and abort InitChain",
					kind, key, prev, nodeName)
			}
			seen[key] = nodeName
			return nil
		}
		if err := dedupe("delegator", msg.DelegatorAddress, seenDelegator); err != nil {
			return err
		}
		if err := dedupe("validator operator address", msg.ValidatorAddress, seenValidator); err != nil {
			return err
		}
		if err := dedupe("consensus pubkey", pubKey, seenPubKey); err != nil {
			return err
		}
	}

	assembleLog.Info("assembled gentxs verified", "count", len(nodes))
	return nil
}

// addMissingGenesisAccounts parses each downloaded gentx to extract the
// delegator address, then adds any accounts not already present in the
// assembler's local genesis.json. This is necessary because each node only
// adds its own account during generate-gentx, but collect-gentxs validates
// that every gentx's delegator exists in genesis state.
func (a *GenesisAssembler) addMissingGenesisAccounts(accountBalance string) error {
	cdc, txCfg := makeCodec()
	ensureBech32()

	gentxDir := a.assembledGentxDir()
	entries, err := os.ReadDir(gentxDir)
	if err != nil {
		return fmt.Errorf("assemble-genesis: reading assemble dir: %w", err)
	}

	genFile := filepath.Join(a.homeDir, "config", "genesis.json")
	appState, genDoc, err := genutiltypes.GenesisStateFromGenFile(genFile)
	if err != nil {
		return fmt.Errorf("assemble-genesis: reading genesis: %w", err)
	}

	authGenState := authtypes.GetGenesisStateFromAppState(cdc, appState)
	accs, err := authtypes.UnpackAccounts(authGenState.Accounts)
	if err != nil {
		return fmt.Errorf("assemble-genesis: unpacking accounts: %w", err)
	}

	bankGenState := banktypes.GetGenesisStateFromAppState(cdc, appState)

	coins, err := sdk.ParseCoinsNormalized(accountBalance)
	if err != nil {
		return fmt.Errorf("assemble-genesis: parsing account balance: %w", err)
	}

	added := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(gentxDir, entry.Name()))
		if err != nil {
			return fmt.Errorf("assemble-genesis: reading %s: %w", entry.Name(), err)
		}

		tx, err := txCfg.TxJSONDecoder()(data)
		if err != nil {
			return fmt.Errorf("assemble-genesis: decoding %s: %w", entry.Name(), err)
		}

		msgs := tx.GetMsgs()
		if len(msgs) == 0 {
			continue
		}
		msg, ok := msgs[0].(*stakingtypes.MsgCreateValidator)
		if !ok {
			continue
		}

		addr, err := sdk.AccAddressFromBech32(msg.DelegatorAddress)
		if err != nil {
			return fmt.Errorf("assemble-genesis: parsing delegator address from %s: %w", entry.Name(), err)
		}

		if accs.Contains(addr) {
			continue
		}

		accs = append(accs, authtypes.NewBaseAccount(addr, nil, 0, 0))
		bankGenState.Balances = append(bankGenState.Balances, banktypes.Balance{
			Address: addr.String(),
			Coins:   coins.Sort(),
		})
		added++
		assembleLog.Info("added missing genesis account", "address", addr.String())
	}

	if added == 0 {
		return nil
	}

	if err := writeBackAuthAndBank(cdc, genFile, genDoc, appState, authGenState, accs, bankGenState); err != nil {
		return fmt.Errorf("assemble-genesis: %w", err)
	}

	assembleLog.Info("genesis accounts reconciled", "added", added, "total", len(accs))
	return nil
}

// Run after addMissingGenesisAccounts so collisions catch validator-
// derived addresses. Non-fork's empty Supply lets bank.InitGenesis
// recompute from balances; the fork mirror updates Supply explicitly.
func (a *GenesisAssembler) addExternalGenesisAccounts(accounts []GenesisAccountEntry) error {
	if len(accounts) == 0 {
		return nil
	}

	cdc, _ := makeCodec()
	ensureBech32()

	genFile := filepath.Join(a.homeDir, "config", "genesis.json")
	appState, genDoc, err := genutiltypes.GenesisStateFromGenFile(genFile)
	if err != nil {
		return fmt.Errorf("assemble-genesis: reading genesis: %w", err)
	}

	authGenState := authtypes.GetGenesisStateFromAppState(cdc, appState)
	accs, err := authtypes.UnpackAccounts(authGenState.Accounts)
	if err != nil {
		return fmt.Errorf("assemble-genesis: unpacking accounts: %w", err)
	}

	bankGenState := banktypes.GetGenesisStateFromAppState(cdc, appState)

	for _, entry := range accounts {
		addr, err := sdk.AccAddressFromBech32(entry.Address)
		if err != nil {
			return fmt.Errorf("assemble-genesis: external account %q: %w", entry.Address, err)
		}
		if accs.Contains(addr) {
			return fmt.Errorf("assemble-genesis: external account %s collides with an existing genesis account", addr.String())
		}
		coins, err := sdk.ParseCoinsNormalized(entry.Balance)
		if err != nil {
			return fmt.Errorf("assemble-genesis: external account %s balance %q: %w", addr.String(), entry.Balance, err)
		}

		baseAccount := authtypes.NewBaseAccount(addr, nil, 0, 0)
		var acc authtypes.GenesisAccount = baseAccount

		if entry.Vesting != nil {
			vestingCoins, err := sdk.ParseCoinsNormalized(entry.Vesting.Amount)
			if err != nil {
				return fmt.Errorf("assemble-genesis: external account %s vesting amount %q: %w", addr.String(), entry.Vesting.Amount, err)
			}
			// IsAllPositive (not Empty): ParseCoinsNormalized truncates a
			// fractional base-denom amount ("0.4usei") to {usei:0}, which is
			// non-empty yet locks nothing — Empty() would miss it. IsAllPositive
			// is false for both the empty and the zero-valued case.
			if !vestingCoins.IsAllPositive() {
				return fmt.Errorf("assemble-genesis: external account %s vesting amount %q must be a positive coin amount (fractional base-denom values truncate to zero)", addr.String(), entry.Vesting.Amount)
			}
			if !coins.IsAllGTE(vestingCoins) {
				return fmt.Errorf("assemble-genesis: external account %s vesting amount %s exceeds balance %s", addr.String(), vestingCoins, coins)
			}
			// EndTime must be strictly after genesis time for both
			// schedules: a continuous account with EndTime <= StartTime
			// is fully vested from block 1 (ContinuousVestingAccount's own
			// Validate rejects StartTime >= EndTime, but only if something
			// calls Validate — see below); a delayed account has no
			// start/end ordering check at all, so an EndTime in the past
			// silently unlocks everything at genesis with no error either
			// way. Both would defeat the entire purpose of a vesting
			// fixture: a balance that's supposed to be locked.
			if entry.Vesting.EndTime <= genDoc.GenesisTime.Unix() {
				return fmt.Errorf("assemble-genesis: external account %s vesting end time %d must be after genesis time %d",
					addr.String(), entry.Vesting.EndTime, genDoc.GenesisTime.Unix())
			}
			// Mirrors msg_server.go's (deprecated-for-live-tx) CreateVestingAccount
			// handler, with the genesis timestamp standing in for
			// ctx.BlockTime() (no live "now" at genesis-assembly time) and no
			// admin (no live tx to have named one).
			baseVestingAccount := vestingtypes.NewBaseVestingAccount(baseAccount, vestingCoins.Sort(), entry.Vesting.EndTime, nil)
			if entry.Vesting.Delayed {
				acc = vestingtypes.NewDelayedVestingAccountRaw(baseVestingAccount)
			} else {
				acc = vestingtypes.NewContinuousVestingAccountRaw(baseVestingAccount, genDoc.GenesisTime.Unix())
			}
			// Belt-and-suspenders: auth.InitGenesis never calls Validate()
			// on genesis accounts (only the separate validate-genesis CLI
			// path does), so an invalid vesting schedule would otherwise
			// reach InitChain unguarded.
			if err := acc.Validate(); err != nil {
				return fmt.Errorf("assemble-genesis: external account %s vesting schedule: %w", addr.String(), err)
			}
		}

		accs = append(accs, acc)
		bankGenState.Balances = append(bankGenState.Balances, banktypes.Balance{
			Address: addr.String(),
			Coins:   coins.Sort(),
		})
		assembleLog.Info("added external genesis account", "address", addr.String(), "balance", entry.Balance, "vesting", entry.Vesting != nil)
	}

	if err := writeBackAuthAndBank(cdc, genFile, genDoc, appState, authGenState, accs, bankGenState); err != nil {
		return fmt.Errorf("assemble-genesis: %w", err)
	}
	return nil
}

// collectGentxs calls genutil.GenAppStateFromConfig — the exact same
// function behind seid collect-gentxs. It decodes each gentx through
// the proto codec, validates balances, extracts persistent peers,
// and writes the final genesis.json.
func (a *GenesisAssembler) collectGentxs() error {
	assembleLog.Info("running collect-gentxs via SDK")

	cdc, txCfg := makeCodec()
	ensureBech32()

	cfg := tmcfg.DefaultConfig()
	cfg.SetRoot(a.homeDir)

	nodeID, valPubKey, err := genutil.InitializeNodeValidatorFiles(cfg)
	if err != nil {
		return fmt.Errorf("assemble-genesis: loading validator files: %w", err)
	}

	genDoc, err := tmtypes.GenesisDocFromFile(cfg.GenesisFile())
	if err != nil {
		return fmt.Errorf("assemble-genesis: reading genesis: %w", err)
	}

	gentxsDir := a.assembledGentxDir()
	initCfg := genutiltypes.NewInitConfig(genDoc.ChainID, gentxsDir, nodeID, valPubKey)

	genBalIterator := banktypes.GenesisBalancesIterator{}

	_, err = genutil.GenAppStateFromConfig(cdc, txCfg, cfg, initCfg, *genDoc, genBalIterator)
	if err != nil {
		return fmt.Errorf("assemble-genesis: collect-gentxs: %w", err)
	}

	return nil
}

// applyOverrides re-reads the assembled genesis.json, applies the
// caller-supplied app_state overrides, and writes the file back. This runs
// after collectGentxs so the dispatched MsgCreateValidator data and
// derived persistent peers are already baked into app_state — overrides
// are an in-place patch on the final assembled doc.
func (a *GenesisAssembler) applyOverrides(overrides map[string]json.RawMessage) error {
	if len(overrides) == 0 {
		return nil
	}

	genFile := filepath.Join(a.homeDir, "config", "genesis.json")
	genDoc, err := tmtypes.GenesisDocFromFile(genFile)
	if err != nil {
		return fmt.Errorf("assemble-genesis: reading genesis for overrides: %w", err)
	}

	var appState map[string]json.RawMessage
	if err := json.Unmarshal(genDoc.AppState, &appState); err != nil {
		return fmt.Errorf("assemble-genesis: parsing app_state for overrides: %w", err)
	}

	if err := applyGenesisOverrides(appState, overrides); err != nil {
		return fmt.Errorf("assemble-genesis: %w", err)
	}

	appStateJSON, err := json.Marshal(appState)
	if err != nil {
		return fmt.Errorf("assemble-genesis: marshaling overridden app_state: %w", err)
	}
	genDoc.AppState = appStateJSON

	if err := genutil.ExportGenesisFile(genDoc, genFile); err != nil {
		return fmt.Errorf("assemble-genesis: writing genesis after overrides: %w", err)
	}

	assembleLog.Info("applied genesis overrides", "count", len(overrides))
	return nil
}

// uploadGenesis reads the assembled genesis.json and uploads it to S3
// at <prefix>/genesis.json where all validators will fetch it from. It
// returns the bare SHA-256 hex digest (no "sha256:" prefix) computed over the
// exact bytes uploaded — the same []byte handed to PutObject, not a re-read or
// re-serialized form — so the digest matches what a follower will download and
// verify. The digest travels to the controller only in-band, over the trusted
// task-result channel; it is never written to S3, where the prefix is
// attacker-writable and a sibling hash would let a poisoned genesis carry its
// own matching digest.
func (a *GenesisAssembler) uploadGenesis(ctx context.Context, cfg AssembleGenesisRequest) (string, error) {
	genesisPath := filepath.Join(a.homeDir, "config", "genesis.json")
	data, err := os.ReadFile(genesisPath)
	if err != nil {
		return "", fmt.Errorf("assemble-genesis: reading genesis.json: %w", err)
	}

	sum := sha256.Sum256(data)
	genesisHash := hex.EncodeToString(sum[:])

	uploader, err := a.s3UploaderFactory(ctx, a.region)
	if err != nil {
		return "", fmt.Errorf("assemble-genesis: building S3 uploader: %w", err)
	}

	key := a.chainID + "/" + "genesis.json"
	assembleLog.Info("uploading assembled genesis", "key", key, "sha256", genesisHash)

	_, err = uploader.UploadObject(ctx, &transfermanager.UploadObjectInput{
		Bucket:      aws.String(a.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return "", seis3.ClassifyS3Error("assemble-and-upload-genesis", a.bucket, key, a.region, err)
	}

	return genesisHash, nil
}

// uploadPeers builds a peers.json from each node's identity.json and uploads
// it to S3 alongside genesis.json. Each entry is a full Tendermint peer address
// using in-cluster DNS: <nodeID>@<name>-0.<name>.<namespace>.svc.cluster.local:26656
func (a *GenesisAssembler) uploadPeers(ctx context.Context, cfg AssembleGenesisRequest, nodes []string) error {
	s3Client, err := a.s3ClientFactory(ctx, a.region)
	if err != nil {
		return fmt.Errorf("assemble-genesis: building S3 client for peers: %w", err)
	}

	prefix := a.chainID + "/"
	var peers []string

	for _, nodeName := range nodes {
		key := fmt.Sprintf("%s%s/identity.json", prefix, nodeName)
		output, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(a.bucket),
			Key:    aws.String(key),
		})
		if err != nil {
			return seis3.ClassifyS3Error("assemble-and-upload-genesis", a.bucket, key, a.region, err)
		}
		data, err := io.ReadAll(output.Body)
		_ = output.Body.Close()
		if err != nil {
			return fmt.Errorf("assemble-genesis: reading identity for %s: %w", nodeName, err)
		}

		var identity struct {
			NodeKey json.RawMessage `json:"node_key"`
		}
		if err := json.Unmarshal(data, &identity); err != nil {
			return fmt.Errorf("assemble-genesis: parsing identity for %s: %w", nodeName, err)
		}

		var nodeKey struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(identity.NodeKey, &nodeKey); err != nil {
			return fmt.Errorf("assemble-genesis: parsing node_key for %s: %w", nodeName, err)
		}
		if nodeKey.ID == "" {
			return fmt.Errorf("assemble-genesis: empty node ID for %s", nodeName)
		}

		dns := fmt.Sprintf("%s-0.%s.%s.svc.cluster.local", nodeName, nodeName, cfg.Namespace)
		peers = append(peers, fmt.Sprintf("%s@%s:26656", nodeKey.ID, dns))
	}

	peersJSON, err := json.Marshal(peers)
	if err != nil {
		return fmt.Errorf("assemble-genesis: marshaling peers.json: %w", err)
	}

	uploader, err := a.s3UploaderFactory(ctx, a.region)
	if err != nil {
		return fmt.Errorf("assemble-genesis: building S3 uploader for peers: %w", err)
	}

	peersKey := prefix + "peers.json"
	assembleLog.Info("uploading peers.json", "key", peersKey, "count", len(peers))

	_, err = uploader.UploadObject(ctx, &transfermanager.UploadObjectInput{
		Bucket:      aws.String(a.bucket),
		Key:         aws.String(peersKey),
		Body:        bytes.NewReader(peersJSON),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return seis3.ClassifyS3Error("assemble-and-upload-genesis", a.bucket, peersKey, a.region, err)
	}
	return nil
}
