# Private control primitives

This package is phase-one implementation, not a deployed operator API or a launch
qualification. Its PostgreSQL transactions and independently observed private
Anvil receipts are executable through `python3 security/control/check.py`.
The checker reconstructs a temporary Go module from the frozen shared manifests,
uses the already-installed dependency cache with readonly module resolution,
requires the authorized PostgreSQL fixture on 25432, and starts an owned chain125
Anvil whose mutations use an exclusively owned mode0700-directory IPC socket.
No existing TCP responder is used for mutation. Private fixture credentials are
public test data from the checked-in bootstrap compose, not production secrets.

## Authority and storage boundaries

`Identity.Resolve` must validate a currently active D11 credential and return its
canonical principal, role and exact service/resource scopes. `Identity.Active`
must compare the entire approval identity snapshot against currently active
identity, role, expiry and scopes, returning false on any change or outage.
Neither interface accepts client-asserted roles. Identity is checked after DB
lock acquisition and again after verifier callbacks before successful commit.
Credentials never enter persisted objects or audit. Reasons must be nonempty
valid UTF8 without CR/LF/NUL and at most1000 bytes; consumers must avoid secret
content in reasons. Audit contains actor/role/operation/scope/reason/result only.

All control writes are serialized by a schema-specific transaction advisory lock.
Each mutation has a five-second total bound, two-second lock timeout, four-second
statement timeout and one-second rollback cleanup. Domain denials roll back a
savepoint, then commit only an immutable denial audit. Unexpected database or
audit errors roll back everything. Audit UPDATE/DELETE/TRUNCATE are rejected by
triggers; production runtime roles must be unable to own/alter these objects or
disable triggers. `Install` is a migration-only constructor, not a public route.
The single lock is appropriate for low-volume private control transitions;
throughput and500kDAU suitability are unqualified.

Inventory and caps are a single platform authority, requiring exact `platform`
scope. Actions, incidents, bounties and their settlement obligations persist the
canonical service/resource scope and reject caller-supplied alternate scopes.
Automation additionally requires the canonical owner ID to equal the actor.
Unknown roles, scopes, kinds and privileged datasets deny by default. Infrastructure
has no finance/support/moderator inheritance. Moderator inventory adjustment and
support private-message/payment-secret export are denied and audited. Finance
can inspect liabilities but this package contains no wallet signing interface.

## Durable action and caps semantics

Proposals bind immutable kind/scope/reason/proposer/payload SHA256/expiry to an
idempotency key. A conflicting reuse is rejected. Every transition uses an
expected uint64 revision, with overflow denied. Approval requires a distinct
operator; execution revalidates the approver. Approved replay/redrive/export
execution creates one durable job in the same transaction. Ten retries return
the same job even after a terminal result; they cannot execute another effect.
The indexer/executor must implement its own idempotent inbox and effect/checkpoint
transaction, then expose read-only `JobResultVerifier` proof. `CompleteAction`
marks a running action/job succeeded or failed only after that proof. No replay
worker is integrated by these package tests.

Caps cover pool_creation, quote, socket and paid_features. Increasing numeric quota, extending expiry, advancing effective time or
reactivating expired positive authority requires a matching, one-use, dual-approved
cap_change payload. Single-operator changes must disable with zero quota or make
both positive quota and active interval subsets of existing versioned authority. Version1, effective time,
expiry and revision are persisted. Admission rereads PostgreSQL without a stale
cache, and denies unavailable/unknown/future/expired state. Consumer read-only
market routes must bypass write admission. Downstream owners must enforce the
numeric limit and wire the frontend pre-wallet gate, precise capability warning,
and propagation metrics. The primitives do not themselves prove the complete
frontend disable journey or cross-replica60-second propagation.

## Disclosed automation

Only recommendation mode `disclosed_assistant` is implemented. Policies begin
in draft and require explicit enable. They contain immutable owner, disclosed
origin, asset, instruments, quote age, expiry, request and spend ceilings.
Recommendation reservations produce amount0, suggestion state and disclosure;
there is no transaction-hash or fabricated-balance field. Request budgets are
reserved atomically per owner AND asset across all policies. Updating a budget
preserves usage; creating more agents does not reset the owner budget. Quote age,
allowlist, policy state and both policy/owner expiry are checked under lock.
Revocation/stopping blocks new reservations while retaining old durable history.
Retrying an existing key returns its original reservation, not a new suggestion.
No recommendation reserves money or affects organic rankings. Discovery owners
must carry the disclosure and exclude sponsored activity from organic signals.

