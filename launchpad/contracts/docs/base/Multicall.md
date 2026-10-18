# Solidity API

## Multicall

Batch multiple calls in a single transaction

_Enables batching of multiple function calls to this contract_

### MulticallFailed

```solidity
error MulticallFailed(uint256 index, bytes reason)
```

### multicall

```solidity
function multicall(bytes[] data) external returns (bytes[] results)
```

Executes multiple calls in a single transaction

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| data | bytes[] | Array of encoded function calls |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| results | bytes[] | Array of return data from each call |

