package tasks

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	cryptocodec "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/codec"
	cryptotypes "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/genutil"
	genutiltypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/genutil/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
)

// populateGenesisValidators derives the initial CometBFT validator set from the
// collected gentxs and writes it into the top-level genDoc.Validators, making
// the assembled genesis self-contained: its validator set is loadable without
// InitChain (e.g. by a state-sync boot), matching canonical chains. A
// collect-gentxs genesis otherwise leaves genDoc.Validators empty and
// materializes the set only at InitChain (height 0).
//
// The derivation mirrors staking's InitChain math exactly, so a normal
// from-genesis boot still passes CometBFT's genesis-vs-InitChain equality
// assertion: consensus power = TokensToConsensusPower(self-bond,
// DefaultPowerReduction) and the pubkey is the gentx's consensus pubkey.
//
// It guards only the divergences that are a pure function of a well-formed
// gentx set — where a per-gentx mapping would silently disagree with the set
// InitChain actually bonds — turning each into a loud, assembly-time failure
// rather than a genesis that hard-errors every founding validator at boot:
//   - a validator whose stake yields consensus power 0 (InitChain drops it),
//   - more validators than the staking MaxValidators cap (InitChain bonds only
//     the top N by power), and
//   - two gentxs sharing a consensus key (CometBFT's NewValidatorSet panics on a
//     duplicate entry).
//
// It does NOT re-validate gentx admissibility — commission below the params
// minimum, a duplicate operator/owner, a self-bond in the wrong denom, an
// unsupported consensus pubkey type. Those are rejected upstream by genutil
// ValidateGenesis and, failing that, by DeliverGenTxs panicking during
// InitChain; this derivation trusts that gate rather than reimplementing the
// staking handler/ante checks.
//
// Runs after collectGentxs/applyOverrides and before uploadGenesis, so the
// validators are part of the bytes that get hashed and distributed. Deriving
// from the assembled genesis's genutil.gen_txs (not the raw gentx files) uses
// the exact input InitChain consumes, so any override that rewrote gen_txs is
// reflected here too.
func (a *GenesisAssembler) populateGenesisValidators() error {
	cdc, txCfg := makeCodec()
	ensureBech32()

	genFile := filepath.Join(a.homeDir, "config", "genesis.json")
	genDoc, err := tmtypes.GenesisDocFromFile(genFile)
	if err != nil {
		return fmt.Errorf("assemble-genesis: reading genesis for validators: %w", err)
	}

	var appState map[string]json.RawMessage
	if err := json.Unmarshal(genDoc.AppState, &appState); err != nil {
		return fmt.Errorf("assemble-genesis: parsing app_state for validators: %w", err)
	}

	maxValidators := stakingtypes.GetGenesisStateFromAppState(cdc, appState).Params.MaxValidators
	genUtilState := genutiltypes.GetGenesisStateFromAppState(cdc, appState)

	validators, err := deriveGenesisValidators(cdc, txCfg, genUtilState.GenTxs)
	if err != nil {
		return err
	}
	if len(validators) == 0 {
		return fmt.Errorf("assemble-genesis: no gentxs to derive genesis validators from")
	}
	if uint32(len(validators)) > maxValidators {
		return fmt.Errorf(
			"assemble-genesis: %d validators exceed staking max_validators %d; InitChain would bond "+
				"only the top %d by power, so genDoc.validators would diverge from the app's set and "+
				"every validator would fail the boot-time genesis/InitChain equality check",
			len(validators), maxValidators, maxValidators,
		)
	}

	genDoc.Validators = validators
	if err := genutil.ExportGenesisFile(genDoc, genFile); err != nil {
		return fmt.Errorf("assemble-genesis: writing genesis with validators: %w", err)
	}

	assembleLog.Info("populated genesis validators", "count", len(validators), "maxValidators", maxValidators)
	return nil
}

// deriveGenesisValidators converts each collected gentx's MsgCreateValidator
// into a GenesisValidator using the same consensus-power conversion InitChain
// applies. It fails loud on the zero-power edge and on a duplicate consensus key
// so the assembled set can never silently diverge from what InitChain would bond
// nor panic CometBFT's NewValidatorSet at boot.
func deriveGenesisValidators(cdc codec.Codec, txCfg client.TxConfig, genTxs []json.RawMessage) ([]tmtypes.GenesisValidator, error) {
	validators := make([]tmtypes.GenesisValidator, 0, len(genTxs))
	seenConsAddr := make(map[string]string, len(genTxs))
	for i, raw := range genTxs {
		tx, err := txCfg.TxJSONDecoder()(raw)
		if err != nil {
			return nil, fmt.Errorf("assemble-genesis: decoding gen_tx %d: %w", i, err)
		}
		msgs := tx.GetMsgs()
		if len(msgs) != 1 {
			return nil, fmt.Errorf("assemble-genesis: gen_tx %d has %d messages, want exactly 1 MsgCreateValidator", i, len(msgs))
		}
		msg, ok := msgs[0].(*stakingtypes.MsgCreateValidator)
		if !ok {
			return nil, fmt.Errorf("assemble-genesis: gen_tx %d is not a MsgCreateValidator", i)
		}

		var pk cryptotypes.PubKey
		if err := cdc.UnpackAny(msg.Pubkey, &pk); err != nil {
			return nil, fmt.Errorf("assemble-genesis: unpacking consensus pubkey for validator %s: %w", msg.ValidatorAddress, err)
		}
		tmPk, err := cryptocodec.ToTmPubKeyInterface(pk)
		if err != nil {
			return nil, fmt.Errorf("assemble-genesis: converting consensus pubkey for validator %s: %w", msg.ValidatorAddress, err)
		}

		consAddr := tmPk.Address().String()
		if prev, dup := seenConsAddr[consAddr]; dup {
			return nil, fmt.Errorf(
				"assemble-genesis: validators %s and %s share consensus key %s; CometBFT's "+
					"NewValidatorSet panics on a duplicate entry, bricking every founding validator at boot",
				prev, msg.ValidatorAddress, consAddr,
			)
		}
		seenConsAddr[consAddr] = msg.ValidatorAddress

		// Same conversion as staking InitChain: k.PowerReduction(ctx) returns
		// sdk.DefaultPowerReduction, and a genesis validator's bonded tokens are
		// its self-delegation (msg.Value). sei-cosmos x/staking/keeper/params.go:58-60
		// returns DefaultPowerReduction unconditionally — it is not a gov param — so
		// hardcoding the constant is safe. If it ever becomes parameterized, this
		// power stops matching InitChain and every validator fails the boot-time
		// genesis/InitChain equality check (replay.go:211-228).
		power := sdk.TokensToConsensusPower(msg.Value.Amount, sdk.DefaultPowerReduction)
		if power == 0 {
			return nil, fmt.Errorf(
				"assemble-genesis: validator %s self-bond %s yields consensus power 0 (below power "+
					"reduction %s); InitChain would drop it, so genDoc.validators would diverge from "+
					"the app's set",
				msg.ValidatorAddress, msg.Value.String(), sdk.DefaultPowerReduction,
			)
		}

		validators = append(validators, tmtypes.GenesisValidator{
			Address: tmPk.Address(),
			PubKey:  tmPk,
			Power:   power,
		})
	}
	return validators, nil
}
