package tasks

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	ethcrypto "github.com/ethereum/go-ethereum/crypto"

	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/tx"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec/legacy"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	cryptocodec "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/codec"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/hd"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keyring"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	authclient "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/client"
	authtx "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/tx"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	vestingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/vesting/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"

	"github.com/sei-protocol/sei-chain/sei-cosmos/x/genutil"
	genutiltypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/genutil/types"
	stakingcli "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/client/cli"

	"github.com/sei-protocol/seictl/sidecar/engine"
	"github.com/sei-protocol/seilog"
)

var gentxLog = seilog.NewLogger("seictl", "task", "generate-gentx")

const (
	gentxMarkerFile  = ".sei-sidecar-gentx-done"
	validatorKeyName = "validator"
)

// GenerateGentxRequest holds the typed parameters for the generate-gentx task.
type GenerateGentxRequest struct {
	ChainID        string `json:"chainId"`
	StakingAmount  string `json:"stakingAmount"`
	AccountBalance string `json:"accountBalance"`
}

// GentxGenerator produces a genesis transaction by calling the same SDK
// functions as seid keys add -> seid add-genesis-account -> seid gentx.
type GentxGenerator struct {
	homeDir string
}

// NewGentxGenerator creates a generator targeting the given home directory.
func NewGentxGenerator(homeDir string) *GentxGenerator {
	return &GentxGenerator{homeDir: homeDir}
}

// Handler returns an engine.TaskHandler for the generate-gentx task type.
//
// Expected params:
//
//	{
//	  "chainId":        "my-chain",
//	  "stakingAmount":  "1000000usei",
//	  "accountBalance": "10000000usei"
//	}
func (g *GentxGenerator) Handler() engine.TaskHandler {
	return engine.TypedHandler(func(ctx context.Context, params GenerateGentxRequest) error {
		if markerExists(g.homeDir, gentxMarkerFile) {
			gentxLog.Debug("already completed, skipping")
			return nil
		}

		if params.ChainID == "" {
			return fmt.Errorf("generate-gentx: missing required param 'chainId'")
		}
		if params.StakingAmount == "" {
			return fmt.Errorf("generate-gentx: missing required param 'stakingAmount'")
		}
		if params.AccountBalance == "" {
			return fmt.Errorf("generate-gentx: missing required param 'accountBalance'")
		}

		cdc, txCfg := makeCodec()
		ensureBech32()

		address, err := g.addValidatorKey(cdc)
		if err != nil {
			return err
		}

		if err := g.addGenesisAccount(cdc, address, params.AccountBalance); err != nil {
			return err
		}

		if err := g.generateGentx(cdc, txCfg, params.ChainID, params.StakingAmount); err != nil {
			return err
		}

		gentxLog.Info("gentx generated", "address", address)
		return writeMarker(g.homeDir, gentxMarkerFile)
	})
}

// addValidatorKey creates a local key and returns its bech32 address.
// Same path as: seid keys add validator --keyring-backend test
//
// Genesis ceremonies use throwaway keys; the production-keyring backend
// configured via SEI_KEYRING_BACKEND is intentionally not consumed here.
// Mixing the two would let a gentx run mutate an operator's production
// keyring (or surface its passphrase prompt) — both unacceptable.
func (g *GentxGenerator) addValidatorKey(cdc codec.Codec) (string, error) {
	gentxLog.Info("creating validator key")

	kb, err := keyring.New(sdk.KeyringServiceName(), keyring.BackendTest, g.homeDir, os.Stdin)
	if err != nil {
		return "", fmt.Errorf("generate-gentx: creating keyring: %w", err)
	}

	info, _, err := kb.NewMnemonic(
		validatorKeyName,
		keyring.English,
		sdk.GetConfig().GetFullBIP44Path(),
		"",
		hd.Secp256k1,
	)
	if err != nil {
		return "", fmt.Errorf("generate-gentx: keys add: %w", err)
	}

	addr := info.GetAddress().String()
	gentxLog.Info("validator key created", "address", addr)
	return addr, nil
}

