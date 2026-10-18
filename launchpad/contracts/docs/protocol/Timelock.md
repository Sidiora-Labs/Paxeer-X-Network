# Solidity API

## Timelock

Trust anchor. Enforces delay on all governance-approved transactions.

_IMMUTABLE — not upgradeable. This is the security root._

### minDelay

```solidity
uint256 minDelay
```

### proposer

```solidity
address proposer
```

### guardian

```solidity
address guardian
```

### queuedTransactions

```solidity
mapping(bytes32 => bool) queuedTransactions
```

### DelayNotMet

```solidity
error DelayNotMet()
```

### TransactionNotQueued

```solidity
error TransactionNotQueued()
```

### TransactionAlreadyQueued

```solidity
error TransactionAlreadyQueued()
```

### Unauthorized

```solidity
error Unauthorized()
```

### ExecutionFailed

```solidity
error ExecutionFailed()
```

### ZeroAddress

```solidity
error ZeroAddress()
```

### InvalidDelay

```solidity
error InvalidDelay()
```

### TransactionQueued

```solidity
event TransactionQueued(bytes32 txHash, address target, uint256 value, bytes data, uint256 eta)
```

### TransactionExecuted

```solidity
event TransactionExecuted(bytes32 txHash, address target, uint256 value, bytes data, uint256 eta)
```

### TransactionCancelled

```solidity
event TransactionCancelled(bytes32 txHash)
```

### ProposerChanged

```solidity
event ProposerChanged(address oldProposer, address newProposer)
```

### GuardianChanged

```solidity
event GuardianChanged(address oldGuardian, address newGuardian)
```

### onlyProposer

```solidity
modifier onlyProposer()
```

### onlyGuardian

```solidity
modifier onlyGuardian()
```

### constructor

```solidity
constructor(uint256 _minDelay, address _proposer, address _guardian) public
```

### queueTransaction

```solidity
function queueTransaction(address target, uint256 value, bytes data, uint256 eta) external returns (bytes32 txHash)
```

Queue a transaction for future execution

### executeTransaction

```solidity
function executeTransaction(address target, uint256 value, bytes data, uint256 eta) external returns (bytes result)
```

Execute a queued transaction after its delay has passed

### cancelTransaction

```solidity
function cancelTransaction(address target, uint256 value, bytes data, uint256 eta) external
```

Cancel a queued transaction (guardian only)

### setProposer

```solidity
function setProposer(address newProposer) external
```

Change the proposer address (only current proposer can change)

### setGuardian

```solidity
function setGuardian(address newGuardian) external
```

Change the guardian address (only current guardian can change)

### getMinDelay

```solidity
function getMinDelay() external view returns (uint256)
```

### _getTxHash

```solidity
function _getTxHash(address target, uint256 value, bytes data, uint256 eta) internal pure returns (bytes32)
```

### receive

```solidity
receive() external payable
```

