# Governance window kit

Renders the proposals for the first governance window after the v6.11 upgrade and dry-runs each one through `paxd` with `--generate-only --offline --chain-id hyperpax_125-1`. A file paxd refuses fails the render.

```
tools/paxeer-x/gov/render.sh --out DIR --paxd build/paxd \
  --height H --release-url URL --release-sha256 HEX --deposit COINS --pax-usd PRICE \
  --pax-asset-id HEX64 --sid-asset-id HEX64 \
  --market-pax-sid HEX64 --market-btc-usd HEX64 --market-eth-usd HEX64 \
  --custody-network-id N --deposit-authority HEX64 \
  --sequencer-id HEX64 --sequencer-pubkey HEX64 \
  [--perp-margin pax|sid] [--rate-update-height H] [--sid-pointer 0xADDR] \
  [--first-batch N] [--last-batch N] [--bridge-config FILE]... [--bridge-manifest FILE] \
  [--from BECH32] [--chain-id ID]
```

| Output | Content | Dry run |
| --- | --- | --- |
| `01-software-upgrade.json` | Plan `v6.11` at `--height`, info naming the v6.11.0 release asset with its SHA-256 | `tx gov submit-proposal software-upgrade` |
| `02-fee-token-params.json` | evm `fee_token_enabled` true, `allowed_fee_denoms` usid at rate 3114000 (usid per whole PAX), `max_fee_token_rate_age` 3400000 blocks | `tx gov submit-proposal param-change` |
| `03-xweb-fee.json` | LayerXProposal with `xweb.MsgSetParams`, fee = 0.001 USD in uhpx at `--pax-usd` | `tx gov submit-proposal layerx-proposal` |
| `04-exchange-markets.json` | LayerXProposal with `layerxexchange.MsgSetMarket` for PAX/SID spot (SID margin), BTC-USD and ETH-USD perps (`--perp-margin`, default PAX) | `tx gov submit-proposal layerx-proposal` |
| `05-custody.json` | CustodyProposal: `MsgSetAsset` for uhpx and usid, `MsgUpdateParams` with the deposit root authority | `tx layerxcustody submit-proposal custody` |
| `06-anchor-sequencer-authorization.json` | Calldata for `setSequencerAuthorization` on the anchor precompile `0x…1014`, sent by the anchor authority | shape check |
| `bridge/<chain>/` | Output of `bridge/deploy/proposals` for each `--bridge-config`: bodies under `bodies/`, `proposals/04-proposal-open-chain.json` and `proposals/05-proposal-sidiora-cap.json` | `tx gov submit-proposal layerxbridge-proposal` |

Each validated file gets a `<file>.tx.json` beside it with the unsigned transaction.

Notes:

- The chain market listing carries only the market id, its custody margin asset and the enabled flag. Tick, lot and margin ratios are part of the LayerX market genesis, not this proposal.
- `MsgUpdateParams` replaces the whole custody parameter set. The kit writes the module defaults for the delays and liveness bound and an empty sequencer authorization list. Check these against the live parameters before you submit.
- The fee-token rate is checked when the proposal executes: `rate_update_height` (default `--height`) must not be above the execution height.
- `test.sh` builds paxd (or uses `PAXD`), renders with fixed inputs and asserts every file. It needs no network.
