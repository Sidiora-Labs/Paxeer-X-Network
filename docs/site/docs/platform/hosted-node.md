# Hosted node

The hosted node is the in-cluster sequencer pod. Image
`ghcr.io/sidiora-labs/layerx-node:0.1.0` builds `layerxd`,
`layerx-genesis-build`, and `layerxctl`, copies
`bootstrap.sh` and `supervisor.sh`, and sets `USER 4020:4020`
with `ENTRYPOINT` `/opt/layerx/supervisor.sh`
(`docker/platform-node/Dockerfile:10-11`;
`docker/platform-node/Dockerfile:20-27`). The binary
`layerxd` is `cmd/layerxd/`; its CLI is `--check-config`,
`--serve`, or `--authority-replica` plus a configuration path
(`cmd/layerxd/lxp_daemon_cli.c:8-16`). Any other argv is
`LXP_ERR_NON_CANONICAL`.

StatefulSet `layerx-node` in `layerx-testnet` has one replica
and headless Service `layerx-node` on port `9443`
(`services/platform/hosted/node/deployment.yaml:12-21`). Pod labels are
`app: layerx-node` and `layerx-plane: trusted-boundary`
(`services/platform/hosted/node/deployment.yaml:24`). There is no Ingress
object in that manifest.

This page covers bootstrap, the supervisor, `layerxd`
listeners, and the node StatefulSet. The colocated TLS
boundaries are [Hosted core](hosted-core.md),
[Hosted authority](hosted-authority.md), and
[Hosted agent boundary](agent-boundary.md). The Human
API, providers, KMS and owner run in this same pod; see
[Hosted Human](../human/hosted.md). The loopback
Paxeer relay is documented with [Paxeer boundary](../concepts/paxeer-boundary.md).

---

## Cluster shape

Containers in the pod (`services/platform/hosted/node/deployment.yaml:106-784`):

| Container | Image | Role |
| --- | --- | --- |
| `layerxd` | `layerx-node:0.1.0` | Sequencer supervisor: bootstrap, `layerxd --serve`, supervisor Unix socket |
| `layerxd-authority` | `layerx-node:0.1.0` | Replica supervisor: `layerxd --authority-replica` |
| `treasury-signer` | `layerx-node:0.1.0` | Treasury signer `signer.py` on Unix socket `/run/layerx/node/treasury-signer.sock` as UID `4020` (`services/platform/hosted/node/deployment.yaml:156-178`) |
| `guarantor-1`, `guarantor-2` | `layerx-node:0.1.0` | Persistent checkpoint-authority processes |
| `paxeer-relay` | `layerx-node:0.1.0` | `socat` TCP `127.0.0.1:18545` to `OPENSSL:paxeer-boundary.layerx-testnet.svc.cluster.local:9443` (`services/platform/hosted/node/deployment.yaml:239-248`) |
| `core-boundary` | `layerx-core-boundary:0.1.0` | TLS core/admin planes; see [Hosted core](hosted-core.md) |
| `receipt-authority` | `layerx-receipt-authority:0.1.0` | TLS receipt authority; see [Hosted authority](hosted-authority.md) |
| `agent-boundary` | `layerx-agent-boundary:0.1.0` | TLS agent boundary; see [Hosted agent boundary](agent-boundary.md) |
| `agentd-boundary` | `layerx-node:0.1.0` | `socat` TLS listener on container port `9454` (`agentd-tls`) to loopback agentd `127.0.0.1:9453` as UID `4021` (`services/platform/hosted/node/deployment.yaml:396-417`) |
| `human` | `layerx-human:0.1.0` | Human HTTPS API on container port `9447`; Service `layerx-human` maps `9443` to `human-https` (`services/platform/hosted/node/deployment.yaml:418-434`; `services/platform/hosted/human/deployment.yaml:1-12`) |
| `components` | `layerx-human:0.1.0` | Privileged Human component graph (`services/platform/hosted/node/deployment.yaml:479`) |
| `human-identity`, `human-security`, `human-movement` | `layerx-human:0.1.0` | Provider binaries as UID `4020` (`services/platform/hosted/node/deployment.yaml:534, 585, 636`) |
| `human-kms` | `layerx-human:0.1.0` | Human KMS as UID `4026` (`services/platform/hosted/node/deployment.yaml:685`) |
| `human-owner` | `layerx-human:0.1.0` | Human owner as UID `4021` (`services/platform/hosted/node/deployment.yaml:738`) |

Services (`services/platform/hosted/node/deployment.yaml:12-14`;
`services/platform/hosted/node/deployment.yaml:867-884`;
`services/platform/hosted/node/deployment.yaml:924-927`;
`services/platform/hosted/human/deployment.yaml:1-12`):

| Service | Type | Port | Target |
| --- | --- | --- | --- |
| `layerx-node` | headless | `9443` | `core-tls` |
| `layerx-pending-core` | ClusterIP | `9443` | `core-tls` (`9443`) |
| `layerx-pending-core-admin` | ClusterIP | `9444` | `core-admin-tls` (`9444`) |
| `layerx-receipt-authority` | ClusterIP | `9443` | `authority-tls` (`9445`) |
| `layerx-agent-boundary` | ClusterIP | `9443` | `agent-tls` (`9446`) |
| `layerx-agentd` | ClusterIP | `9443` | `agentd-tls` (`9454`) (`services/platform/hosted/node/deployment.yaml:924-927`) |
| `layerx-human` | ClusterIP | `9443` | `human-https` (`9447`) (`services/platform/hosted/human/deployment.yaml:1-12`) |

