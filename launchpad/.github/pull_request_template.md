## Summary

What does this PR do? Link any related issues.

## Type of change

- [ ] Bug fix
- [ ] New feature
- [ ] Breaking change
- [ ] Refactor / performance improvement
- [ ] Documentation update
- [ ] Contract / deployment change

## Checklist

- [ ] I ran `make ci` for Go changes, or `pnpm compile && pnpm test && pnpm lint:sol` for contract changes.
- [ ] I added/updated tests for any changed code, including regression tests for bug fixes.
- [ ] Coverage gates remain satisfied (≥85% repo-wide, ≥90% for critical modules).
- [ ] I did not introduce lint warnings (`golangci-lint`, `go vet`, `solhint`, `eslint`, `prettier`).
- [ ] I did not use floats for money — all token/price/PnL math uses `math/big.Int` or `uint256`.
- [ ] I read the relevant source-of-truth docs (`README.md`, `knowledge/kindlelaunch.frozen.kvx`, `CLAUDE.md`) before making changes.
- [ ] I did not modify production infrastructure, validator config, or rate cards without explicit approval.
- [ ] I did not commit from the dev box; this PR is for the user's review/commit workflow.

## Additional notes

Any deployment steps, security considerations, or screenshots.
