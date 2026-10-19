# layerx-agentd

The non-authoritative LayerX agent daemon. This crate is the daemon library;
the `layerx-agentd` binary is built by the `layerx-agentd-host` crate from
`src/main.rs`. See the [agent workspace README](../../README.md) for the
boundary it sits behind and how to run its tests.

## Modes

`LAYERX_AGENT_MODE` selects `full` (also the default when unset) or
`human-owner`. Other values, including an empty value, are refused.

Full mode starts the Human owner and the verified Programs reader. Its Programs
probe, admission journal, protected sequencer history, replica identity and
credential checks remain required.

Human-owner mode starts only the Human owner. All `LAYERX_AGENT_HUMAN_*` LNI,
peer, authority, session key, store, socket and limit inputs remain required.
It does not read the Programs probe, admission journal, sequencer history or
node reader inputs, and it refuses `LAYERX_AGENT_MCP_BINDING_ROOT`, which needs
full mode. It reuses `LAYERX_AGENT_PROGRAM_LISTEN` (IPv4 loopback only) and
`LAYERX_AGENT_PROGRAM_BEARER_TOKEN` (at least 32 bytes, distinct from
`LAYERX_AGENT_HUMAN_AUTHORITY_BEARER`) for authenticated `GET /healthz`. Other
routes return 404. Health returns 200 with `{"ready":true}` only while the
owner is running, after a fresh node LNI handshake and an authority registry
read for every configured peer. Dependency failure returns 503 with
`{"ready":false}`; owner termination stops the process. Health requests keep
the daemon's 16 KiB header bound and 10-second socket timeouts.

## Outbound TLS

`LAYERX_AGENT_HUMAN_AUTHORITY_CA_DER` is required in both modes and names a file
containing the DER-encoded CA certificate for the Human authority HTTPS
endpoint. `LAYERX_AGENT_AUTHORITY_CA_DER` is additionally required in full mode
and names the DER CA file trusted by both Programs read endpoints. Empty or
unreadable CA files are refused.

Outbound clients use `ureq` with the rustls provider and trust only the
supplied CA (`src/outbound_tls.rs`); hostname verification stays enabled.
Human authority endpoints require HTTPS. Programs endpoints require HTTPS or
plain HTTP on a loopback host.

TLS tests (`src/outbound_tls/tests.rs`) generate temporary CA and server
certificates with the `openssl` crate and run a real loopback server through
`native-tls`; both are dev-dependencies of this crate only.