// addGenesisAccount adds the validator's account and balance to genesis.
// Same path as: seid add-genesis-account <addr> <balance>
func (g *GentxGenerator) addGenesisAccount(cdc codec.Codec, address, balance string) error {
	gentxLog.Info("adding genesis account", "address", address, "balance", balance)

	addr, err := sdk.AccAddressFromBech32(address)
	if err != nil {
		return fmt.Errorf("generate-gentx: parsing address: %w", err)
	}

	coins, err := sdk.ParseCoinsNormalized(balance)
	if err != nil {
		return fmt.Errorf("generate-gentx: parsing balance: %w", err)
	}

	genFile := filepath.Join(g.homeDir, "config", "genesis.json")
	appState, genDoc, err := genutiltypes.GenesisStateFromGenFile(genFile)
	if err != nil {
		return fmt.Errorf("generate-gentx: reading genesis: %w", err)
	}

	// auth module: add account (same as seid add-genesis-account)
	authGenState := authtypes.GetGenesisStateFromAppState(cdc, appState)
	accs, err := authtypes.UnpackAccounts(authGenState.Accounts)
	if err != nil {
		return fmt.Errorf("generate-gentx: unpacking accounts: %w", err)
	}

	if accs.Contains(addr) {
		return fmt.Errorf("generate-gentx: account already exists at %s", addr)
	}

	accs = append(accs, authtypes.NewBaseAccount(addr, nil, 0, 0))
	accs = authtypes.SanitizeGenesisAccounts(accs)

	genAccs, err := authtypes.PackAccounts(accs)
	if err != nil {
		return fmt.Errorf("generate-gentx: packing accounts: %w", err)
	}
	authGenState.Accounts = genAccs

	authStateBz, err := cdc.MarshalAsJSON(&authGenState)
	if err != nil {
		return fmt.Errorf("generate-gentx: marshaling auth state: %w", err)
	}
	appState[authtypes.ModuleName] = authStateBz

	// bank module: add balance (same as seid add-genesis-account)
	bankGenState := banktypes.GetGenesisStateFromAppState(cdc, appState)
	bankGenState.Balances = append(bankGenState.Balances, banktypes.Balance{
		Address: addr.String(),
		Coins:   coins.Sort(),
	})
	bankGenState.Balances = banktypes.SanitizeGenesisBalances(bankGenState.Balances)

	bankStateBz, err := cdc.MarshalAsJSON(bankGenState)
	if err != nil {
		return fmt.Errorf("generate-gentx: marshaling bank state: %w", err)
	}
	appState[banktypes.ModuleName] = bankStateBz

	// evm module: associate sei address with eth address.
	// Same as seid add-genesis-account when --keyring-backend=test.
	if err := g.addEVMAddressAssociation(cdc, appState, addr); err != nil {
		return fmt.Errorf("generate-gentx: adding EVM association: %w", err)
	}

	appStateJSON, err := json.Marshal(appState)
	if err != nil {
		return fmt.Errorf("generate-gentx: marshaling app state: %w", err)
	}
	genDoc.AppState = appStateJSON

	return genutil.ExportGenesisFile(genDoc, genFile)
}

