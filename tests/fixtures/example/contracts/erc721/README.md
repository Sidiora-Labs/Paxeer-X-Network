`ERC721.sol` declares the `DummyERC721` contract and the interfaces it implements. The committed artifacts are `DummyERC721.abi` and `DummyERC721.bin`; Go tests (for example `modules/evm/client/wasm/query_test.go`) deploy `DummyERC721.bin` directly, so no Go binding is committed.

To regenerate the artifacts, run from the repository root and keep only the `DummyERC721` files:

```
solc --bin -o tests/fixtures/example/contracts/erc721 tests/fixtures/example/contracts/erc721/ERC721.sol --overwrite
solc --abi -o tests/fixtures/example/contracts/erc721 tests/fixtures/example/contracts/erc721/ERC721.sol --overwrite
```

To produce a Go binding as well:

```
abigen --abi=tests/fixtures/example/contracts/erc721/DummyERC721.abi --pkg=erc721 --out=tests/fixtures/example/contracts/erc721/ERC721.go
```
