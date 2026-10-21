# Contributing to Paxeer X Network

Paxeer X Network is accounting and settlement software that moves real value. Contributions must preserve deterministic replay, conservation of value, explicit authority and fail-closed behavior.

This project is licensed under the Apache License 2.0. By contributing you agree that your work is licensed under those terms. Sign off every commit under the Developer Certificate of Origin:

```sh
git commit -s
```

Follow [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md). Report vulnerabilities only through [SECURITY.md](SECURITY.md). Questions go to the channels in [SUPPORT.md](SUPPORT.md).

## Repository layout

[docs/MONOREPO.md](docs/MONOREPO.md) describes the tree. In short:

- `apps/` holds the user-facing frontends, `services/` the off-chain programs, `sdk/` the client SDKs, `contracts/` the Solidity contracts, `tests/` the cross-cutting test suites, and `tools/` the developer and release tooling.
- The Paxeer X chain node (`paxd`) is the root Go module (`go.mod`, built through `chain.mk`); its packages live in root directories such as `node/`, `modules/`, `precompiles/`, `rpc/` and `consensus/`. The LayerX kernel (`layerxd`, C17) lives in `src/`, `include/` and `cmd/`. These root directories move under `core/` in a later release.
- `core/deps/`, `sdk/js/` and several directories under `tools/` are vendored upstream snapshots. Each has a `VENDOR.kvx` pin record, and `scripts/ci/vendor-verify.sh <dir>` proves the directory equals the pinned upstream commit. Do not edit them in place; change the upstream repository and re-pin.

## Specifications come first

Planned work is defined in KVX specifications under [`spec/`](spec/). Each feature directory holds a `spec.kvx` with requirements, acceptance criteria and a task list; each task has a status, a dependency wave, the tasks it requires, the paths it touches and one `verify_cmd`. [spec/workflow.kvx](spec/workflow.kvx) is the working contract and names the active feature.

Working a task:

1. Read [GOTCHA.kvx](GOTCHA.kvx), the repository's append-only ledger of known pitfalls, and the feature's `spec.kvx`.
2. Take the next pending task whose required tasks are done, and mark it `in_progress`.
3. Implement the whole task before building or testing it.
4. Qualify it once: format the touched files, run one build of its targets and run its `verify_cmd`. Fix failures the task caused; record unrelated or pre-existing failures as one entry in the feature's `qualification.kvx` instead of chasing them.
5. Mark the task `done` only after its `verify_cmd` passes.

A change to planned behavior changes the specification in the same pull request. A reproducible defect can be fixed without a spec task; link the issue instead.

Files generated from KVX are not edited by hand: `AGENTS.md`, `CLAUDE.md` and the other IDE rule files are rendered from `spec/workflow.kvx` by `tools/specgen`. `make specgen-lint` checks that they are current.

## Commits

- Sign off every commit (`git commit -s`).
- One logical change per commit. The first line is a plain sentence that starts with a verb and says what changed; release notes are generated from first lines by `git-cliff` with [.github/cliff.toml](.github/cliff.toml), which groups them by that leading verb (Add, Fix, Remove, Document, Test and so on).
- When the commit implements a specification task, end the message with the feature tag, for example `[spec:monorepo-layout]`.
- State what was verified and only what actually ran.

## Toolchains

- Go: the version in `go.mod` (the release build refuses any other)
- C17 for the LayerX kernel (`-std=c17` in the root `Makefile`)
- Rust 1.91.1 (`rust-toolchain.toml`)
- Solidity 0.8.27 for the root Foundry project (`foundry.toml`)
- Node.js for the TypeScript apps and SDKs; each workspace declares its own package manager

The development container under [.devcontainer/](.devcontainer/) provides a ready environment. Replay qualification needs further compilers and runners listed in [docs/QUALIFICATION.md](docs/QUALIFICATION.md).

## Gates

Run the narrowest target that covers your change while you work, then the gate the task names.

```sh
make build             # LayerX kernel library
make test              # LayerX kernel tests
make test-contracts    # Foundry tests of the root contracts
make paxeer-build      # paxd chain node
make paxeer-test       # paxd unit tests
make paxeer-lint       # golangci-lint over the chain node
make public-audit      # publication audit: pinned actions, required files, secrets
make specgen-lint      # generated rule files match spec/workflow.kvx
```

`scripts/ci/layout-check.sh` fails while any file or configuration names a path that has moved. The pre-commit hooks in `.pre-commit-config.yaml` check whitespace, Markdown, YAML, `cargo fmt` and `forge fmt`. Moved trees keep their own targets, such as `make agent-build`, `make human-build` and `make bridge-test-evm`.

`make ci`, `make paxeer-ci` and `make monorepo-ci` are aggregate gates that include sanitizer and reproducibility runs; they run once per wave or at release rather than per change. Arithmetic, replay, recovery, settlement and cross-architecture changes have extra gates in [docs/QUALIFICATION.md](docs/QUALIFICATION.md).

## Pull requests

- Branch from `main` and keep the change narrow.
- Complete [the pull request template](.github/pull_request_template.md): the specification task or requirement, the compatibility impact, and every command you ran with its exit code.
- Call out any gate you could not run and why.
- Pull requests run the GitHub Actions workflows under `.github/workflows/`, including CodeQL analysis and an offline check of relative Markdown links.

Reviewers may ask for negative tests, replay vectors or migration analysis when a change crosses a trust boundary. A passing local run is not authorization to deploy contracts, run validators or handle real assets.

## Rules that are never relaxed

- Do not relax an assertion, loosen a bound, widen a type, add a silent fallback, skip or delete a test, or disable a check to make something pass. If the code and a check disagree, leave both intact and explain the conflict.
- Do not add stub, mock, placeholder or fake implementations to satisfy tests. Test real code paths with real types.
- Preserve canonical byte encodings and result-code assignments.
- Use checked fixed-width integer arithmetic on consensus-critical paths. Floating point is prohibited in state transition functions.
- Keep `402LXP` as the only balance writer in the kernel. Modules emit validated transfer sets.
- Keep network access, wall-clock time, filesystem enumeration order and database iteration order out of deterministic state transitions.
- Reject malformed or non-canonical input explicitly and transactionally.
- Never commit credentials, keys or `.env` files.