// generateGentx builds and signs a MsgCreateValidator.
// Same path as: seid gentx validator <amount> --chain-id <chain>
func (g *GentxGenerator) generateGentx(cdc codec.Codec, txCfg client.TxConfig, chainID, stakingAmount string) error {
	gentxLog.Info("generating gentx", "chainId", chainID, "stakingAmount", stakingAmount)

	cfg := tmcfg.DefaultConfig()
	cfg.SetRoot(g.homeDir)

	nodeID, valPubKey, err := genutil.InitializeNodeValidatorFiles(cfg)
	if err != nil {
		return fmt.Errorf("generate-gentx: loading validator files: %w", err)
	}

	kb, err := keyring.New(sdk.KeyringServiceName(), keyring.BackendTest, g.homeDir, os.Stdin)
	if err != nil {
		return fmt.Errorf("generate-gentx: opening keyring: %w", err)
	}

	keyInfo, err := kb.Key(validatorKeyName)
	if err != nil {
		return fmt.Errorf("generate-gentx: looking up key: %w", err)
	}

	// Read and validate genesis
	genDoc, err := tmtypes.GenesisDocFromFile(cfg.GenesisFile())
	if err != nil {
		return fmt.Errorf("generate-gentx: reading genesis: %w", err)
	}

	var genesisState map[string]json.RawMessage
	if err := json.Unmarshal(genDoc.AppState, &genesisState); err != nil {
		return fmt.Errorf("generate-gentx: parsing app_state: %w", err)
	}

	// Validate account has sufficient balance (same check seid gentx does)
	coins, err := sdk.ParseCoinsNormalized(stakingAmount)
	if err != nil {
		return fmt.Errorf("generate-gentx: parsing staking amount: %w", err)
	}

	genBalIterator := banktypes.GenesisBalancesIterator{}
	if err := genutil.ValidateAccountInGenesis(
		genesisState, genBalIterator, keyInfo.GetAddress(), coins, cdc,
	); err != nil {
		return fmt.Errorf("generate-gentx: %w", err)
	}

	// Resolve the pod's IP for the gentx memo (nodeID@ip:port).
	// collect-gentxs requires a non-empty memo for peer discovery.
	// The memo IP is vestigial — the controller overwrites persistent
	// peers with DNS-based addresses — but it must be non-empty.
	// Same call as seid gentx: server.ExternalIP().
	ip, ipErr := server.ExternalIP()
	if ipErr != nil {
		gentxLog.Warn("ExternalIP resolution failed, memo will contain empty IP", "error", ipErr)
	}

	// Build MsgCreateValidator (same struct seid gentx populates)
	createValCfg := stakingcli.TxCreateValidatorConfig{
		ChainID:                 chainID,
		NodeID:                  nodeID,
		Moniker:                 cfg.Moniker,
		Amount:                  stakingAmount,
		CommissionRate:          "0.1",
		CommissionMaxRate:       "0.2",
		CommissionMaxChangeRate: "0.01",
		MinSelfDelegation:       "1",
		PubKey:                  valPubKey,
		IP:                      ip,
		P2PPort:                 "26656",
	}

	clientCtx := client.Context{}.
		WithKeyring(kb).
		WithCodec(cdc).
		WithTxConfig(txCfg).
		WithFromAddress(keyInfo.GetAddress())

	txFactory := tx.Factory{}.
		WithChainID(chainID).
		WithKeybase(kb).
		WithTxConfig(txCfg)

	txBldr, msg, err := stakingcli.BuildCreateValidatorMsg(clientCtx, createValCfg, txFactory, true)
	if err != nil {
		return fmt.Errorf("generate-gentx: building MsgCreateValidator: %w", err)
	}

	if err := msg.ValidateBasic(); err != nil {
		return fmt.Errorf("generate-gentx: invalid MsgCreateValidator: %w", err)
	}

	// Round-trip through the codec — same flow as seid gentx
	w := &bytes.Buffer{}
	clientCtx = clientCtx.WithOutput(w)

	if err := authclient.PrintUnsignedStdTx(txBldr, clientCtx, []sdk.Msg{msg}); err != nil {
		return fmt.Errorf("generate-gentx: generating unsigned tx: %w", err)
	}

	stdTx, err := txCfg.TxJSONDecoder()(w.Bytes())
	if err != nil {
		return fmt.Errorf("generate-gentx: decoding unsigned tx: %w", err)
	}

	txBuilder, err := txCfg.WrapTxBuilder(stdTx)
	if err != nil {
		return fmt.Errorf("generate-gentx: wrapping tx builder: %w", err)
	}

	if err := authclient.SignTx(txFactory, clientCtx, validatorKeyName, txBuilder, true, true); err != nil {
		return fmt.Errorf("generate-gentx: signing: %w", err)
	}

	signedJSON, err := txCfg.TxJSONEncoder()(txBuilder.GetTx())
	if err != nil {
		return fmt.Errorf("generate-gentx: encoding signed tx: %w", err)
	}

	// Write gentx file
	gentxDir := filepath.Join(g.homeDir, "config", "gentx")
	if err := os.MkdirAll(gentxDir, 0o700); err != nil {
		return fmt.Errorf("generate-gentx: creating gentx dir: %w", err)
	}

	gentxFile := filepath.Join(gentxDir, fmt.Sprintf("gentx-%s.json", nodeID))
	return os.WriteFile(gentxFile, append(signedJSON, '\n'), 0o600)
}

