# EVM RPC .io / .iox tests

Integration tests for Paxeer X chain EVM RPC compatibility with Ethereum JSON-RPC. The suite runs every `.io` and `.iox` file under [`testdata/`](testdata/) against a live RPC endpoint.

### `.io` vs `.iox`

- **`.io`** - vanilla JSON-RPC fixtures (`>>` / `<<`) with no harness directives.
- **`.iox`** - same line format, plus harness extensions: `@ bind` / `<< @ ref_pair`, `@ expect_body_contains`, `@ expect_response_header`, and the `__SEED__` / `__REVERTER__` placeholders.

## How to run

1. Start the local cluster from the repo root: `make docker-cluster-start` (EVM RPC on port 8545; add `DOCKER_DETACH=true` to run it in the background).
2. Run the script from the repo root:
   ```bash
   ./integration_test/evm_module/scripts/evm_rpc_tests.sh
   ```

Before `go test`, the script uses `docker exec` on the node container to associate the sender, send one EVM tx and deploy two contracts (a minimal contract and a reverter), so data-dependent `.iox` fixtures have a known block, tx and contract. It then runs this package with `PAX_EVM_IO_RUN_INTEGRATION=1` and the websocket tests in `../ws_test` with `PAX_EVM_WS_RUN_INTEGRATION=1`.

Script knobs:

| Variable | Default | Purpose |
| -------- | ------- | ------- |
| `PAX_EVM_RPC_URL` | `http://localhost:8545` | RPC endpoint under test |
| `PAX_EVM_IO_TX_CONTAINER` | `pax-node-0` | Container used to send the seed transactions |
| `PAX_EVM_IO_TX_FROM` | `admin` | Key that signs the seed transactions |
| `PAX_EVM_IO_TX_PASSWORD` | local keyring password | Keyring password inside the container |
| `PAX_EVM_IO_TX_RECIPIENT` | fixed test address | Recipient of the seed EVM transfer |
| `PAX_EVM_IO_PROJECT_ROOT` | repo path inside the node image | Where the contract hex files live in the container |
| `PAX_EVM_IO_KEYRING_BACKEND` | unset | Passed as `--keyring-backend` when set |

To run a subset with extra `[DEBUG]` output, set `PAX_EVM_IO_DEBUG_FILES` to a comma-separated list of fixture paths relative to `testdata/` (for example `eth_call/call-contract.io`).

### Legacy `pax_*` gating

Legacy `pax_*` / `pax2_*` methods are gated by `enabled_legacy_pax_apis`. The default `paxd init` allowlist is `pax_getPaxAddress`, `pax_getEVMAddress` and `pax_getCosmosTx` (see `rpc/config/config.go`). The docker localnet `app.toml` enables every gated method except `pax_sign`.

Deprecation is asserted in `testdata/pax_legacy_deprecation/`:

- `pax_sign-disabled.iox` - gate errors carry `error.data` `legacy_pax_deprecated`.
- `deprecation-success.iox` - allowlisted calls succeed and carry the `Pax-Legacy-RPC-Deprecation` response header (body unchanged).
- `batch-nonobject-tail-gate.iox` - a JSON-RPC batch with `pax_sign` and a trailing non-object entry; asserts `legacy_pax_deprecated`, `Invalid Request` and `-32600` in the raw body (see `rpc/pax_legacy_http.go`).

Directives used by these fixtures:
- `@ expect_body_contains substring` - response body must contain the substring.
- `@ expect_response_header Header-Name` - response must include that HTTP header (case-insensitive lookup).

### Comparing legacy vs giga (RPC parity)

To check that the giga executor behaves like the legacy executor at the spec level (same methods return result vs error):

The suite does not detect which executor a node uses. It only sends JSON-RPC to `PAX_EVM_RPC_URL`, so you choose the executor by how you start the node or which URL you pass.

Run a local cluster with giga enabled:

```bash
# All 4 nodes use giga (and OCC), foreground:
GIGA_EXECUTOR=true GIGA_OCC=true make docker-cluster-start

# Same, in the background so you can run the RPC test script:
GIGA_EXECUTOR=true GIGA_OCC=true DOCKER_DETACH=true make docker-cluster-start
# Wait until build/generated/launch.complete has 4 lines, then:
./integration_test/evm_module/scripts/evm_rpc_tests.sh
```

