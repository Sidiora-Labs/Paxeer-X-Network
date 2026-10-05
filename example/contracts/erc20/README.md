`ERC20.sol` declares the `IERC20` interface and the `ERC20` contract; solc writes the `.abi` and `.bin` of both, and `ERC20.go` is the abigen binding of `ERC20`.

To regenerate these files, run from the repository root:

```
solc --bin -o example/contracts/erc20 example/contracts/erc20/ERC20.sol --overwrite
solc --abi -o example/contracts/erc20 example/contracts/erc20/ERC20.sol --overwrite
abigen --abi=example/contracts/erc20/ERC20.abi --pkg=erc20 --out=example/contracts/erc20/ERC20.go
```
