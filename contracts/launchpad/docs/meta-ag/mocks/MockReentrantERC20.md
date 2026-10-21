# Solidity API

## MockReentrantERC20

Malicious ERC20 that re-enters a configurable target on transfer.

_Used by {PECORVault} tests to prove `nonReentrant` guards fire on
     emergencyWithdraw / pullTokens / pushTokens / deposit. Re-entry is
     opt-in so the same mock can double as a plain ERC20 where desired._

### name

```solidity
string name
```

### symbol

```solidity
string symbol
```

### decimals

```solidity
uint8 decimals
```

### totalSupply

```solidity
uint256 totalSupply
```

### balanceOf

```solidity
mapping(address => uint256) balanceOf
```

### allowance

```solidity
mapping(address => mapping(address => uint256)) allowance
```

### reentryTarget

```solidity
address reentryTarget
```

### reentryCalldata

```solidity
bytes reentryCalldata
```

### reentryArmed

```solidity
bool reentryArmed
```

### lastReentryOk

```solidity
bool lastReentryOk
```

### lastReentryRet

```solidity
bytes lastReentryRet
```

### lastReentryRecorded

```solidity
bool lastReentryRecorded
```

### Transfer

```solidity
event Transfer(address from, address to, uint256 value)
```

### Approval

```solidity
event Approval(address owner, address spender, uint256 value)
```

### ReentryArmed

```solidity
event ReentryArmed(address target, bytes payload)
```

### ReentryAttempted

```solidity
event ReentryAttempted(bool ok, bytes ret)
```

### constructor

```solidity
constructor(string name_, string symbol_, uint8 decimals_) public
```

### mint

```solidity
function mint(address to, uint256 amount) external
```

### armReentry

```solidity
function armReentry(address target, bytes payload) external
```

### disarmReentry

```solidity
function disarmReentry() external
```

### approve

```solidity
function approve(address spender, uint256 amount) external returns (bool)
```

### transfer

```solidity
function transfer(address to, uint256 amount) external returns (bool)
```

### transferFrom

```solidity
function transferFrom(address from, address to, uint256 amount) external returns (bool)
```

### _transfer

```solidity
function _transfer(address from, address to, uint256 amount) internal
```

### _maybeReenter

```solidity
function _maybeReenter() internal
```

