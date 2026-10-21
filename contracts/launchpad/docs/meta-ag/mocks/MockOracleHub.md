# Solidity API

## MockOracleHub

Minimal IOracleHub spy that returns configured prices and
        availability per token. Used by MetaAGRouter unit tests to
        exercise the S4 sanity-check branches in isolation, without
        spinning up the full PriceOracle → PriceOracleAdapter → OracleHub
        composition.

_Scope-isolated under `contracts/meta-ag/mocks/`. Only implements
     the two functions MetaAGRouter._oracleSanityCheck consumes:
       - isPriceAvailable(token) → (bool, uint256 confidence)
       - getPrice(token) → uint256 price (18 decimals)

     The mock has no upstream relayer / staleness logic — tests drive it
     directly via setPrice(token, price) and setAvailable(token, bool)._

### CONFIDENCE

```solidity
uint256 CONFIDENCE
```

Default confidence returned alongside isPriceAvailable.

### setPrice

```solidity
function setPrice(address token, uint256 price) external
```

Set both price and availability in one call. Use 0 + false
        to clear a token's state.

### setAvailable

```solidity
function setAvailable(address token, bool flag) external
```

Override availability without touching price (tests that need
        to simulate the "price = X but adapter says not available" path).

### isPriceAvailable

```solidity
function isPriceAvailable(address token) external view returns (bool available, uint256 bestConfidence)
```

### getPrice

```solidity
function getPrice(address token) external view returns (uint256)
```

