# Bootstrap qualification

Use the versions in `tools/production/bootstrap/toolchain/versions.txt`, Docker Compose, and Anvil available on PATH. Run from the repository root. The contract fixture reads the sibling `../contracts` checkout and its installed Hardhat compiler artifacts. No production credentials are required.

Install the client with its pinned package manager:

```sh
cd client
npm exec --yes --package=pnpm@9.15.0 -- pnpm install --frozen-lockfile
cd ..
```

Run the complete current-source gate:

```sh
npm exec --yes --package=pnpm@9.15.0 -- python3 tools/production/qualify.py --task 0.1 --spec spec/production-system/spec.kvx --require-current-source
```

The gate verifies tools, dependency integrity and source provenance; starts loopback fixtures; deploys real contracts only on a private EVM; builds all thirteen Go modules and the client; and executes registered Go, SDK and frontend tests. Two Go suites may run concurrently. Each individual Go invocation uses one package build worker. Livestream currently has no existing tests and is explicitly build-only.

Evidence is written to `evidence/production-system/qualification/0.1/<run>/`. Read `result.json` for the final result and `progress.json` for completed checks during a run. A process exit, named skipped test, missing expected test, stale source or incomplete check makes qualification fail. Empty utility packages do not substitute for a module test suite.

The frontend tests use a separate ephemeral Anvil instance and terminate it afterward. The reusable Compose fixture persists for subsequent checks; stop it with `docker compose -f deploy/production/bootstrap/compose.yml down`. Do not add `-v` when retaining fixture data is desired.

A passing bootstrap establishes the existing-suite baseline. It does not certify feature completeness, browser journeys, deployment readiness or 500,000 daily active users. Later specification gates cover those obligations.