Loopback listeners inside the pod are not Services:
program HTTP `127.0.0.1:9401`, replica HTTP `127.0.0.1:9402`,
LNI Unix socket `/run/layerx/node/layerxd.lni.sock`, supervisor
Unix socket `/run/layerx/node/supervisor.sock`
(`services/platform/hosted/node/deployment.yaml:55-58`;
`services/platform/hosted/node/deployment.yaml:130-131`;
`services/platform/hosted/node/bootstrap.sh:358-359`).

`platform-hosted-topology-check` loads
`services/platform/hosted/node/deployment.yaml` among its default
manifests (`services/platform/hosted/tests/topology-check.sh:18`;
`services/platform/hosted/tests/topology-check.sh:84`). Beta-cluster
apply of `node.yaml` must create the trusted-boundary Services
including `layerx-pending-core`,
`layerx-pending-core-admin`, `layerx-receipt-authority`, and
`layerx-agent-boundary`
(`services/platform/hosted/tests/beta-cluster.sh:91`;
`services/platform/hosted/tests/beta-cluster.sh:862-864`). Qualification
binds `LAYERX_QUALIFICATION_NODE_URL` to Service
`layerx-pending-core` and `LAYERX_QUALIFICATION_AGENT_URL` to
Service `layerx-agent-boundary`
(`services/platform/hosted/tests/beta-cluster.sh:1134-1137`).

---

## Bootstrap artefacts

`bootstrap.sh` writes a signed genesis and the files the
supervisor sources (`services/platform/hosted/node/bootstrap.sh:4-8`;
`services/platform/hosted/node/bootstrap.sh:73-78`). Required flags:
`--data-dir`, `--run-dir`, `--network-id`, `--sequencer-key`,
`--treasury-key` (`services/platform/hosted/node/bootstrap.sh:14-26`;
`services/platform/hosted/node/bootstrap.sh:191-195`). `--data-dir` and
`--run-dir` must differ, and the run directory must not sit
inside the data directory
(`services/platform/hosted/node/bootstrap.sh:347-348`). The data
directory is created mode `0700` and must be empty unless
`--force` (`services/platform/hosted/node/bootstrap.sh:343`;
`services/platform/hosted/node/bootstrap.sh:352-354`). The run directory
is mode `0750` with group `--lni-gid`
(`services/platform/hosted/node/bootstrap.sh:355-357`).

Outputs under the data directory
(`services/platform/hosted/node/bootstrap.sh:73-78`):

| Path | Contents |
| --- | --- |
| `genesis/genesis.manifest` | Signed genesis manifest from `layerx-genesis-build` |
| `genesis/00000000000000000000.lxs` | Genesis snapshot |
| `genesis/paxeer-registration-request.lxrr` | LXRR v1, length `73` (`services/platform/hosted/node/bootstrap.sh:416`) |
| `genesis/paxeer-deployment-descriptor.lxgd` | LXGD descriptor |
| `genesis/genesis.registration` | LXGR v1 self-registration, length `82`, written only when `--custody-profile` is absent (`services/platform/hosted/node/bootstrap.sh:420-434`) |
| `genesis/genesis-request.lxgb` | The LXGB v2 genesis request `layerx-genesis-build` consumed, moved out of the bootstrap scratch directory once the genesis artefacts exist so the request a genesis was built from stays auditable (`services/platform/hosted/node/bootstrap.sh:504-505`) |
| `identities.txt` | One line `{treasury-did-hex}:{treasury-public-key}:0` (`services/platform/hosted/node/bootstrap.sh:438`) |
| `checkpoints/` | Empty checkpoint directory |
| `logs/` | `program-feed.log`, `canonical.log`, `receipt-authority.log`, `batch.log`, `evidence.log` |
| `replica/receipt-authority.log` | Replica log |
| `secrets/program-token` | Program-listener bearer |
| `secrets/replica-token` | Replica-listener bearer |
| `secrets/treasury-key.hex` | Treasury seed hex |
| `secrets/guarantor-key.pem` | secp256k1 guarantor private key |
| `sequencer.conf` / `replica.conf` | Eight-line `layerxd` configurations; both pass `layerxd --check-config` (`services/platform/hosted/node/bootstrap.sh:449-455`) |
| `sequencer.env` / `replica.env` | Daemon environments, mode `0600`; `sequencer.env` names the sequencer seed file as `LAYERX_NODE_SEQUENCER_KEY_FILE` and never carries the seed |
| `node.env` | Published node facts, mode `0644` |
| `treasury.json` | Treasury DID, public key, account, asset, genesis balance, network id (`services/platform/hosted/node/bootstrap.sh:543-546`) |

The run directory receives `core.env` with
`LAYERX_CORE_SEQUENCER_ID` and `LAYERX_CORE_TREASURY_ASSET`
(`services/platform/hosted/node/bootstrap.sh:538-541`). Cluster bring-up
waits until `node.env`, the LXGD descriptor, and the LXRR
request are present, then fetches those three files
(`services/platform/hosted/tests/beta-cluster.sh:885-902`).