// addEVMAddressAssociation derives the Ethereum address from the
// validator's secp256k1 private key and writes it into the EVM module's
// genesis state. This mirrors seid add-genesis-account lines 136-148.
//
// We avoid importing x/evm (which transitively pulls in wasmvm/duckdb
// and breaks CGO_ENABLED=0) by operating on the genesis JSON directly.
// The result is identical: an AddressAssociation entry is appended.
func (g *GentxGenerator) addEVMAddressAssociation(cdc codec.Codec, appState map[string]json.RawMessage, addr sdk.AccAddress) error {
	evmRaw, ok := appState["evm"]
	if !ok {
		// No evm module in genesis — skip silently.
		return nil
	}

	kb, err := keyring.New(sdk.KeyringServiceName(), keyring.BackendTest, g.homeDir, nil)
	if err != nil {
		return err
	}

	pk, err := getPrivateKeyOfAddr(kb, addr)
	if err != nil {
		return fmt.Errorf("deriving ETH address for EVM association: %w", err)
	}

	ethAddr := ethcrypto.PubkeyToAddress(pk.PublicKey)

	// Unmarshal the evm genesis state as generic JSON, append the
	// association, and marshal back. This avoids importing x/evm/types
	// which pulls in wasmvm via its transitive dependency chain.
	var evmState map[string]json.RawMessage
	if err := json.Unmarshal(evmRaw, &evmState); err != nil {
		return fmt.Errorf("parsing evm genesis state: %w", err)
	}

	var associations []json.RawMessage
	if raw, ok := evmState["address_associations"]; ok && string(raw) != "null" {
		if err := json.Unmarshal(raw, &associations); err != nil {
			return fmt.Errorf("parsing evm address_associations: %w", err)
		}
	}

	entry, _ := json.Marshal(map[string]string{
		"sei_address": addr.String(),
		"eth_address": ethAddr.Hex(),
	})
	associations = append(associations, entry)

	evmState["address_associations"], _ = json.Marshal(associations)
	appState["evm"], _ = json.Marshal(evmState)
	return nil
}

// getPrivateKeyOfAddr extracts the secp256k1 private key for the given
// address from the test keyring. Mirrors seid's getPrivateKeyOfAddr
// in cmd/seid/cmd/genaccounts.go:212-238.
func getPrivateKeyOfAddr(kb keyring.Keyring, addr sdk.Address) (*ecdsa.PrivateKey, error) {
	keys, err := kb.List()
	if err != nil {
		return nil, err
	}
	for _, key := range keys {
		localInfo, ok := key.(keyring.LocalInfo)
		if !ok {
			continue
		}
		if localInfo.GetAddress().Equals(addr) {
			priv, err := legacy.PrivKeyFromBytes([]byte(localInfo.PrivKeyArmor))
			if err != nil {
				return nil, err
			}
			privKey, err := ethcrypto.HexToECDSA(hex.EncodeToString(priv.Bytes()))
			if err != nil {
				return nil, err
			}
			return privKey, nil
		}
	}
	return nil, fmt.Errorf("key not found for address %s", addr)
}

// makeCodec builds a proto codec with the minimum interface types
// registered for genesis ceremony operations. vestingtypes is required to
// marshal/unmarshal a ContinuousVestingAccount or DelayedVestingAccount —
// addExternalGenesisAccounts can produce either when a GenesisAccountEntry
// sets Vesting, so this codec must resolve their type URLs even though no
// gentx itself is ever a vesting account.
func makeCodec() (codec.Codec, client.TxConfig) {
	registry := codectypes.NewInterfaceRegistry()
	cryptocodec.RegisterInterfaces(registry)
	authtypes.RegisterInterfaces(registry)
	vestingtypes.RegisterInterfaces(registry)
	banktypes.RegisterInterfaces(registry)
	stakingtypes.RegisterInterfaces(registry)
	cdc := codec.NewProtoCodec(registry)

	txConfig := authtx.NewTxConfig(cdc, authtx.DefaultSignModes)
	return cdc, txConfig
}

// ensureBech32 sets the Sei bech32 address prefixes if not already set.
func ensureBech32() {
	cfg := sdk.GetConfig()
	if cfg.GetBech32AccountAddrPrefix() != "sei" {
		cfg.SetBech32PrefixForAccount("sei", "seipub")
		cfg.SetBech32PrefixForValidator("seivaloper", "seivaloperpub")
		cfg.SetBech32PrefixForConsensusNode("seivalcons", "seivalconspub")
	}
}
