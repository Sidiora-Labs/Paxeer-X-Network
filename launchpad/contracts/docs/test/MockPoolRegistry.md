# Solidity API

## MockPoolRegistry

Minimal stand-in for IPoolRegistry used by EventEmitter v2
        auth-mesh tests. Allows the test to mark arbitrary addresses
        as "registered pools" without spinning up the full Launchpad
        deployment graph (Factory → NFT → PoolRegistry).

### setRegistered

```solidity
function setRegistered(address pool, bool registered) external
```

### isRegisteredPool

```solidity
function isRegisteredPool(address pool) external view returns (bool)
```

