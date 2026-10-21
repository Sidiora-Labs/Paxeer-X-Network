# Security Policy

Paxeer X Network handles authorization, accounting, custody, bridging and settlement of real value. Treat any defect that can affect value conservation, authority, finality, replay determinism, withdrawal uniqueness, upgrade safety or private key handling as a security issue.

## Report privately

Do not open a public issue, discussion or pull request for a suspected vulnerability. Report it through GitHub private vulnerability reporting: open the repository's Security tab and choose "Report a vulnerability", or go directly to <https://github.com/Sidiora-Labs/Paxeer-X-Network/security/advisories/new>. Only the maintainers see the report.

Include:

- the affected commit and component
- prerequisites and an end-to-end reproduction
- the violated invariant and the plausible impact
- the privileges exploitation needs, such as a validator, governance, attestor or operator key
- any proposed fix or test case

Test only against systems and assets you own or have written permission to test. Do not probe public endpoints, validators, bridges or user data, do not move funds, and do not keep secrets you obtain during research.

## Handling and disclosure

The maintainers confirm receipt in the advisory, assess scope and severity, and work on the fix in a private advisory fork when one is needed. The fix lands on `main`, and when it changes the chain node it ships in a release as described in [RELEASE.md](RELEASE.md). The advisory is published once the fix is available, crediting the reporter unless they ask otherwise. A report found to be out of scope is closed with the reason given in the advisory. Please keep details private until the advisory is published.

There is no bug bounty.

## Supported versions

The supported line is `main` and the current release named in `tools/release/version.json`. Fixes land on `main` and ship in the next chain node release.

## Scope

In scope is the code in this repository, including:

- the Paxeer X chain node and its modules, precompiles, RPC and storage (`node/`, `modules/`, `precompiles/`, `rpc/`, `consensus/`, `storage/`, `engine/` and the other root Go packages)
- the LayerX kernel, its programs and `layerxctl` (`src/`, `include/`, `cmd/`, `programs/`)
- the vendored dependency trees the node builds from (`core/deps/`)
- the Solidity contracts (`contracts/`, `contracts/bridge/`, `contracts/launchpad/`)
- off-chain services, SDKs and applications (`services/`, `sdk/`, `apps/`)

The root directories of the chain node and the kernel move under `core/` in a later release.

Out of scope:

- defects that need a compromised validator, governance or operator key, unless the code fails to enforce a limit it claims to enforce
- denial of service by sheer traffic volume against hosted endpoints
- findings from automated scanners without a demonstrated impact
- defects in an upstream project that do not affect how this repository uses it; report those upstream

A conduct concern that is not a vulnerability follows [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).