A non-zero `--treasury-balance` is refused:
`treasury_balance_unsupported`. The genesis manifest admits
only the three system accounts at balance zero
(`services/platform/hosted/node/bootstrap.sh:217-219`). Default balance
is `0`. `--asset` must be 64 lowercase hex and not zero;
default is
`b5a32b12029f8ddfb905f90f280f664b46390de0fc62770fc197dd87b18cd898`,
`sha256("layerx-beta-asset:LXT")`
(`services/platform/hosted/node/bootstrap.sh:29-30`;
`services/platform/hosted/node/bootstrap.sh:146`;
`services/platform/hosted/node/bootstrap.sh:214-216`). ConfigMap
`layerx-node-config` pins that same asset id, network id
`402`, network name `layerx-testnet`, replica id
`6c61796572782d626574612d726563656970742d617574686f726974792d3031`,
and Paxeer relay port `18545`
(`services/platform/hosted/node/deployment.yaml:2-9`). The sequencer
arguments name the metadata suffix as `--genesis-metadata`
`/run/layerx/genesis/metadata.lxgb`, a `subPath` mount of key
`metadata.lxgb` of ConfigMap `layerx-node-genesis-metadata`, so
the bootstrap sees a regular file rather than the ConfigMap
symlink it refuses (`services/platform/hosted/node/deployment.yaml:123-124`;
`services/platform/hosted/node/deployment.yaml:155`;
`services/platform/hosted/node/deployment.yaml:805`). Bring-up publishes
that ConfigMap from the authoritative Asset records before the
node manifest is applied.

`--custody-profile` must name a readable regular file that is
not a symlink and is exactly 223 bytes
(`services/platform/hosted/node/bootstrap.sh:292-297`). When set,
`layerx-genesis-build` receives `--custody-profile` and
bootstrap does not write `genesis.registration`
(`services/platform/hosted/node/bootstrap.sh:405-407`;
`services/platform/hosted/node/bootstrap.sh:423-434`).

---

## Genesis request and guarantor keys

Bootstrap generates a secp256k1 key at
`secrets/guarantor-key.pem`, derives the compressed public key
(33 bytes, prefix `02` or `03`), and sets
`GUARANTOR_ID = sha256("layerx-beta-guarantor:" || public-key-hex)`
(`services/platform/hosted/node/bootstrap.sh:364-368`). Cluster bring-up
requires `LAYERX_NODE_GENESIS_GUARANTOR_ID` as 64 hex and
`LAYERX_NODE_GENESIS_GUARANTOR_PUBLIC_KEY` as that compressed
form (`services/platform/hosted/tests/beta-cluster.sh:903-908`). The
same public key is the Paxeer guarantor signer input
(`services/platform/hosted/tests/beta-cluster.sh:917-923`).

The genesis request is LXGB v2. Its existing fixed body is 395 bytes;
`--genesis-metadata FILE` supplies the required canonical metadata suffix:

- magic `LXGB`, version `2`, protocol `3`
- network id, genesis timestamp milliseconds
- one parameter `parameter-version` = `1`
- one guarantor (id, compressed public key, bond `0`)
- the genesis asset, fee coefficients, and demand coefficients
- Asset record count u16, then each record's u16 byte length and canonical
  version-3 bytes, followed by the named fee schedule's u16 byte length and
  canonical version-2 bytes (all integers big-endian)

Supply the authoritative Asset records, including issuer DID id32, original
salt, cap, pause and supply. The fresh empty genesis builder requires zero
circulating supply and custody issuer kind; it validates the records and
schedule before signing. Do not invent salts for an existing asset. The
metadata file must be kept outside a data directory discarded with `--force`.
Its length must exceed 219 bytes and may not exceed
`16384 - 314 - 81 * certificate_threshold`, the builder limit
`GENESIS_BUILD_REQUEST_MAX_BYTES` less the fixed request body for the
guarantor count the settlement document carries; the bound is checked once
that count is known, before any directory is created
(`services/platform/hosted/node/bootstrap.sh:307-312`;
`cmd/layerx-genesis/lxp_genesis_build_cli.c:18`).
The same suffix is required by `prepare-beta.py --genesis-metadata FILE` and
`tests/bridge/custody_genesis.py --genesis-metadata FILE`. Version-1 decoding
remains available for existing signed artifacts; it cannot restore omitted
metadata.

`layerx-genesis-build` signs that request with the sequencer
seed (`services/platform/hosted/node/bootstrap.sh:400-408`). LXRR bytes
`[9,41)` are `LAYERX_NODE_GENESIS_STATE_ROOT`; the last 32
bytes are `LAYERX_NODE_GENESIS_RECEIPT_STATE_ROOT`
(`services/platform/hosted/node/bootstrap.sh:417-418`). Without a
custody profile, LXGR anchors both registration roots to the
receipt state root (`services/platform/hosted/node/bootstrap.sh:420-432`).

To migrate a post-genesis checkpoint containing the retired
`asset:<asset-id>:issuance` account name, stop the node and run:

```sh
layerx-genesis-build --migrate-snapshot-issuance \
  SOURCE.lxs genesis/genesis.manifest SEQUENCER.key MIGRATED-CHECKPOINTS
```

`MIGRATED-CHECKPOINTS` must not exist. The command verifies the signed genesis
manifest and requires its Ed25519 signer key, verifies the source snapshot and
its legacy state root, renames only retired issuance accounts to the
`module:asset:value:<account-id>` form while preserving their account ids and
all other state, and writes a same-sequence LXS3
checkpoint. Its authorization binds both snapshot digests, both canonical
roots, the prior receipt root, the newly derived receipt root, network id and
rename count. On restore, `layerxd` accepts LXS3 only when that authorization
verifies against the signer in `genesis.manifest`. Preserve and deploy the
matching `.lxi` identity sidecar beside the migrated `.lxs`; the migration
command does not rewrite identity history.

