# Release v6.12.0 address table

Addresses, module accounts and tokens of release v6.12.0. Code citations are against the tree the release starts from; constructors under a package that does not exist there are added by the feature that owns the contract file. The plan constant is V6120Upgrade = "v6.12.0" of node/upgrades_v6120.go (baseline task 1.0.3).

## Precompiles

| Address | Name | Constructor | Module | Activation rule | Contract file |
|---|---|---|---|---|---|
| 0x0000000000000000000000000000000000001015 | Perpetuals (ExchangeAddress, modules/layerxexchange/types/ids.go:15) | exchangeVersionSwitch(perps, legacy, live) of baseline task 1.2.1: precompiles/perps NewPrecompile as the new value; the unchanged v6.9 exchange contract of precompiles/layerxexchange NewPrecompile (precompiles/layerxexchange/layerxexchange.go:140) as the legacy value | layerxexchange activation entry (node/upgrades.go:496); keeper modules/perps | Keeps its activation entry of activationPrecompileGate (node/upgrades.go:489-503); a putils.VersionedPrecompiles in the shape of precompiles/p256/setup.go:15-24 keyed {LatestUpgrade, ActivationUpgrade, V6120Upgrade}, selected by the App field v6120Live that the v6.12.0 handler sets; historical heights resolve through GetCustomPrecompilesVersions (modules/evm/keeper/keeper.go:356-385): legacy below the v6.12.0 done height, new at and above it | perps.md |
| 0x0000000000000000000000000000000000001017 | Launchpad (modules/launchpad/types/keys.go:26) | launchpadVersionSwitch(g2, g1, live) of baseline task 1.2.1: the generation 2 precompile of precompiles/launchpad NewPrecompile as the new value; the current precompile (precompiles/launchpad/launchpad.go:180) kept under precompiles/launchpad/legacy/v610 as the legacy value | launchpad (node/upgrades.go:498) | Keeps its activation entry (node/upgrades.go:498); the same versioned map and live switch as 0x1015 | launchpad.md |
| 0x0000000000000000000000000000000000001020 | PriceFeed | precompiles/pricefeed NewPrecompile | pricefeed | v6120PrecompileGate (Upgrade V6120Upgrade) of baseline task 1.2.1 through SetCustomPrecompileActivation (modules/evm/keeper/keeper.go:207-241); no code below the done height | pricefeed.md |
| 0x0000000000000000000000000000000000001021 | PECOR (market orders, swaps, quotes) | entry of precompiles/spot NewPrecompileSet | spot | v6120PrecompileGate; no code below the done height | spot.md |
| 0x0000000000000000000000000000000000001022 | PECOROrders (limit orders) | entry of precompiles/spot NewPrecompileSet | spot | v6120PrecompileGate; no code below the done height | spot.md |
| 0x0000000000000000000000000000000000001023 | PECORStopOrders (stop-limit and stop-loss) | entry of precompiles/spot NewPrecompileSet | spot | v6120PrecompileGate; no code below the done height | spot.md |
| 0x0000000000000000000000000000000000001024 | PECORQuoter | entry of precompiles/spot NewPrecompileSet | spot | v6120PrecompileGate; no code below the done height | spot.md |
| 0x0000000000000000000000000000000000001025 | PECORVault | entry of precompiles/spot NewPrecompileSet | spot | v6120PrecompileGate; no code below the done height | spot.md |
| 0x0000000000000000000000000000000000001026 | PECORVaultShare | none: each vault share is a bank denom with its own ERC20 native pointer | spot | Not in any activation entry; carries no code at any height, eth_getCode returns 0x | spot.md |
| 0x0000000000000000000000000000000000001027 | PECORRouter | none | spot | Not in any activation entry; carries no code at any height, eth_getCode returns 0x | spot.md |
| 0x000000000000000000000000000000000000100A | Pointer view (precompiles/pointerview/pointerview.go:24) | existing | evm | Unchanged; getNativePointer(string) (precompiles/pointerview/pointerview.go:18) is the read path of the pointer rule below | address-table.md |

LatestUpgrade stays the last line of node/tags (node/upgrades.go:47-55); v6.12.0 is a named plan with no node/tags entry and the handler activates the new set explicitly. The giga executor hands every address above except 0x1026, 0x1027 and 0x100A to the ordinary path through gigaV6120FailFast of baseline task 1.2.1, built from AllCustomPrecompilesFailFastLate (engine/executor/precompiles/failfast.go:128) plus 0x1020-0x1025 mapped to FailFastSingleton (engine/executor/precompiles/failfast.go:101).

