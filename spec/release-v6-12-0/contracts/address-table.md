# Release v6.12.0 address table

Addresses, module accounts and tokens of release v6.12.0. Code citations are against the tree the release starts from; constructors under a package that does not exist there are added by the feature that owns the contract file.

## Precompiles

| Address | Name | Constructor | Module | Activation rule | Contract file |
|---|---|---|---|---|---|
| 0x0000000000000000000000000000000000001015 | Perpetuals (ExchangeAddress, modules/layerxexchange/types/ids.go:15) | precompiles/perps NewPrecompile from the v6.12.0 done height; layerxexchangev69.NewPrecompile in precompiles/layerxexchange/legacy/v69, the copy of precompiles/layerxexchange/layerxexchange.go:140, below it | layerxexchange activation entry (node/upgrades.go:496); keeper modules/perps | Keeps its v6.9 activation entry; the version switches at the v6.12.0 done height in the shape of precompiles/p256/setup.go:15-24, and historical heights resolve through GetCustomPrecompilesVersions (modules/evm/keeper/keeper.go:356-385) | perps.md |
| 0x0000000000000000000000000000000000001017 | Launchpad (modules/launchpad/types/keys.go:26) | precompiles/launchpad NewPrecompile (precompiles/launchpad/launchpad.go:180); the current precompile under v6.10 in precompiles/launchpad/legacy/v610 | launchpad (node/upgrades.go:498) | Stays behind activationPrecompileGate (node/upgrades.go:498); the v6.12.0 hook MigrateV6120 serves both deployment generations through it | launchpad.md |
| 0x0000000000000000000000000000000000001020 | PriceFeed | precompiles/pricefeed NewPrecompile | pricefeed | v6.12.0 activation entry; no code below the done height | pricefeed.md |
| 0x0000000000000000000000000000000000001021 | PECOR (market orders, swaps, quotes) | entry of precompiles/spot NewPrecompileSet | spot | v6.12.0 activation entry; no code below the done height | spot.md |
| 0x0000000000000000000000000000000000001022 | PECOROrders (limit orders) | entry of precompiles/spot NewPrecompileSet | spot | v6.12.0 activation entry; no code below the done height | spot.md |
| 0x0000000000000000000000000000000000001023 | PECORStopOrders (stop-limit and stop-loss) | entry of precompiles/spot NewPrecompileSet | spot | v6.12.0 activation entry; no code below the done height | spot.md |
| 0x0000000000000000000000000000000000001024 | PECORQuoter | entry of precompiles/spot NewPrecompileSet | spot | v6.12.0 activation entry; no code below the done height | spot.md |
| 0x0000000000000000000000000000000000001025 | PECORVault | entry of precompiles/spot NewPrecompileSet | spot | v6.12.0 activation entry; no code below the done height | spot.md |
| 0x0000000000000000000000000000000000001026 | PECORVaultShare | none: each vault share is a bank denom with its own ERC20 native pointer | spot | Under the v6.12.0 activation entry and carries no code | spot.md |
| 0x0000000000000000000000000000000000001027 | PECORRouter | none | spot | Under the v6.12.0 activation entry and carries no code | spot.md |

Every new address is gated in the shape of activationPrecompileGate (node/upgrades.go:489-503) through SetCustomPrecompileActivation (modules/evm/keeper/keeper.go:207-241). The giga executor fails fast on a custom precompile that is not active (engine/executor/precompiles/failfast.go:116-117).

## Module accounts

| Account | Module | Holds | Contract file |
|---|---|---|---|
| perps | perps | Every uusdl of the perpetuals venue, including the bank coins behind perps_fees and perps_insurance | perps.md |
| perps_fees | perps | Fee recipient; its balance is a perps account in the perps store, its bank coins sit in the perps module account | perps.md |
| perps_insurance | perps | Insurance fund; its balance is a perps account in the perps store, its bank coins sit in the perps module account | perps.md, perps-parameters.md |
| spot | spot | Every spot denom; book reserves, escrows, vault pools and fee accruals are keeper records over that one balance per denom | spot.md |
| launchpad | launchpad | authtypes.NewModuleAddress(ModuleName) (modules/launchpad/keeper/keeper.go:40, modules/launchpad/types/keys.go:10); gains Minter permission to mint uusdl in step 1 of MigrateV6120; keeps no escrow for pool reserves, which sit in the bank accounts of the pool addresses | launchpad.md |
| launchpad_treasury | launchpad | Existing treasury account (modules/launchpad/types/keys.go:14), unchanged | launchpad.md |

Spot fees have no account of their own: they are accruals inside the spot module account.

## Tokens

| Denom | Decimals | Pointer | Use | Contract file |
|---|---|---|---|---|
| uusdl | 6 | 0x85FcD13735F4309833A503EE804ea32395851479, the existing USDL address (contracts/libraries/Constants.sol:14-16), installed by step 1 of MigrateV6120 | Launchpad quote denom (modules/launchpad/types/params.go:31) and the only perpetuals collateral | launchpad.md, perps.md, spot.md |
| uhpx | from bank metadata | ERC20 native pointer of the base denom | Base denom (modules/evm/keeper/params.go:22), PAX, pricefeed market 11 | spot.md, pricefeed.md |
| usid | from bank metadata | ERC20 native pointer of usid | SID, pricefeed market 12; holds the legacy SID balances of the SID proxy (node/upgrades_v611.go:24-26) | spot.md, pricefeed.md |

Pointer rule: every token address is read with GetERC20NativePointer (precompiles/utils/expected_keepers.go:127) and resolved back to its denom with GetNativePointee (modules/evm/keeper/pointer.go:444); decimals come from the bank metadata of the denom, never from a constant in the reader.
