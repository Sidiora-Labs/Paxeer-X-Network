# Solidity API

## TransferHelper

Safe ERC20 transfer wrappers that handle non-standard return values

_Handles tokens that return bool, return nothing, or revert_

### TransferFailed

```solidity
error TransferFailed()
```

### TransferFromFailed

```solidity
error TransferFromFailed()
```

### ApproveFailed

```solidity
error ApproveFailed()
```

### safeTransfer

```solidity
function safeTransfer(address token, address to, uint256 value) internal
```

Safely transfers tokens (handles non-standard ERC20s)

### safeTransferFrom

```solidity
function safeTransferFrom(address token, address from, address to, uint256 value) internal
```

Safely transfers tokens from a sender (handles non-standard ERC20s)

### safeApprove

```solidity
function safeApprove(address token, address spender, uint256 value) internal
```

Safely approves a spender (handles non-standard ERC20s)

