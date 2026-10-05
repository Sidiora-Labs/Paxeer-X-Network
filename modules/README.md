# Paxeer X chain modules

Chain-specific modules of the Paxeer X chain (`paxd`) live under this directory:

- `evm` — native EVM execution, address association, receipts, pointers, and precompile integration
- `epoch` — time-based hooks and epoch lifecycle management
- `mint` — inflation and native-token minting policy
- `oracle` — validator exchange-rate voting and price aggregation
- `store` — module-level store integration helpers
- `tokenfactory` — permissioned creation and management of native token denominations
- `layerxanchor` — LayerX kernel checkpoint registry, finality authority, availability record, and guarantor bonds behind the `layerxAnchor` precompile
- `layerxcustody` — native custody of LayerX kernel funds; releases only against proof-carrying withdrawals and forced exits
- `layerxexchange` — LayerX exchange intents submitted through the `layerxExchange` precompile; margin moves only through `layerxcustody`
- `layerxbridge` — attested bridge between the Paxeer X chain and external chains: chain registry, attestor set, per-asset caps, and `bridgeIn`/`bridgeOut` through the `layerxBridge` precompile
- `layerxgov` — governance proposal content (`LayerXProposal`) that executes packaged parameter and admin messages for the LayerX-facing modules and `xweb` through the message router
- `launchpad` — native bonding-curve markets over `tokenfactory` denoms, reached through the `launchpad` precompile
- `xweb` — attested web data for EVM contracts through the `xweb` precompile; attestors, threshold, and pause state are set by governance

Framework-provided modules remain under `sdk/x/`; interchain applications live
under `interchain/modules/`.
