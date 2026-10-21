# Introducing KindleLaunch: The Architecture Behind the Next Generation AMM Launchpad

*A deep dive into the design decisions, layered architecture, and novel mechanisms powering the KindleLaunch Launchpad AMM on HyperPaxeer.*

---

## Foreword

Every protocol is a set of bets. Bets on what matters. Bets on what doesn't. Bets on what the next generation of DeFi users will actually need.

KindleLaunch is our bet.

This paper is not a whitepaper. It's an honest, technical account of how KindleLaunch is built — what we borrowed, what we rejected, what we invented, and why. If you've shipped smart contracts, you'll find the tradeoffs familiar. If you haven't, you'll find the principles accessible.

Let's get into it.

---

## The Problem Space

Launchpads have a trust problem.

The current generation suffers from one of two failure modes: they're either too permissive — allowing rug pulls, sniper bots, and whale manipulation to destroy communities before they form — or they're too restrictive, creating walled gardens where creativity and customization are sacrificed at the altar of safety.

The AMM layer underneath most launchpads is similarly compromised. Static fees don't reflect market reality. Monolithic contracts make auditing a nightmare. And when something breaks, it breaks everything.

KindleLaunch is designed to solve this at the architecture level — not the policy level. Good structure makes bad outcomes structurally harder to achieve.

---

## Standing on the Shoulders of Giants

We're not here to reinvent everything. We studied five protocols in depth and borrowed the best ideas from each:

**Uniswap V2** taught us the value of the Core/Periphery split: keep your mathematical heart minimal and immutable, and let a replaceable periphery handle user-facing complexity.

**Uniswap V4** gave us hooks — the idea that pool behavior can be extended through lifecycle callbacks without touching core logic. We've adapted this into what we call *Opticals*.

**Balancer V2** demonstrated that separating token accounting from pool math creates a cleaner, more auditable system. We took that lesson and applied it to fee accounting specifically.

**Pump.fun** pioneered the dual-reserve model — virtual reserves for pricing, real reserves for actual holdings. This is central to how KindleLaunch manages liquidity at launch.

**Aerodrome** showed how separating fee logic into a dedicated contract (rather than baking it into the pool itself) produces dramatically cleaner code and more flexible distribution strategies.

We've synthesized these lessons into a single coherent architecture. But we also made one major deliberate departure.

---

## The Key Architectural Decision: No Singleton

Uniswap V4 and Balancer use a singleton vault — one contract that manages all pools. It's elegant for multi-hop routing across arbitrary token pairs.

We chose not to do this.

Here's why: every KindleLaunch pool is paired with USDL. There are no multi-token paths. No complex routing trees. When your entire design assumes a single base asset, a singleton adds enormous complexity without meaningful benefit.

