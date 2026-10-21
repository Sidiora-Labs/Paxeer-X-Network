# Governance

Paxeer X Network is developed by Sidiora Labs. The Sidiora Labs core team (`@Sidiora-Labs/core`) owns every path in [.github/CODEOWNERS](.github/CODEOWNERS) and maintains the repository.

## Maintainers

The core team:

- decides what is built, by writing and accepting specifications under [`spec/`](spec/)
- reviews and merges changes to `main`
- cuts releases as described in [RELEASE.md](RELEASE.md)
- handles private security reports as described in [SECURITY.md](SECURITY.md)
- enforces [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)

A merged contribution does not make its author a maintainer. The core team adds and removes maintainers.

## How decisions are made

Planned work is written down before it is built. Each feature is a specification under `spec/<feature>/spec.kvx` with requirements, acceptance criteria and a task list ordered in dependency waves; [spec/workflow.kvx](spec/workflow.kvx) defines how tasks are claimed, qualified and closed and names the active feature. A design decision is recorded in the specification, and a change that contradicts an accepted requirement changes the specification first.

Reproducible defects are fixed directly against the issue that reports them.

The record of a decision is the merged specification and code on `main`.

## Releases and chain upgrades

A release of the chain node is a version in `tools/release/version.json` and a matching `paxeer-network/vX.Y.Z` tag; [RELEASE.md](RELEASE.md) gives the procedure. A release that changes chain state carries a named upgrade plan, and the network adopts it through an on-chain governance proposal.

## Security fixes

Suspected vulnerabilities do not go through public issues or pull requests. They follow [SECURITY.md](SECURITY.md): private report, private fix, then publication once the fix is available.
