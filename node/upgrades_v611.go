package app

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	layerxbridgetypes "github.com/sidiora-labs/paxeer-network/modules/layerxbridge/types"
	tokenfactorykeeper "github.com/sidiora-labs/paxeer-network/modules/tokenfactory/keeper"
	tokenfactorytypes "github.com/sidiora-labs/paxeer-network/modules/tokenfactory/types"
	sdk "github.com/sidiora-labs/paxeer-network/sdk/types"
	"github.com/sidiora-labs/paxeer-network/sdk/types/module"
	upgradetypes "github.com/sidiora-labs/paxeer-network/sdk/x/upgrade/types"
)

// sidHoldersJSON is the SID holder snapshot the v6.11 plan migrates.
//
//go:embed testdata/sid-holders.json
var sidHoldersJSON []byte

// SidioraProxyAddress is the SID ERC-20 proxy whose legacy balances the v6.11
// plan moves into the usid bank denom.
var SidioraProxyAddress = common.HexToAddress("0x21f7b20a555199fa73A238B1a91FD0f549068fEe")

// sidLegacyERC20Location is the ERC-7201 location of the OpenZeppelin
// ERC20Upgradeable storage (openzeppelin.storage.ERC20): _balances at +0,
// _allowances at +1 and _totalSupply at +2.
var sidLegacyERC20Location = erc7201Location("openzeppelin.storage.ERC20")

// sidLegacyMigrationLocation is the ERC-7201 location, in the SID proxy's
// storage, of the record the v6.11 plan leaves: the height it migrated at in
// +0 and the legacy supply it could not attribute to any listed holder in +1,
// which the contract's migrateLegacy path may claim later.
var sidLegacyMigrationLocation = erc7201Location("paxeer.storage.SidioraLegacyMigration")

type sidHolder struct {
	Address string `json:"address"`
	Balance string `json:"balance"`
}

// erc7201Location returns keccak256(abi.encode(uint256(keccak256(id)) - 1)) & ~0xff.
func erc7201Location(id string) common.Hash {
	inner := new(big.Int).SetBytes(crypto.Keccak256([]byte(id)))
	inner.Sub(inner, big.NewInt(1))
	location := crypto.Keccak256Hash(common.BigToHash(inner).Bytes())
	location[31] = 0
	return location
}

func slotAt(base common.Hash, offset int64) common.Hash {
	return common.BigToHash(new(big.Int).Add(base.Big(), big.NewInt(offset)))
}

// sidLegacyBalanceSlot is the slot of _balances[holder] in the legacy layout.
func sidLegacyBalanceSlot(holder common.Address) common.Hash {
	return crypto.Keccak256Hash(common.LeftPadBytes(holder.Bytes(), 32), sidLegacyERC20Location.Bytes())
}

func sidLegacyTotalSupplySlot() common.Hash { return slotAt(sidLegacyERC20Location, 2) }

func sidMigratedHeightSlot() common.Hash { return slotAt(sidLegacyMigrationLocation, 0) }

func sidShortfallSlot() common.Hash { return slotAt(sidLegacyMigrationLocation, 1) }

func parseSIDHolders(raw []byte) ([]common.Address, []sdk.Int, error) {
	var entries []sidHolder
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, nil, fmt.Errorf("SID holder list: %w", err)
	}
	seen := make(map[common.Address]bool, len(entries))
	addresses := make([]common.Address, 0, len(entries))
	balances := make([]sdk.Int, 0, len(entries))
	for _, entry := range entries {
		if !common.IsHexAddress(entry.Address) {
			return nil, nil, fmt.Errorf("SID holder list: invalid address %q", entry.Address)
		}
		address := common.HexToAddress(entry.Address)
		if seen[address] {
			return nil, nil, fmt.Errorf("SID holder list: duplicate address %s", address.Hex())
		}
		seen[address] = true
		balance, ok := sdk.NewIntFromString(entry.Balance)
		if !ok || balance.IsNegative() {
			return nil, nil, fmt.Errorf("SID holder list: invalid balance %q for %s", entry.Balance, address.Hex())
		}
		addresses = append(addresses, address)
		balances = append(balances, balance)
	}
	return addresses, balances, nil
}