Sequencer and treasury seeds are 32 raw bytes or 64 hex
characters and must yield distinct public keys
(`services/platform/hosted/node/bootstrap.sh:283-315`). Sequencer id is
`sha256("layerx-sequencer:" || public-key-hex)`
(`services/platform/hosted/node/bootstrap.sh:316`). Replica id defaults
to `sha256("layerx-authority-replica:" || public-key-hex)`
(`services/platform/hosted/node/bootstrap.sh:317-321`). The hosted
manifest overrides replica id from ConfigMap
(`services/platform/hosted/node/deployment.yaml:45-46`;
`services/platform/hosted/node/deployment.yaml:68`).

---

## Settlement environment

Five keys bind Paxeer settlement. They are either in the
bootstrap process environment or deferred to
`--settlement-env FILE`
(`services/platform/hosted/node/bootstrap.sh:55-64`;
`services/platform/hosted/node/bootstrap.sh:221-232`). The file form
excludes those five from the environment
(`services/platform/hosted/node/bootstrap.sh:228-229`). Path must be
absolute (`services/platform/hosted/node/bootstrap.sh:227`).

`bootstrap.sh --check-settlement FILE` is the validator
(`services/platform/hosted/node/bootstrap.sh:67-72`;
`services/platform/hosted/node/bootstrap.sh:135-138`). The file must be
a readable regular file of at most 4096 bytes, holding exactly
these `KEY=VALUE` lines with no repeats and no other keys
(`services/platform/hosted/node/bootstrap.sh:110-132`):

| Key | Rule |
| --- | --- |
| `LAYERX_NODE_PAXEER_CHAIN_ID` | Positive decimal `uint64` (`services/platform/hosted/node/bootstrap.sh:94-97`) |
| `LAYERX_NODE_SETTLEMENT_CONTRACT` | the layerxAnchor precompile `0x0000000000000000000000000000000000001014`, the address guarantors sign and bond at (`services/platform/hosted/node/bootstrap.sh` `validate_settlement`) |
| `LAYERX_NODE_CHECKPOINT_REGISTRY` | the same precompile address; checkpoints are submitted to and read back from it |
| `LAYERX_NODE_PAXEER_RPC_ADDRESS` | exactly `127.0.0.1` (`services/platform/hosted/node/bootstrap.sh:106`) |
| `LAYERX_NODE_PAXEER_RPC_PORT` | `1..=65535` (`services/platform/hosted/node/bootstrap.sh:107`) |

The hosted sequencer args pass `--settlement-env`
`/run/layerx/settlement/settlement.env`
(`services/platform/hosted/node/deployment.yaml:63-64`). That path is
ConfigMap `layerx-node-settlement`, optional at apply
(`services/platform/hosted/node/deployment.yaml:229`). Cluster bring-up
writes the five lines after contract deploy, runs
`--check-settlement`, then applies that ConfigMap
(`services/platform/hosted/tests/beta-cluster.sh:953-957`).

When `sequencer.env` names `LAYERX_NODE_SETTLEMENT_ENV`, the
sequencer supervisor waits for the file (log every 30 seconds,
cap `LAYERX_NODE_SETTLEMENT_WAIT_SECONDS`, default `3600`),
validates it with `--check-settlement`, and exports the five
values before `layerxd --serve`
(`services/platform/hosted/node/supervisor.sh:30-34`;
`services/platform/hosted/node/supervisor.sh:161-181`;
`services/platform/hosted/node/supervisor.sh:188-198`).

---

## `node.env` keys

`node.env` is mode `0644`
(`services/platform/hosted/node/bootstrap.sh:509-537`):

| Key | Role |
| --- | --- |
| `LAYERX_NODE_NETWORK_ID` | Decimal network id |
| `LAYERX_NODE_ASSET_ID` | Genesis asset hex |
| `LAYERX_NODE_LNI_SOCKET` | Absolute LNI Unix path |
| `LAYERX_NODE_SUPERVISOR_SOCKET` | Absolute supervisor Unix path |
| `LAYERX_NODE_PROGRAM_URL` | `http://127.0.0.1:{program-port}` |
| `LAYERX_NODE_REPLICA_URL` | `http://127.0.0.1:{replica-port}` |
| `LAYERX_NODE_PROGRAM_BEARER_TOKEN_FILE` | `{data}/secrets/program-token` |
| `LAYERX_NODE_REPLICA_BEARER_TOKEN_FILE` | `{data}/secrets/replica-token` |
| `LAYERX_NODE_SEQUENCER_ID` | Sequencer id hex |
| `LAYERX_NODE_SEQUENCER_PUBLIC_KEY` | Sequencer public key hex |
| `LAYERX_NODE_REPLICA_ID` | Replica id hex |
| `LAYERX_NODE_GENESIS_STATE_ROOT` | From LXRR |
| `LAYERX_NODE_GENESIS_RECEIPT_STATE_ROOT` | From LXRR |
| `LAYERX_NODE_GENESIS_GUARANTOR_ID` | Guarantor id hex |
| `LAYERX_NODE_GENESIS_GUARANTOR_PUBLIC_KEY` | Compressed guarantor public key hex |
| `LAYERX_NODE_GENESIS_GUARANTOR_KEY_FILE` | `{data}/secrets/guarantor-key.pem` |
| `LAYERX_PAXEER_GENESIS_DIR` | `{data}/genesis` |
| `LAYERX_NODE_TREASURY_DID` | `did:layerx:{treasury-public-key}` |
| `LAYERX_NODE_TREASURY_PUBLIC_KEY` | Treasury public key hex |
| `LAYERX_NODE_TREASURY_ACCOUNT` | `agent:{did}:main` |
| `LAYERX_NODE_TREASURY_BALANCE` | Genesis treasury balance (`0`) |
| `LAYERX_NODE_TREASURY_KEY_FILE` | `{data}/secrets/treasury-key.hex` |
| `LAYERX_NODE_SEQUENCER_CONFIG` | `{data}/sequencer.conf` |
| `LAYERX_NODE_REPLICA_CONFIG` | `{data}/replica.conf` |
| `LAYERX_NODE_SEQUENCER_ENV` | `{data}/sequencer.env` |
| `LAYERX_NODE_REPLICA_ENV` | `{data}/replica.env` |

