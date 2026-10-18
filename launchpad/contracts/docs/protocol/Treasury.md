# Solidity API

## Treasury

Receives and holds protocol fee revenue. Disbursements via governance.

_UUPS proxy. Only DEPOSITOR_ROLE can deposit. Only admin can withdraw._

### DEPOSITOR_ROLE

```solidity
bytes32 DEPOSITOR_ROLE
```

### eventEmitter

```solidity
address eventEmitter
```

### constructor

```solidity
constructor() public
```

### initialize

```solidity
function initialize(address _eventEmitter, address _admin) external
```

### deposit

```solidity
function deposit(address token, uint256 amount) external
```

Deposit tokens into the treasury

_Caller must have DEPOSITOR_ROLE. Tokens must be pre-approved._

### withdraw

```solidity
function withdraw(address token, address to, uint256 amount) external
```

Withdraw tokens from the treasury

_Only admin (governance/timelock) can withdraw._

### getBalance

```solidity
function getBalance(address token) external view returns (uint256)
```

Returns the treasury balance for a given token

### _authorizeUpgrade

```solidity
function _authorizeUpgrade(address) internal
```