// runV611Upgrade runs the module migrations, moves the usid mint authority to
// the bridge module account and migrates the legacy SID ERC-20 balances of the
// embedded holder list into usid.
func (app *App) runV611Upgrade(ctx sdk.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
	newVM, err := app.mm.RunMigrations(ctx, app.configurator, fromVM)
	if err != nil {
		return newVM, err
	}
	if err := app.moveSidioraMintAuthority(ctx); err != nil {
		return newVM, fmt.Errorf("upgrade %s: %w", V611Upgrade, err)
	}
	if err := app.migrateSIDHolders(ctx, sidHoldersJSON); err != nil {
		return newVM, fmt.Errorf("upgrade %s: %w", V611Upgrade, err)
	}
	return newVM, nil
}

// moveSidioraMintAuthority makes the bridge module account the only admin of
// the usid denom, creating the denom under the bridge module when the chain
// has none, so no other account keeps a usid mint right.
func (app *App) moveSidioraMintAuthority(ctx sdk.Context) error {
	denom := layerxbridgetypes.SidioraDenom()
	bridge := layerxbridgetypes.ModuleAddress().String()
	admin, err := app.TokenFactoryKeeper.GetAuthorityMetadata(ctx, denom)
	if err != nil {
		return err
	}
	if admin.Admin == "" {
		if _, err := app.TokenFactoryKeeper.CreateDenom(ctx, bridge, layerxbridgetypes.SidioraSubdenom); err != nil {
			return fmt.Errorf("create %s: %w", denom, err)
		}
		return nil
	}
	if admin.Admin == bridge {
		return nil
	}
	_, err = tokenfactorykeeper.NewMsgServerImpl(app.TokenFactoryKeeper).ChangeAdmin(sdk.WrapSDKContext(ctx),
		&tokenfactorytypes.MsgChangeAdmin{Sender: admin.Admin, Denom: denom, NewAdmin: bridge})
	return err
}

// migrateSIDHolders mints, for each listed holder, exactly the legacy balance
// its slot in the SID proxy holds to the holder's account and zeroes that slot.
// The slot value wins over the listed one. The legacy total supply the listed
// holders do not cover is recorded as the claimable shortfall and never minted.
// A chain that already migrated is left untouched.
func (app *App) migrateSIDHolders(ctx sdk.Context, raw []byte) error {
	if app.EvmKeeper.GetState(ctx, SidioraProxyAddress, sidMigratedHeightSlot()) != (common.Hash{}) {
		return nil
	}
	holders, listed, err := parseSIDHolders(raw)
	if err != nil {
		return err
	}
	denom := layerxbridgetypes.SidioraDenom()
	minted := sdk.ZeroInt()
	for i, holder := range holders {
		slot := sidLegacyBalanceSlot(holder)
		balance := sdk.NewIntFromBigInt(app.EvmKeeper.GetState(ctx, SidioraProxyAddress, slot).Big())
		if !balance.Equal(listed[i]) {
			logger.Info("SID holder balance differs from the snapshot; using the on-chain slot",
				"holder", holder.Hex(), "listed", listed[i].String(), "onchain", balance.String())
		}
		if balance.IsZero() {
			continue
		}
		coins := sdk.NewCoins(sdk.NewCoin(denom, balance))
		if err := app.BankKeeper.MintCoins(ctx, tokenfactorytypes.ModuleName, coins); err != nil {
			return fmt.Errorf("mint %s for %s: %w", coins, holder.Hex(), err)
		}
		recipient := app.EvmKeeper.GetPaxAddressOrDefault(ctx, holder)
		if err := app.BankKeeper.SendCoinsFromModuleToAccount(ctx, tokenfactorytypes.ModuleName, recipient, coins); err != nil {
			return fmt.Errorf("send %s to %s: %w", coins, holder.Hex(), err)
		}
		app.EvmKeeper.SetState(ctx, SidioraProxyAddress, slot, common.Hash{})
		minted = minted.Add(balance)
	}
	supply := sdk.NewIntFromBigInt(app.EvmKeeper.GetState(ctx, SidioraProxyAddress, sidLegacyTotalSupplySlot()).Big())
	if minted.GT(supply) {
		return fmt.Errorf("minted %s usid over the legacy SID total supply %s", minted, supply)
	}
	shortfall := supply.Sub(minted)
	if shortfall.IsPositive() {
		logger.Info("SID legacy supply not covered by the holder snapshot; recorded as claimable",
			"minted", minted.String(), "supply", supply.String(), "shortfall", shortfall.String())
		app.EvmKeeper.SetState(ctx, SidioraProxyAddress, sidShortfallSlot(), common.BigToHash(shortfall.BigInt()))
	}
	app.EvmKeeper.SetState(ctx, SidioraProxyAddress, sidMigratedHeightSlot(), common.BigToHash(big.NewInt(ctx.BlockHeight())))
	return nil
}