`sequencer.env` additionally carries snapshot, manifest,
registration, identities, logs, history database, the sequencer
id and public key, `LAYERX_NODE_SEQUENCER_KEY_FILE` (the
`--sequencer-key` path, absolute, outside the data directory),
batch range `1`..`18446744073709551615`, replica
address/port/id/token, program address/port/token, LNI socket,
allowed uid/gid, `LAYERX_NODE_LNI_FRAME_BYTES=1212416`, and
`LAYERX_NODE_LNI_DEADLINE_MS=2000`, or
`LAYERX_NODE_SETTLEMENT_ENV` in place of the five settlement
keys (`services/platform/hosted/node/bootstrap.sh:457-494`).

`replica.env` carries replica log, replica id, sequencer id
and public key, batch range, bearer token, and
`LAYERX_AUTHORITY_ADDRESS=127.0.0.1` plus port
(`services/platform/hosted/node/bootstrap.sh:496-506`).

`layerxd --serve` requires
`LAYERX_NODE_LNI_FRAME_BYTES` equal to
`LXP_DAEMON_LNI_MAX_FRAME_BYTES`
(`cmd/layerxd/lxp_daemon_process.c:3804-3810`). Bootstrap
writes `1212416` (`services/platform/hosted/node/bootstrap.sh:492`).
`cmd/layerxd/lni.env.example:23` writes `1146902`. Those two
values differ.

---

## Sequencer seed delivery

The sequencer seed is read by `bootstrap.sh` once, to sign the
genesis request, and is not copied under the data directory:
the temporary signer key under `work/` is deleted after
`layerx-genesis-build` returns and `sequencer.env` records only
the key file path. `--sequencer-key` must name a readable
regular file outside the data directory (also after resolving
symlinks), because `--force` and the supervisor reset discard
that directory.

The supervisor never sources an environment file with
`set -a`. `load_environment` validates every line as a
`LAYERX_*` `KEY=VALUE` pair without control characters, exports
it, and refuses a `LAYERX_NODE_SEQUENCER_PRIVATE_KEY` line.
Before the sequencer supervisor publishes a generation (first
start, restart against a retained data directory, and every
reset) `check_sequencer_environment` refuses a seed line in
`sequencer.env`, reads the seed named by
`LAYERX_NODE_SEQUENCER_KEY_FILE` (32 raw bytes or 64 hex
characters) through a read-only descriptor it opens itself, and
refuses to continue unless the derived public key equals
`LAYERX_NODE_SEQUENCER_PUBLIC_KEY`. When it starts
`layerxd --serve` it reads the seed the same way inside the
subshell that execs the daemon and exports
`LAYERX_NODE_SEQUENCER_PRIVATE_KEY` there only; the daemon
reads that variable (`cmd/layerxd/lxp_daemon_process.c`,
`cmd/layerxd/lxp_daemon_lni.c`). The seed never appears on a
command line, in the supervisor's own environment, in
`node.env`, `replica.env`, `core.env`, or the guarantor
identity directories.

Callers outside the supervisor that start `layerxd --serve`
from a bootstrap-generated `sequencer.env` (the daemon test
scripts under `tests/daemon/`) source
`services/platform/hosted/node/sequencer-env.sh` and call
`layerx_sequencer_environment <sequencer.env>` in the subshell
that execs the daemon instead of `set -a; source`. The helper
exports the `LAYERX_*` lines with the same validation, refuses a
seed line, reads the key file named by
`LAYERX_NODE_SEQUENCER_KEY_FILE` and exports
`LAYERX_NODE_SEQUENCER_PRIVATE_KEY` for the exec'd daemon only.
`tests/daemon/guarantor-integration.py` performs the same
derivation in Python before it launches the daemon. Tools that
only replay the checkpoint log with `sequencer.env` sourced
(`tests/daemon/guarantor-publication-chain.py`) never read the
seed and are unaffected.

Refusals, all exit `1` before a generation is published:
`must not carry LAYERX_NODE_SEQUENCER_PRIVATE_KEY`,
`LAYERX_NODE_SEQUENCER_KEY_FILE missing`, `sequencer key file is
not a regular file`, `sequencer key file must hold 32 raw bytes
or 64 hex characters`, `does not match the bound sequencer
public key`.

The pod keeps `shareProcessNamespace: true`
(`services/platform/hosted/node/deployment.yaml:28`): the LNI peer
credential check refuses a `SO_PEERCRED` pid of `0`, which is
what a sibling container in its own pid namespace reports, so
the boundary containers must share the daemon's pid namespace.
Processes with the daemon's uid in that namespace can therefore
still read the daemon environment; the daemon container is the
only one that mounts `/run/layerx/keys/sequencer.key`.

---

## LNI socket

