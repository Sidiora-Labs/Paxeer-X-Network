# Solidity API

## SidioraERC20

Minimal immutable ERC20 token deployed via CREATE2 by the Factory

_Total supply minted once in constructor to recipient. No mint/burn after creation._

### constructor

```solidity
constructor(string _name, string _symbol, uint256 _totalSupply, address _recipient) public
```

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| _name | string | Token name |
| _symbol | string | Token symbol |
| _totalSupply | uint256 | Total supply to mint (6 decimals) |
| _recipient | address | Address to receive the entire supply (the pool) |

