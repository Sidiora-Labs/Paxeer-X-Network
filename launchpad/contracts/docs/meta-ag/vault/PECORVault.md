# Solidity API

## PECORVault

Single multi-asset inventory vault for the Sidiora Meta-AG stack.
        Holds every non-Sidiora liquidity token, operator-gated pull/push,
        Timelock-admin upgrades. REPLACES live PECORVault
        (`0x6500B1B3F8067772041C68b2c51D8E7A84e20C31`) via migration
        Path M1 at Phase 10.3.

_Spec reference: `docs/architecture/pecor-sidiora-aggregator-spec.md` §7.5
     (FROZEN 2026-04-24). Regression tests: `test/meta-ag/vault/PECORVault.test.js`.

Inheritance (spec §7.5 — Pausable deliberately excluded):
  IPECORVault, Initializable, UUPSUpgradeable, AccessControl, ReentrancyGuard

Roles:
  - DEFAULT_ADMIN_ROLE → Timelock at deploy (invariant S1)
  - OPERATOR_ROLE      → PECOR, PECOROrders, VaultAdapter (granted later)

Storage layout (append-only per S12):
  slot 0:  AccessControl._roles
  slot 1:  weth                       (address)
  slot 2:  transactionTracker         (address)
  slot 3:  authorizedOperators        (mapping)
  slot 4:  _tokens                    (mapping)
  slot 5:  _registeredTokens          (address[])
  slot 6:  _registeredStablecoins     (address[])
  slot 7:  _tokenIndex                (mapping, 1-indexed)
  slot 8:  _stablecoinIndex           (mapping, 1-indexed)
  slot 9..58: __gap[50]_

### OPERATOR_ROLE

```solidity
bytes32 OPERATOR_ROLE
```

### ZeroAddress

```solidity
error ZeroAddress()
```

### ZeroAmount

```solidity
error ZeroAmount()
```

### InvalidArrayLength

```solidity
error InvalidArrayLength()
```

### TokenAlreadyRegistered

```solidity
error TokenAlreadyRegistered()
```

### TokenNotRegistered

```solidity
error TokenNotRegistered()
```

### ReservesMismatch

```solidity
error ReservesMismatch()
```

### NativeTransferFailed

```solidity
error NativeTransferFailed()
```

### WethOnly

```solidity
error WethOnly()
```

### weth

```solidity
address weth
```

Canonical wrapped-native token. Semantically immutable —
        assigned exactly once in {initialize}, never mutated afterwards.

### transactionTracker

```solidity
address transactionTracker
```

Transaction analytics hub (Phase 7 emitter). May be zero at
        bootstrap; Timelock rotates via {setTransactionTracker}.

### authorizedOperators

```solidity
mapping(address => bool) authorizedOperators
```

Mirror of `OPERATOR_ROLE` membership. Kept for O(1) view by
        integrations that prefer a boolean over `hasRole` staticcalls.

### constructor

```solidity
constructor() public
```

### initialize

```solidity
function initialize(address weth_, address tracker_, address admin_) external
```

Initialize the UUPS proxy (spec §7.5)

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| weth_ | address |  |
| tracker_ | address |  |
| admin_ | address |  |

### _authorizeUpgrade

```solidity
function _authorizeUpgrade(address) internal
```

### registerToken

```solidity
function registerToken(address token, bool isStablecoin_) external
```

### setStablecoinStatus

```solidity
function setStablecoinStatus(address token, bool isStablecoin_) external
```

### setOperator

```solidity
function setOperator(address operator, bool authorized) external
```

### setTransactionTracker

```solidity
function setTransactionTracker(address tracker) external
```

### emergencyWithdraw

```solidity
function emergencyWithdraw(address token, uint256 amount, address recipient) external
```

### syncReserves

```solidity
function syncReserves(address token) external
```

### syncAllReserves

```solidity
function syncAllReserves() external
```

### deposit

```solidity
function deposit(address token, uint256 amount) external
```

### depositBatch

```solidity
function depositBatch(address[] tokenList, uint256[] amounts) external
```

### depositNative

```solidity
function depositNative() external payable
```

Wrap native coin into WETH and credit reserves (WETH must be registered).

### pullTokens

```solidity
function pullTokens(address token, address from, uint256 amount) external returns (uint256 actualAmount)
```

Pull tokens from `from` into the vault (fee-on-transfer safe).

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| actualAmount | uint256 | Amount actually received after any token-side fees. |

### pushTokens

```solidity
function pushTokens(address token, address to, uint256 amount) external
```

### withdrawNative

```solidity
function withdrawNative(uint256 amount, address to) external
```

Unwrap WETH to native coin and forward to recipient.

### updateReserves

```solidity
function updateReserves(address tokenIn, uint256 amountIn, address tokenOut, uint256 amountOut) external
```

### getReserves

```solidity
function getReserves(address token) external view returns (uint256)
```

### getTokenInfo

```solidity
function getTokenInfo(address token) external view returns (bool, bool, uint8, uint256, uint256, uint256)
```

_Named return parameters are omitted here on purpose: the interface
     names one of them `isStablecoin`, but re-declaring that identifier
     in this contract's scope would shadow the public view function
     {isStablecoin(address)} and trip Solidity's shadow warning.
     Callers should rely on positional tuple order — identical to the
     interface declaration — e.g. via array destructuring._

### isStablecoin

```solidity
function isStablecoin(address token) external view returns (bool)
```

### getTokenDecimals

```solidity
function getTokenDecimals(address token) external view returns (uint8)
```

### hasLiquidity

```solidity
function hasLiquidity(address token, uint256 amount) external view returns (bool)
```

### getRegisteredTokens

```solidity
function getRegisteredTokens() external view returns (address[])
```

### getRegisteredStablecoins

```solidity
function getRegisteredStablecoins() external view returns (address[])
```

### getRegisteredTokenCount

```solidity
function getRegisteredTokenCount() external view returns (uint256)
```

### getAllReserves

```solidity
function getAllReserves() external view returns (address[] tokens, uint256[] reserves)
```

### getUntrackedFunds

```solidity
function getUntrackedFunds(address token) external view returns (uint256)
```

### receive

```solidity
receive() external payable
```

Accept native transfers only from the wrapped-native contract
        during {withdrawNative} unwraps. Every other sender reverts
        with {WethOnly}.

### _pullAndMeasure

```solidity
function _pullAndMeasure(address token, address from, uint256 amount) internal returns (uint256 received)
```

_Pull tokens via `transferFrom` and return the actually credited amount.
     Handles fee-on-transfer tokens by comparing the vault's balance
     delta across the call._

### _syncOne

```solidity
function _syncOne(address token) internal
```

_Reconcile a single registered token's reserves with the actual
     balance held by the vault. Adds any positive delta to `reserves`
     and `totalDeposited`. No-op on parity._

### _removeStablecoin

```solidity
function _removeStablecoin(address token) internal
```

_Compact 1-indexed removal from `_registeredStablecoins`._

### _fetchDecimals

```solidity
function _fetchDecimals(address token) internal view returns (uint8)
```

_Best-effort ERC20 decimals fetch; falls back to 18 on failure._