The production LNI is the sole activity-ingress boundary
(`cmd/layerxd/lni.env.example:1-5`). Path is
`RUN_DIR/layerxd.lni.sock`, length less than 108
(`services/platform/hosted/node/bootstrap.sh:358-360`;
`include/layerx/lxp_daemon.h:36`). The parent is mode `0750`,
owned by the daemon uid with the configured client gid. The
socket is mode `0660`, owned by `layerxd` and that gid
(`cmd/layerxd/lxp_daemon_process.c:3790`;
`cmd/layerxd/lxp_daemon_lni.c:3321-3324`;
`cmd/layerxd/lni.env.example:2-5`). `SO_PEERCRED` requires the
exact configured uid and gid; a mismatch is `LXP_ERR_AUTH_SCOPE`
(`cmd/layerxd/lxp_daemon_lni.c:2975-2991`). The client uid must
differ from the daemon uid
(`services/platform/hosted/node/bootstrap.sh:238`;
`cmd/layerxd/lni.env.example:6`). Hosted values: daemon
`4020:4020`, `--lni-uid 4021`, `--lni-gid 4020`
(`services/platform/hosted/node/deployment.yaml:26`;
`services/platform/hosted/node/deployment.yaml:59-62`). Colocated
`core-boundary`, `receipt-authority`, and `agent-boundary`
run as `4021:4020` so the LNI admits them
(`services/platform/hosted/node/deployment.yaml:119`;
`services/platform/hosted/node/deployment.yaml:155`;
`services/platform/hosted/node/deployment.yaml:196`).

Interface is LNI 1.4 (`cmd/layerxd/lxp_daemon_lni.c:38-39`).
The first frame must be NodeInfo request tag `1` with
correlation `0` and empty payload; otherwise refusal class `2`
`LXP_ERR_AUTH_SCOPE` (`cmd/layerxd/lxp_daemon_lni.c:40-41`;
`cmd/layerxd/lxp_daemon_lni.c:3126-3132`). Later tags
(`cmd/layerxd/lxp_daemon_lni.c:40-60`;
`cmd/layerxd/lxp_daemon_lni.c:3138-3167`):

| Tag | Name |
| --- | --- |
| `1` / `2` | NodeInfo |
| `3` / `4` | Submit |
| `5` / `6` | ReceiptLookup |
| `7` / `8` | AccountRead |
| `12` / `13` | BatchHeader |
| `14` / `15` | Checkpoint |
| `16` / `17` | ProofBundle |
| `25` | Error |
| `26` / `27` | PreparationState |
| `28` / `29` | FinalityEvidenceRegister |
| `30` / `31` | Simulate |

An unknown tag is refusal class `3` `LXP_ERR_MODULE_DISABLED`.
A second NodeInfo is class `1` `LXP_ERR_NON_CANONICAL`.
Sequencer NodeInfo advertises at least
`authenticated_durable_submit`, `batch_header`, `node_info`,
`preparation_state`, `receipt_lookup`, `submit`; evidence,
finalizer, and `simulate` bits depend on the daemon
(`cmd/layerxd/lxp_daemon_lni.c:1311-1334`). [Agentd](../agents/agentd.md)
consumes receipts and batch evidence from this plane; it does
not mint balances.

---

## Supervisor socket

The sequencer supervisor listens on
`RUN_DIR/supervisor.sock` via `socat` `UNIX-LISTEN`, mode
`660`, group `LAYERX_NODE_LNI_ALLOWED_GID`
(`services/platform/hosted/node/supervisor.sh:358-360`). One line per
connection, 10 second read
(`services/platform/hosted/node/supervisor.sh:77-78`):

| Request | Success | Refusal |
| --- | --- | --- |
| `reset` | `{"state":"reset","reset_id":"<16 hex>"}` after stop, discard, bootstrap `--force`, restart (`services/platform/hosted/node/supervisor.sh:80-102`; `services/platform/hosted/node/supervisor.sh:363-387`) | `supervisor_unavailable` retry 5; `reset_failed` retry 30; `reset_timeout` retry 60 |
| `status` | `{"state":"running","generation":N}` or `{"state":"stopped","generation":N}` (`services/platform/hosted/node/supervisor.sh:104-111`) | none |
| any other line | | `unknown_request` retry never (`services/platform/hosted/node/supervisor.sh:113-114`) |

Reset coordinates the replica through
`reset.<id>.stop-replica` / `reset.<id>.replica-stopped`
(`services/platform/hosted/node/supervisor.sh:24-28`;
`services/platform/hosted/node/supervisor.sh:367-373`). A daemon that
exits on its own ends the supervisor with status `1`
(`services/platform/hosted/node/supervisor.sh:36-37`).

---

## Program listener

`layerxd --serve` binds the program listener only on
`127.0.0.1` (`cmd/layerxd/lxp_daemon_listener.c:397-408`).
Hosted port `9401`. `Authorization: Bearer` must match
`LAYERX_NODE_PROGRAM_BEARER_TOKEN` in length and constant-time
compare (`cmd/layerxd/lxp_daemon_protocol.c:134-139`;
`cmd/layerxd/lxp_daemon_protocol.c:888-890`). Duplicate
`Authorization` or a non-`HTTP/1.1` request line is
`LXP_ERR_NON_CANONICAL` (`cmd/layerxd/lxp_daemon_listener.c:154-166`).
GET only (`cmd/layerxd/lxp_daemon_protocol.c:496`).

