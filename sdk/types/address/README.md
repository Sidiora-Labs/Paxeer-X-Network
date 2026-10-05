# Address

This package defines Cosmos SDK address-related functions:

- `Hash(typ, key)` creates an address from an address type and a key.
- `Compose(typ, subAddresses)` creates an address from several sub-addresses.
- `Module(moduleName, key)` creates a module account address from a module name and a key.
- `Derive(address, key)` derives a new address from an address and a derivation key.
- `LengthPrefix` and `MustLengthPrefix` prefix an address with its length for use in store keys.

The scheme follows upstream Cosmos SDK ADR-028 (public key addresses).
