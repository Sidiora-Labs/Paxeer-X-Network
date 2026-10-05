# Paxeer X Network documentation

This directory holds the documentation of Paxeer X Network: the MkDocs site, the
wiki tree the root [README.md](../README.md) links into, monorepo notes and the
Paxeer X chain RPC references. The hosted documentation is
[docs.paxeer.app](https://docs.paxeer.app/). Normative protocol text lives under
[`spec/`](../spec/), not here.

| Path | Contents |
| --- | --- |
| [site/](site/) | MkDocs Material site (Overview, Concepts, Protocol, Programs, Agents, Human, Platform, Interop, Operators, Reference); preview and build instructions in [site/README.md](site/README.md) |
| [site/build_wiki.py](site/build_wiki.py) | Renders `wiki/` to static HTML under `site/out/` and refuses broken relative links |
| [build/build_wiki.py](build/build_wiki.py) | Older wiki renderer writing to `docs/_site/`, with a `--check` flag |
| [wiki/README.md](wiki/README.md) | About the wiki tree |
| [wiki/Home.md](wiki/Home.md) | Wiki index |
| [wiki/Quickstart.md](wiki/Quickstart.md) | Quickstart |
| [wiki/Protocol.md](wiki/Protocol.md) | Activity envelope and the three rules |
| [wiki/Modules.md](wiki/Modules.md) | Module index; one page per module under `wiki/` |
| [wiki/Fees.md](wiki/Fees.md) | Canonical fee schedule |
| [wiki/Programs.md](wiki/Programs.md) | Programs runtime, ABI, occupancy, LXT-20 |
| [wiki/LNI.md](wiki/LNI.md) | Node interface v1.7 |
| [wiki/AgentApi.md](wiki/AgentApi.md) | Agent daemon contract |
| [wiki/Mcp.md](wiki/Mcp.md) | The 21 MCP tools of `layerx-mcp` |
| [wiki/Roadmap.md](wiki/Roadmap.md) | Beta surface expansion (in development, not shipped) |
| [MONOREPO.md](MONOREPO.md) | Monorepo layout, build boundaries, and release tags |
| [QUALIFICATION.md](QUALIFICATION.md) | Evidence levels layered by risk |
| [readme/](readme/) | Translations of the root README |
| [runbooks/paxeer-x-fork.md](runbooks/paxeer-x-fork.md) | Validator runbook for the Paxeer X upgrade of the chain |
| [paxeer-network.md](paxeer-network.md) | Paxeer X chain overview (EVM chain ID `125`) |
| [paxeer-chain-docs.md](paxeer-chain-docs.md) | Paxeer X chain docs index: Swagger/OpenAPI, EVM JSON-RPC notes |
| [evm_jsonrpc_unsupported.md](evm_jsonrpc_unsupported.md) | EVM JSON-RPC methods that are registered but return a documented error |
| [swagger-ui/](swagger-ui/), [swagger/](swagger/) | Swagger UI assets, and the `statik.go` package that embeds them for `paxd` |