| Path | Success body |
| --- | --- |
| `GET /v1/protocol/account-state/head` | `current`, `receipt_hex`, `receipt_digest`, `state_root`, `observed_sequence`, `observed_at`, `batch_evidence` (`cmd/layerxd/lxp_daemon_protocol.c:267-282`; `cmd/layerxd/lxp_daemon_protocol.c:300-306`) |
| `GET /v1/receipts/{64 hex}/account-state` | same document for that receipt digest (`cmd/layerxd/lxp_daemon_protocol.c:499-510`) |
| `GET /v1/programs/account-state/changes?after_sequence={decimal}` | `records[]` of `sequence`, `ordinal`, `program_id`, `activity_type`, `event_type`, `receipt_digest`; `complete_through`; `scanned_through_sequence`; `caught_up` (`cmd/layerxd/lxp_daemon_protocol.c:366-389`; `cmd/layerxd/lxp_daemon_protocol.c:546-551`) |
| `GET /v1/programs/{64 hex}/account-state?at={decimal}` | `program_id`, `record_hex`, `record_digest`, `receipt_digest`; `at` must equal the feed head (`cmd/layerxd/lxp_daemon_protocol.c:318-342`; `cmd/layerxd/lxp_daemon_protocol.c:553-564`) |
| `GET /v1/programs/activities/{64 hex}/artifacts?receipt_digest={64 hex}` | `activity_id`, `receipt_digest`, `terminal_payload`, `call_graph` (`cmd/layerxd/lxp_daemon_protocol.c:419-427`; `cmd/layerxd/lxp_daemon_protocol.c:530-544`) |
| `GET /v1/programs/receipts/by-idempotency/{64 lowercase hex}` | `activity_id`, `receipt` (`cmd/layerxd/lxp_daemon_protocol.c:462-466`; `cmd/layerxd/lxp_daemon_protocol.c:514-528`) |
| `GET /v1/batches/{64 hex}/receipt-authority?receipt_digest={64 hex}` | `sequencer_public_key`, `batch_evidence` (`cmd/layerxd/lxp_daemon_protocol.c:481-485`; `cmd/layerxd/lxp_daemon_protocol.c:567-580`) |

HTTP mapping after a valid parse
(`cmd/layerxd/lxp_daemon_protocol.c:888-901`;
`cmd/layerxd/lxp_daemon_listener.c:214-251`):

| Status | Body | Condition |
| --- | --- | --- |
| `200` | route JSON | `LXP_OK` |
| `401` | `{"error":<LXP_ERR_BAD_SIGNATURE>}` | bearer mismatch |
| `404` | `{"error":<LXP_ERR_UNKNOWN_ACTIVITY>}` | unknown path or non-GET |
| `503` | `{"error":<code>}` | any other `lxp_result` |
| `400` | `{"error":"refused"}` | framing failure before `protocol_route` |

Those two error envelopes differ: routed failures are a
numeric `error`; parse failures are the string `refused`.
Missing `changes` query is `LXP_ERR_NON_CANONICAL`
(`cmd/layerxd/lxp_daemon_protocol.c:546-547`). Uppercase hex
in an idempotency key is `LXP_ERR_NON_CANONICAL`
(`cmd/layerxd/lxp_daemon_protocol.c:519-523`). At most four
program-listener connections
(`include/layerx/lxp_daemon.h:30`).

The supervisor treats the sequencer as ready when
`GET /v1/programs/account-state/changes?after_sequence=0`
returns `HTTP/1.1 200` and a page matching that records
schema, and the LNI socket exists
(`services/platform/hosted/node/supervisor.sh:237-268`).

---

## Replica listener

`layerxd --authority-replica` binds only `127.0.0.1`
(`cmd/layerxd/lxp_daemon_authority_replica.c:773-778`). Hosted
port `9402`. Bearer is `Authorization: Bearer` plus
`LAYERX_AUTHORITY_BEARER_TOKEN`, compared constant-time
(`cmd/layerxd/lxp_daemon_authority_replica.c:430-439`).

| Method and path | Success | Refusal |
| --- | --- | --- |
| `POST /v1/receipt-authority/ingest` | `201` `{"authority_replica_id":"<64 hex>"}` (`cmd/layerxd/lxp_daemon_authority_replica.c:501-536`) | `401` without bearer; `503` on append failure |
| `GET /v1/batches/{64 hex}/receipt-authority?receipt_digest={64 hex}` | `200` evidence JSON (`cmd/layerxd/lxp_daemon_authority_replica.c:537-557`) | `401`; `404` on miss or malformed selector |

The supervisor treats the replica as ready on `HTTP/1.1 404`
for the all-zero batch and digest
(`services/platform/hosted/node/supervisor.sh:247-249`). The sequencer
posts ingest to that replica
(`cmd/layerxd/lxp_daemon_authority_replica.c:916`). Program
and replica tokens must differ; program address/port must
differ from replica address/port
(`services/platform/hosted/node/bootstrap.sh:339`;
`cmd/layerxd/lxp_daemon_process.c:3770-3777`).

---

## Mounted paths and env

Sequencer container `layerxd` (`services/platform/hosted/node/deployment.yaml:151-158`):

| Path | Source |
| --- | --- |
| `/var/lib/layerx` | PVC `data` (50Gi) |
| `/run/layerx` | emptyDir Memory 16Mi |
| `/run/layerx/keys/sequencer.key` | Secret `layerx-node-keys` (`layerxd` container only) |
| `/run/layerx/genesis/metadata.lxgb` | ConfigMap `layerx-node-genesis-metadata` key `metadata.lxgb`, `subPath` mount |
| `/run/layerx/tokens/program-token` | Secret `layerx-node-tokens` |
| `/run/layerx/tokens/replica-token` | Secret `layerx-node-tokens` |
| `/run/layerx/settlement/settlement.env` | ConfigMap `layerx-node-settlement` |