Instead, we use **per-pool Beacon Proxies**. Each pool is its own isolated contract instance. This gives us fault containment (a bug in one pool can't drain another), cleaner auditability, and — critically — atomic upgradability across all pools simultaneously via the Beacon pattern.

More on that shortly.

---

## The Virtual Reserve Model

Before diving into architecture layers, it's worth understanding the core AMM innovation: KindleLaunch's virtual USDL reserve.

Every pool launches with a *virtual* USDL reserve — a fixed amount (10,000 USDL by default) that exists for pricing purposes only, not actual holdings. The *real* USDL reserve starts at zero and grows as buys come in.

The effective reserve used for price calculation is:

```
Effective USDL = virtualUsdlReserve + realUsdlBalance
```

This means a token pool has a defined starting price even with zero real liquidity behind it. Early buyers are pricing against the virtual reserve. As the real reserve grows, the virtual reserve becomes proportionally less significant, and the token graduates toward real market-discovered pricing.

The math that flows from this is clean constant product:

```
BUY:   amountOut = (tokenReserve × amountInAfterFee) / (effectiveUsdl + amountInAfterFee)
SELL:  amountOut = (effectiveUsdl × amountInAfterFee) / (tokenReserve + amountInAfterFee)
```

One important invariant: **the protocol can never pay out more real USDL than it holds.** The explicit `amountOut <= realUsdlBalance` check is essential: the curve prices against virtual plus real reserves, and sufficiently large sells can quote more USDL than the pool actually holds. Such sells revert at the real-liquidity limit.

---

## The Six-Layer Architecture

KindleLaunch is organized into six distinct layers. The dependency rule is absolute: **data flows strictly downward.** No layer may import from anything above it.

```
Periphery → Opticals → Core-Logic → Data → Protocol → Libraries/Bases
```

Here's what lives in each layer and why.

---

### Layer 1: Protocol — The Rules of the Game

The Protocol layer defines the global invariants that every other contract respects.

**ProtocolConfig** is the single source of truth for all protocol parameters: fee floors and ceilings, the virtual USDL default, the creation fee (an anti-spam mechanism), and the weights used in dynamic fee calculation. Every contract that needs a parameter reads it from here — there's no duplication, no drift.

Fresh Sidiora deployments charge **1 USDL per pool creation** (1,000,000 base units of six-decimal USDL), excluding network gas. Factory and router creation paths collect this fee once and credit the treasury ledger. Core deployment rejects USDL tokens with other decimal counts. The fee is denominated in USDL; launchpad pricing does not use an oracle to track its dollar value. Existing deployments retain their stored fee: upgrading the implementation does not rerun initialization. Their administrator must call `ProtocolConfig.setCreationFee(1000000)` through the configured governance and Timelock process. The fee remains governable.

Custom deployment tooling must enforce the same six-decimal USDL requirement: `ProtocolConfig.initialize` itself does not validate token decimals.

The [creation-fee regressions](test/core/creation-fee.test.js) check payer, treasury and market state around all three entrypoints. Their `describe` groups use isolated `beforeEach` fixtures; the permit case covers the existing-allowance fallback with a bounded `deadline`. [ProtocolConfig tests](test/protocol/ProtocolConfig.test.js) check initialization and administrative controls, using the same six-decimal units as the `p6` helper in [test constants](test/helpers/constants.js). [Deployment tests](test/remediation/Deployment.test.js) exercise the `main` entrypoint in [deploy.js](scripts/deploy.js), including decimal rejection before transactions, fee preservation through governance handover and restart recovery. These are local tests; they do not establish a live deployment's current fee.

**Treasury** holds accumulated protocol revenue. It cannot modify config or interact with pools. Its only jobs are receiving and disbursing.

**Timelock** is the trust anchor of the entire system, and uniquely, it is the only governance contract we chose to make *immutable*. If the Timelock could be upgraded, the security model would be self-referential. The Timelock enforces its configured delay on governance-approved actions; deployment defaults to 48 hours.

**GovernanceModule** defaults to administrator-controlled governance through Timelock. The deployment uses `initializeAdmin(timelock, admin)` without a voting token; `SID_ADDRESS` is not required in this mode. The designated administrator queues changes with `adminExecute`, and Timelock enforces the delay before execution. Upgrade and role administration remain with Timelock. Set `GOVERNANCE_ADMIN_ADDRESS` to an administrator or multisig; otherwise the deployment wallet retains decision authority. Administrator-only deployments reject token proposals and administrator-mode deactivation. `GOVERNANCE_MODE=token` explicitly selects the existing SID checkpoint-voting path. See the [mainnet deployment instructions](operations/README.md#launchpad-mainnet-deployment).

---

### Layer 2: Data — The Memory of the Protocol

Three contracts form the protocol's persistent record-keeping.

**EventEmitter** is the single event hub for the entire protocol. Inspired by GMX V2's design, every meaningful action in KindleLaunch — a swap, a market creation, a fee distribution, a config update — emits through this one contract. Every event carries a poolId, a block timestamp, and a block number. Indexers need to track exactly one address.

**PoolRegistry** handles on-chain pool discovery. It maps token addresses to pool addresses, creators to their pools, and stores creation metadata. The Router validates against it. The Quoter queries it. The Factory writes to it. Nobody else does.

**FeeAccumulator** is where fee accounting lives — completely separated from the pool itself. After each swap, the pool records the fee amount here and then focuses on what it's good at: AMM math. The FeeAccumulator handles the four distribution strategies (more below), the protocol's cut, and the treasury sweep. Clean separation of concerns.

---

### Layer 3: Core-Logic — The Heart

This is where value is created and traded.

**KindleLaunchFactory** orchestrates market creation in a single transaction. In sequence: read parameters from ProtocolConfig, collect the creation fee, deploy a new ERC20 token via CREATE2, deploy a new pool via Beacon Proxy, transfer the entire token supply to the pool, mint the fee rights NFT to the creator, register everything in PoolRegistry, emit MarketCreated. That's a complete market, atomically.

**KindleLaunchPool** is the AMM engine. It is deliberately narrow in scope: AMM math, state transitions, fee recording, and optical hooks. It does not manage fee strategies, mint tokens, or register itself. Dynamic fees are calculated via the FeeLib library, considering pool age, price volatility (tracked via an 8-slot circular price buffer), and whale concentration. After calculating the fee, it calls FeeAccumulator.recordFee() and moves on.

**KindleLaunchERC20** is intentionally the simplest contract in the system: a standard ERC20 with Permit support, minted once by the Factory, supply fixed forever. Users must be able to trust that their token code doesn't change. So it doesn't.

**KindleLaunchNFT** represents fee rights. One NFT per pool, minted to the creator at launch. Transfer it, and you transfer all future fee rights. The NFT stores the chosen fee strategy and can be updated by whoever holds it. This is a fundamentally different model from liquidity position tokens — here, the NFT isn't about liquidity, it's about ongoing protocol revenue rights.

---

### Layer 4: Opticals — Programmable Pool Behavior

Opticals are plugin contracts that inject custom logic at defined points in a pool's lifecycle. The interface exposes four hooks: `beforeSwap`, `afterSwap`, `beforeFeeDistribution`, and `afterFeeDistribution`. A bitmap flags which hooks are active, so the pool skips unused callbacks without wasted gas.

Six preset Opticals ship with the protocol:

| Optical | What It Does |
|---|---|
| **AntiSnipe** | Blocks buys above a supply percentage threshold in the first N blocks |
| **MaxWallet** | Enforces a maximum token holding per wallet |
| **BuybackBurn** | Redirects a portion of fees to buy back and permanently burn tokens |
| **Tax** | Adds a configurable buy/sell tax routed to the NFT owner |
| **Cooldown** | Enforces minimum time between trades per wallet (anti-bot) |
| **Vesting** | Implements gradual token unlock for specified wallets |

Opticals are immutable once deployed. New behavior requires a new deployment. Audited once, trusted forever.

The OpticalRegistry provides trust signaling — unregistered opticals are flagged as unverified to users and frontends, but they still function. This is freedom with transparency, not control.

---

### Layer 5: Periphery — What Users Touch

The Periphery layer is the public face of the protocol.

**Router** is the single entry point for all user transactions: creating markets, buying, and selling. It's effectively stateless — it validates, routes, and delegates. It handles all token transfers into pools before triggering swaps. It inherits Multicall, meaning multiple actions can be batched into a single transaction.

**Quoter** is read-only and consumes no gas via staticcall. It provides exact input/output quotes with fee breakdowns and price impact, pool stats, market cap calculations, and paginated pool discovery. The frontend leans on this heavily.

**FeesRouter** is the interface for NFT holders to manage their fee strategy: switch between CLAIM, BURN, AIRDROP, and LP_REWARDS, and execute each accordingly.

---

### Layer 6: Libraries and Bases — Zero External Dependencies

Every mathematical and utility primitive in KindleLaunch is built in-house. No OpenZeppelin. No external library dependencies. This was a deliberate choice: fewer external contracts means fewer attack vectors, simpler audits, and full understanding of every line of code in the system.

KindleLaunchMath handles fixed-point arithmetic. FeeLib encapsulates dynamic fee calculation. ReserveLib handles the constant product math. TransferHelper wraps token transfers safely. BitFlag manages the optical hook bitmap.

The base contracts (ERC20Base, ERC721Base, Proxy patterns, AccessControl, ReentrancyGuard, etc.) are similarly in-house implementations of battle-tested patterns.

---

## Upgradeability Without Trust Compromise

This is one of the harder problems in protocol design: how do you build something that can be improved over time without creating an upgrade backdoor that undermines user trust?

KindleLaunch's answer is layered and explicit:

**Singleton contracts** (Factory, Router, NFT, etc.) use UUPS proxies. An upgrade can only be queued through the GovernanceModule and executed through the Timelock after its mandatory delay.

**Pool instances** use Beacon Proxies. There is exactly one PoolBeacon contract. Every pool reads its implementation from this beacon. Upgrading the beacon's implementation upgrades all pools simultaneously — atomically, consistently, with no stragglers. The beacon itself is only upgradeable through the same Timelock.

**User tokens** (KindleLaunchERC20) are immutable. No upgrade path, ever. Token holders need this guarantee.

**The Timelock itself** is immutable. This is the root of the trust model.

**Optical presets** are immutable. New behavior = new contract = new audit.

The result: no EOA admin key holds upgrade power. No single developer can push a malicious upgrade in the middle of the night. Every change is delayed, observable, and cancellable by guardians.

---

## Fee Strategy: Four Ways to Win

When a creator launches a market on KindleLaunch, they mint an NFT that represents their fee rights. That NFT can be configured with one of four strategies at any time:

**CLAIM** — Fees accumulate in USDL and the NFT holder withdraws on demand. The purest form of protocol revenue sharing.

**BURN** — Accumulated fees are transferred to the dead address. A deflationary signal. A commitment mechanism.

**AIRDROP** — Fees accumulate and are distributed proportionally to all current token holders. The gas cost of on-chain iteration is viable on HyperPaxeer's subsidized network. It turns fees into community rewards.

**LP_REWARDS** — Fees are transferred to the pool as additional real USDL reserve. This increases the pool's real liquidity depth over time, compounding into tighter spreads and better prices for everyone. Mechanically clean: the pool simply calls syncReserves() and re-reads its own balance, avoiding any upward call across the layer boundary.

---

## Dynamic Fees

KindleLaunch's fee system is not static. It is calculated fresh on every swap, considering three inputs:

**Pool Age** — New pools carry higher fees. This reflects genuine uncertainty about price discovery in the early blocks. Fees decay over time as the market matures.

**Volatility** — A circular buffer of 8 recent price snapshots tracks short-term price movement. High volatility raises fees to compensate liquidity providers for increased risk.

**Whale Concentration** — Large relative order sizes indicate concentrated buying or selling. Higher concentration raises fees proportionally.

The three components combine with configurable weights defined in ProtocolConfig. The result is a fee that reflects actual market conditions — not a number someone picked at deployment and never changed.

Fees are bounded by minFeeBps (0.10%) and maxFeeBps (3.00%), with a 0.30% base.

---

## Deployment Details

- **Chain**: HyperPaxeer (EVM Chain ID 125)
- **Solidity**: 0.8.27, viaIR enabled
- **Upgrade Authority**: Timelock via GovernanceModule
- **Governance Token**: SID (existing ERC20 on Paxeer)
- **Initial Admin**: Network-owned multisig, transitioning to full SID-holder governance

---

## What We Are and Aren't

KindleLaunch is a launchpad AMM — specifically designed for the one-sided USDL-paired model that launchpads require. It is not a general-purpose DEX. It is not trying to be Uniswap.

It is designed for:
- Projects that want to launch a token with real market mechanics from block one
- Creators who want genuine fee rights, not just governance tokens
- Communities that want protection from snipers and bots built into the protocol layer
- Developers who want to extend pool behavior through a clean, auditable hook system

---





### Operator safety and upgrade qualification

The [production operations runbook](operations/README.md) covers limited emergency pause authority, governed recovery, oracle jump limits and readiness, optical identity renewal, read-only health inspection, and reproducible release qualification. Existing deployments require explicit role wiring and the documented migration checks before these capabilities are operational.

Run scripts with the connected Hardhat network; `NETWORK_TYPE`, if supplied, must agree. The canonical manifest is `deployments/<network>-addresses.json`, with flat `Name_proxy`/`Name_impl` keys and `_meta.network` as the chain ID. Legacy manifests require an explicit operator migration; no implicit network fallback is safe.

Core deployment uses temporary deployer authority, hands administration to Timelock, disables governance bootstrap mode, and hands proposer authority to GovernanceModule. Core `ADMIN_ADDRESS` overrides fail before deployment. Beacon ownership acceptance is queued through Timelock and remains pending until its ETA: the deployment manifest records that outstanding step. A deployment is not finalized until delayed acceptance succeeds and owner/roles are checked. MetaAG likewise wires under temporary authority and hands administrative roles to the existing Timelock; relayer, keeper and fee collector remain explicit operational roles.

Upgrade commands require `UPGRADE_PLAN` pointing to a reviewed JSON plan: `chainId`, `migrationCalldata`, `simulation: {file, sha256}`, and `contracts` entries containing `contract`, `proxyKey`, `beacon`, `oldImplementation`, `oldCodeHash`, `baseline`, `baselineSha256`, and `newArtifactHash` (keccak256 creation bytecode). Preflight checks live implementation identity, pinned storage baseline, recursive type compatibility, artifact identity and simulation evidence hash before broadcasting. Evidence hashes establish identity, not quality: reviewers must verify the fork simulation and migration assertions. Retain baseline layouts and prior artifacts; incompatible layouts require a separately designed migration, not a refreshed baseline. Storage gaps are conservatively immutable in the automated checker.

Documentation generation is opt-in: `RUN_DOCGEN=true` enables compile-time docgen. Install the pinned static analyzer in an isolated environment with `python -m venv .venv-audit`, `.venv-audit/bin/pip install -r requirements-audit.txt`, then `.venv-audit/bin/slither . --config-file slither.config.json`. Local tests, static analysis and code review do not constitute live deployment qualification.


Beacon upgrade plans additionally require `tokenInventory: {file, sha256}` on the pool entry. The JSON inventory must cover every registered pool and contain `pool`, `token`, `runtimeHash`, and reviewed `transferHookSimulation` evidence. Preflight verifies token identity, pool binding, and historical balance/supply APIs. Existing immutable tokens lacking these capabilities require a separate migration design and cannot be made compatible by upgrading their pool beacon. Legacy FeeAccumulator upgrades additionally need bounded accounting migration and reviewed immutable legacy airdrop allocations; legacy NFTs need bounded pool-index migration before new mints. Preserve historical artifacts and audit the actual migration transaction sequence.

Write helpers retain transaction hash, nonce, target, calldata and value before awaiting confirmation. They do not automatically resubmit ambiguous writes: reconcile the recorded receipt and operation-specific postcondition before retrying. Finalized core proxies and beacon are controlled by Timelock, so direct EOA upgrade scripts cannot bypass governance; prepare and execute upgrades through the governed proposal lifecycle.


MetaAG deployment defaults the Sidiora spot adapter to disabled. `SIDIORA_MIN_LIQUIDITY_RAW` must be a positive USDL amount in token base units; default is 1,000 USDL scaled using its live decimals. Explicit `ENABLE_SIDIORA_SPOT_REFERENCE=true` additionally requires an independent `USDL_USD_ORACLE_ADDRESS` implementing a fresh positive `getPrice(USDL)`; this opts into instantaneous manipulable pool prices, not manipulation resistance. The adapter's disabled default is not evidence of healthy price availability.


Interrupted deployments can resume with the same environment and `RESUME_DEPLOYMENT=true`. The manifest binds configuration, chain, deployer, constructor bytecode, confirmed transaction identities and runtime hashes. Confirmed implementations, proxies and immutable contracts are reused; wiring and administrative handoffs reconcile exact on-chain postconditions. Proxy and beacon implementation drift fails before new broadcasts. New creation intent, predicted address and nonce are persisted before broadcasting, so an ambiguous RPC response cannot silently cause duplicate deployment. Ambiguous pending or failed creation receipts require explicit operator reconciliation rather than automatic resubmission. Keep the original checkpoint; do not replace it with a newly generated manifest. Expired beacon acceptance requires a reviewed governance cancellation/requeue.

For an existing local-node checkpoint, retain the original deployment variables and run:

```sh
RESUME_DEPLOYMENT=true node node_modules/hardhat/internal/cli/cli.js run scripts/deploy.js --network localhost
RESUME_DEPLOYMENT=true node node_modules/hardhat/internal/cli/cli.js run scripts/meta-ag/deploy-pecor-meta-ag.js --network localhost
```

Select the intended connected network explicitly. Core and layered plans occupy separate `_deployment` and `_metaAgDeployment` sections of the same manifest; layered recovery preserves the inherited core identity. The layered repair script delegates to the bound deployment plan when present. An unknown broadcast, missing or failed receipt, configuration mismatch, or implementation drift stops recovery for review instead of creating replacement contracts.