## Module accounts

Every module account address is authtypes.NewModuleAddress(name); its EVM address, the module EVM address, is the address GetEVMAddressOrDefault (modules/evm/keeper/address.go:41) returns for it. Every maccPerms address is a blocked receiver unless it is in allowedReceivingModAcc (node/app.go:282-306, node/app.go:3465).

| Account | Module | maccPerms | Holds | Contract file |
|---|---|---|---|---|
| perps | perps | none | Every USDL of the perpetuals venue as the USDL ERC20 balance of the perps module EVM address, including the balances behind perps_fees and perps_insurance; moved in by transferFrom under the user's allowance to 0x1015 and out by transfer through CallEVM | perps.md |
| perps_fees | perps | none | Fee recipient; a perps account in the perps store over the perps module EVM address balance | perps.md |
| perps_insurance | perps | none | Insurance fund; a perps account in the perps store over the perps module EVM address balance | perps.md, perps-parameters.md |
| spot | spot | {Minter, Burner}, the spot keeper refusing any mint or burn outside spot/share/* denoms; in allowedReceivingModAcc | uhpx and SID as bank balances; USDL and every registered ERC20 as ERC20 balances of the spot module EVM address; book reserves, escrows, vault pools and fee accruals are keeper records over those balances | spot.md |
| launchpad | launchpad | none (the rule of node/app.go:296-299) | authtypes.NewModuleAddress(ModuleName) (modules/launchpad/keeper/keeper.go:40, modules/launchpad/types/keys.go:10); the USDL of every pool as the USDL ERC20 balance of the launchpad module EVM address, including the first-generation pool USDL the v6.12.0 handler moves there by ERC20 transfer; no mint right | launchpad.md |
| launchpad_treasury | launchpad | none | Existing treasury account (modules/launchpad/types/keys.go:14), unchanged | launchpad.md |
| pricefeed | pricefeed | none | Nothing; the module keeps observations and aggregates only | pricefeed.md |

Pool addresses are ordinary accounts and never enter maccPerms. Spot fees have no account of their own: they are accruals inside the spot module account.

## Tokens

| Token | Kind | Decimals | Address | Use | Contract file |
|---|---|---|---|---|---|
| USDL | existing ERC20 contract; no bank denom, no pointer, never minted | 6 (USDL_TOKEN_DECIMALS, contracts/libraries/Constants.sol:16) | 0x85FcD13735F4309833A503EE804ea32395851479 (USDL_TOKEN, contracts/libraries/Constants.sol:14) | Launchpad quote asset in ERC20 units read by balanceOf, the only perpetuals collateral and the spot quote asset; moved by transferFrom and transfer | launchpad.md, perps.md, spot.md |
| PAX | bank denom uhpx (BaseDenom, modules/evm/keeper/params.go:22) | 6, the exponent of the uhpx metadata written by baseline task 1.2.4 | ERC20 native pointer of uhpx created by baseline task 1.2.4, the first v6.12.0 hook | Base denom, pricefeed market 11, spot base asset; moved by bank transfer | spot.md, pricefeed.md |
| SID | bank denom layerxbridgetypes.SidioraDenom() (modules/layerxbridge/types/keys.go:53-59) | 6 (SidioraDecimals, modules/layerxbridge/types/keys.go:18) | ERC20 native pointer of the SID denom created by baseline task 1.2.4 | Pricefeed market 12, spot base asset; receives the legacy SID balances of the SID proxy (node/upgrades_v611.go:23-25) through the SID holder migration hook of the v6.12.0 handler; moved by bank transfer | spot.md, pricefeed.md |

Pointer rule: the PAX and SID token addresses are read with getNativePointer of 0x100A (precompiles/pointerview/pointerview.go:18), the view over GetERC20NativePointer (precompiles/utils/expected_keepers.go:127), and resolved back to the denom with GetNativePointee (modules/evm/keeper/pointer.go:444); decimals come from the bank metadata of the denom, never from a constant in the reader. USDL has no pointer: getNativePointer of any denom never returns its address, and its decimals are the ERC20 decimals().