Delegated execution returns `authority_unavailable` unconditionally. This is an
explicit release block until a real supported contract/provider delegation,
trading quote/consent validation and per-owner monetary settlement reservation
are independently qualified. No server-held user signing keys are supported.

## Funded bounties and external inventory

Amounts are canonical nonnegative uint256 decimal strings. PAX is native chain125
with18 decimals; current USDL is exactly
0x85fcd13735f4309833a503ee804ea32395851479 with6 decimals. No cross-asset sums exist.
The launch protocol creation fee is1000000 raw USDL, with native gas separate;
this package does not change the factory fee or its deployment.

Bounties record title/rules/reward asset and amount/custody funding source/deadline/
published refund disposition. Draft cannot become funded without an independently
confirmed inventory observation, matching source custody, fresh within60 seconds,
and enough atomically unreserved inventory. Funding reserves the full liability
before active publication. Two80-unit reserves against100 units admit one.
Expired drafts cannot reserve. Active/submitted/adjudicating/awarded transitions
are constrained; cancellation/expiry releases pending/reserved liability and
records reversed history. It never fabricates a refund transaction.

Award requires a configured trusted `AwardAuthority` that binds canonical bounty,
acceptance rules, reviewed submission evidence and exact beneficiary. Missing
reviewer authority rejects. The test reviewer is explicitly synthetic; it proves
admission and transaction rollback, not real adjudication or payout authority.
Award atomically creates the pending obligation and durable settlement outbox.
The same obligation key survives timeout/unknown states. This package never
submits payments, so an unknown response cannot cause an automatic second payout.
The external payment worker must reconcile the same key before any resubmission.

`ChainVerifier` is read-only and requires trusted current-manifest/custody approval
on every read. It independently checks chain125, pinned custody, authentic balance
at a committed block, ERC20 decimals, successful receipt, exact canonical block
hash, amount and recipient. PAX settlement additionally verifies signed transaction
chain/source; ERC20 settlement requires an exact USDL Transfer from custody to
beneficiary. A successful unique receipt can settle one obligation only. A second
obligation cannot consume it. No ordinary reorg/confirmation-depth waiting is
added: the frozen Paxeer chain profile specifies deterministic committed finality.
Anvil tests prove canonical receipt handling, not Paxeer consensus behavior.
Production current manifest is active (Timelock owns PoolBeacon since 2026-10-13); the private manifest
fixture used by tests is NOT an accepted mainnet attestation.

Inventory drift freezes new funded/awarded obligations while preserving visible
liabilities. PAX payouts also pay native gas, so reconciliation deliberately
freezes on unexplained gas debit. A separately approved gas/deposit/reversal
ledger and reconciliation worker are REQUIRED before production treasury use.
There is no manual bypass/unfreeze method. Real escrow/provider contract,
organizational payout controller, funding source provenance, reviewer policy,
payout worker, scheduled<=60second reconciliation and alerting remain release
prerequisites. No legal-control claim is made for an unconfigured provider.

## Incidents

Detection atomically records append-only evidence identities, quarantines the
service identity and emits a durable secret-revocation/cache-invalidation request.
Further privileged mutations for that identity deny immediately at this store.
Actual L45 secret/cache adapters must consume the outbox; package-level quarantine
does not prove external revocation completion. Public status is fixed nonsensitive
text. Recovery requires current dual approval bound to the exact incident,
rotation and integrity references and a trusted read-only RecoveryVerifier.
A newer quarantine for the same identity cannot be cleared by old recovery.
Recovery preserves incident history and requires ownership/adaptor integration;
real credential-compromise drills and external60-second containment remain pending.

## Frozen interface integration

These are internal domain models. They must not be serialized directly to public
frozen DTOs. Required adapters include OperatorJob executing->running,
succeeded->completed; internal funded/active/submitted/adjudicating/awarded bounty
states to frozen funding/open/reviewing/paid WITHOUT mapping awarded to paid.
Only a settled obligation can yield paid. Native Asset maps to AssetRef native,
address null, chainId"125", decimals18; USDL maps to erc20 and canonical address.
Reasons/revisions/cursors/quality/watermark wrappers must come from owning gateway
and read-model contracts, never invented zero values. No routes/manifest/lockfiles/
shared migrations/generated files are modified by L47.
