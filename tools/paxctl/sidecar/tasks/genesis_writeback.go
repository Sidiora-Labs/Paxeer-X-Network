package tasks

import (
	"encoding/json"
	"fmt"

	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"

	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/genutil"
)

// writeBackAuthAndBank serializes mutated auth + bank state and exports
// the genesis file. Callers wrap the returned error with task context.
func writeBackAuthAndBank(
	cdc codec.Codec,
	genFile string,
	genDoc *tmtypes.GenesisDoc,
	appState map[string]json.RawMessage,
	authGenState authtypes.GenesisState,
	accs authtypes.GenesisAccounts,
	bankGenState *banktypes.GenesisState,
) error {
	accs = authtypes.SanitizeGenesisAccounts(accs)
	genAccs, err := authtypes.PackAccounts(accs)
	if err != nil {
		return fmt.Errorf("packing accounts: %w", err)
	}
	authGenState.Accounts = genAccs
	authStateBz, err := cdc.MarshalAsJSON(&authGenState)
	if err != nil {
		return fmt.Errorf("marshaling auth state: %w", err)
	}
	appState[authtypes.ModuleName] = authStateBz

	bankGenState.Balances = banktypes.SanitizeGenesisBalances(bankGenState.Balances)
	bankStateBz, err := cdc.MarshalAsJSON(bankGenState)
	if err != nil {
		return fmt.Errorf("marshaling bank state: %w", err)
	}
	appState[banktypes.ModuleName] = bankStateBz

	appStateJSON, err := json.Marshal(appState)
	if err != nil {
		return fmt.Errorf("marshaling app state: %w", err)
	}
	genDoc.AppState = appStateJSON

	if err := genutil.ExportGenesisFile(genDoc, genFile); err != nil {
		return fmt.Errorf("writing genesis: %w", err)
	}
	return nil
}