Treasury signer container `treasury-signer`
(`services/platform/hosted/node/deployment.yaml:179-181`):

| Path | Source |
| --- | --- |
| `/run/layerx` | emptyDir Memory 16Mi, shared with `layerxd`; carries the signer socket `/run/layerx/node/treasury-signer.sock` |
| `/run/layerx/treasury/treasury.key` | Secret `layerx-node-keys` key `treasury.key` through volume `treasury-material`, read-only (`services/platform/hosted/node/deployment.yaml:805`) |

That is the pod's only mount of treasury key material. The
`core-boundary` container mounts none and signs over
`/run/layerx/node/treasury-signer.sock`, which it takes from
`LAYERX_CORE_TREASURY_SIGNER_SOCKET` in `core.env`
(`services/platform/hosted/node/deployment.yaml:282`;
`services/platform/hosted/core/src/main.rs:259-261`).

Env from ConfigMap `layerx-node-config`:
`LAYERX_NODE_NETWORK_ID`, `LAYERX_NODE_ASSET_ID`,
`LAYERX_NODE_REPLICA_ID`
(`services/platform/hosted/node/deployment.yaml:65-68`).
`paxeer-relay` reads `LAYERX_NODE_PAXEER_RELAY_PORT` from that
ConfigMap and `LAYERX_NODE_PAXEER_BOUNDARY`
`paxeer-boundary.layerx-testnet.svc.cluster.local`, with CA
`/run/layerx/trust/ca.crt`
(`services/platform/hosted/node/deployment.yaml:84-90`;
`services/platform/hosted/node/deployment.yaml:94`).

TLS Secrets for colocated boundaries:
`layerx-pending-core-tls`, `layerx-pending-core-admin-tls`,
`layerx-receipt-authority-tls`, `layerx-agent-boundary-tls`,
`layerx-internal-ca` (`services/platform/hosted/node/deployment.yaml:231-235`).

---

## NetworkPolicy

Ingress `layerx-node-ingress`
(`services/platform/hosted/node/deployment.yaml:266-284`):

| From | Ports |
| --- | --- |
| `app=layerx-testnet-control` | `9443`, `9444`, `9445` |
| `app=layerx-gateway` | `9443`, `9445`, `9446` |
| `app=layerx-program-registry` | `9445`, `9446` |
| `app=layerx-faucet` | `9443` |
| namespace `layerx-developer`, `app=layerx-webhooks` | `9445`, `9446` |
| namespace `layerx-developer`, `app=layerx-dashboard-api` | `9445`, `9446` |

Egress `layerx-node-egress`: DNS `53` and `app=paxeer` TCP
`9443` (`services/platform/hosted/node/deployment.yaml:286-296`).
Loopback program/replica/LNI/supervisor sockets do not cross
the NetworkPolicy.

---

## Tests

`make platform-test-node` syntax-checks `bootstrap.sh`,
`supervisor.sh`, and `node-test.sh`, then runs
`services/platform/hosted/node/tests/node-test.sh`
(`services/platform/Makefile.inc:140-142`). That script requires root
so the LNI client can present a uid other than the daemon
(`services/platform/hosted/node/tests/node-test.sh:7-9`;
`services/platform/hosted/node/tests/node-test.sh:42-45`). It resolves
socat from `LAYERX_TEST_SOCAT_BIN` or, when that is unset, from
`PATH`, refusing with `socat_invalid` or `socat_missing`
otherwise, and passes it to both supervisors as `--socat`
(`services/platform/hosted/node/tests/node-test.sh:35-41`). It builds the
LXGB v2 metadata suffix with `tests/support/lxgb_metadata.py`
over the beta asset and the treasury public key it generates,
keeps that file outside the data directory so the supervisor
reset can bootstrap again from it, and passes `--asset` and
`--genesis-metadata` to the sequencer supervisor
(`services/platform/hosted/node/tests/node-test.sh:91-104`;
`services/platform/hosted/node/tests/node-test.sh:135-137`). It proves:

- `node.env` network id, asset id, treasury public key, run
  directory mode `0750` and LNI gid, LXGR length `82`, treasury
  identity line
  (`services/platform/hosted/node/tests/node-test.sh:152-160`)
- LNI handshake as the client uid: network id, role
  `Sequencer`, sequencer public key, `AccountRead`
  (`services/platform/hosted/node/tests/node-test.sh:163-169`)
- `layerxctl read-state` `evidence=authenticated_node_snapshot`
  (`services/platform/hosted/node/tests/node-test.sh:171-176`)
- treasury balance equals `LAYERX_NODE_TREASURY_BALANCE`
  (`services/platform/hosted/node/tests/node-test.sh:178-183`)
- a signed SEND is `acknowledged` and a repeated canonical
  submit is byte-identical
  (`services/platform/hosted/node/tests/node-test.sh:185-201`)
- supervisor `status` is `running` generation `1`; `reset`
  rebuilds genesis (new manifest inode, a new checkpoint
  directory whose admission journal is back to its 32-byte
  superblock) and generation `2`
  (`services/platform/hosted/node/tests/node-test.sh:420-446`)
- stopping the supervisors removes `supervisor.sock` and
  leaves no `layerxd` processes
  (`services/platform/hosted/node/tests/node-test.sh:227-238`)

[Home](../index.md)
