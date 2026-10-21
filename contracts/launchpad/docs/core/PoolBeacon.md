# Solidity API

## PoolBeacon

Stores the SidioraPool implementation address. All pool proxies read from this.

_Thin wrapper around UpgradeableBeacon. Owner = Timelock.
     Upgrading this ONE contract upgrades ALL pools atomically._

### constructor

```solidity
constructor(address initialImplementation, address initialOwner) public
```