Without `GIGA_EXECUTOR` and `GIGA_OCC`, the cluster uses the legacy (V2) executor. The node image applies these in `docker/localnode/scripts/step4_config_override.sh`.

1. Run the suite against the legacy endpoint and note the final report:
   ```bash
   PAX_EVM_RPC_URL=<legacy_url> ./integration_test/evm_module/scripts/evm_rpc_tests.sh
   ```
   The report looks like:
   ```
   ========== Pax EVM RPC .io/.iox test report ==========
     Total:  ...
     Passed: ...
     Failed: ...
     Skipped: ...
     Pass rate: ...%
   ```
2. Run the same suite against the giga endpoint:
   ```bash
   PAX_EVM_RPC_URL=<giga_url> ./integration_test/evm_module/scripts/evm_rpc_tests.sh
   ```
3. Compare Total, Passed, Failed and Skipped. Same numbers mean spec parity for that run. Any difference points to a method that returns a result on one node and an error on the other.

For a fair comparison both endpoints should serve the same chain (same genesis and blocks), so the seed block and deploy tx exist on both.

## Test mix

| Kind      | Count | Description |
| --------- | ----- | ----------- |
| **.io**   | 97    | Request/response fixtures curated from [ethereum/execution-apis](https://github.com/ethereum/execution-apis), plus Paxeer X additions. |
| **.iox**  | 64    | Paxeer X fixtures using bindings, placeholders and directives; includes `not-supported.iox` files and `pax_legacy_deprecation/*.iox`. |
| **Total** | 161   | All under `testdata/`; the runner executes every file. |

Fixtures live in `testdata/`; see [`testdata/README.md`](testdata/README.md). Methods that answer with an explicit unsupported error are listed in [`docs/evm_jsonrpc_unsupported.md`](../../../docs/evm_jsonrpc_unsupported.md).

### Fixtures replaced by self-contained versions

Some execution-apis fixtures depended on contracts at fixed addresses that only exist on the execution-apis test chain. They are replaced by fixtures that use the script-deployed reverter:

| Upstream fixture | Replacement |
| ---------------- | ----------- |
| `eth_call/call-revert-abi-error.io` | `eth_call/call-revert-abi-error-pax.iox` |
| `eth_call/call-revert-abi-panic.io` | `eth_call/call-revert-abi-panic-pax.iox` |
| `eth_estimateGas/estimate-call-abi-error.io` | `eth_estimateGas/estimate-call-abi-error-pax.iox` |
| `eth_estimateGas/estimate-failed-call.io` | `eth_estimateGas/estimate-call-abi-error-pax.iox` and `estimate-call-abi-panic-pax.iox` |

## What is checked

**Spec-only:** for each request/response pair, the runner only checks that the response kind matches the expected one: presence of `result` vs `error`. Response values are not compared. Batch responses need an explicit directive.

## Outcomes

- **Pass** - response kind matches the expected kind.
- **Skip** - a required binding or placeholder is missing (for example `${txHash}`, or `__REVERTER__` when `PAX_EVM_IO_REVERTER_ADDRESS` is unset).
- **Fail** - response kind mismatch. The runner logs the actual `error.code` and `error.message` (or a short `result` snippet), which tells apart not implemented (`-32601`), invalid params (`-32602`), a disabled endpoint, or another spec mismatch.

### What "seed" means here

**Seed** is the block the script creates before the tests run, so data-dependent fixtures have deterministic data to query.

1. The script sends one EVM tx and deploys one contract; the deploy block is the block that includes that deploy.
2. The script sets `PAX_EVM_IO_SEED_BLOCK` to that block number (hex) and `PAX_EVM_IO_DEPLOY_TX_HASH` to the deploy tx hash.
3. In `.iox` fixtures, `__SEED__` is replaced by that block number (or by `"latest"` if the seed is not set).
4. Fixtures can bind `${txHash}` from a first request (for example `eth_getBlockByNumber(__SEED__, true)` -> `result.transactions.0.hash`); `${deployTxHash}` is pre-filled from the script when set.
5. The script also deploys a reverter contract and sets `PAX_EVM_IO_REVERTER_ADDRESS`; `__REVERTER__` is replaced by that address. The reverter answers empty calldata or `0x01` with `Error("user error")` and `0x02` with a panic.
