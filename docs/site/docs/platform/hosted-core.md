# Hosted core

`layerx-platform-core` is the crate; `layerx-core-boundary` is the binary
(`services/platform/hosted/core/Cargo.toml:2`,
`services/platform/hosted/core/Cargo.toml:8-14`). It is the TLS HTTP boundary in front
of `layerxd`. `platform_core` binds the core and admin listeners separately
(`services/platform/hosted/core/src/main.rs:2025-2032`). `config` parses their defaults
as `0.0.0.0:9443` and `0.0.0.0:9444`
(`services/platform/hosted/core/src/main.rs:279-280`). Both planes speak TLS. The core
plane loads `LAYERX_CORE_TLS_CERT_DER` and `LAYERX_CORE_TLS_KEY_DER`. When
`LAYERX_CORE_CLIENT_CA_DER` is set, that plane installs a client-certificate
verifier that also `allow_unauthenticated`; when the variable is unset, the
core plane uses no client authentication
(`services/platform/hosted/core/src/main.rs:193-222, 248-252, 281-285`). The admin plane
loads `LAYERX_CORE_ADMIN_TLS_CERT_DER` and
`LAYERX_CORE_ADMIN_TLS_KEY_DER` and always uses no client authentication
(`services/platform/hosted/core/src/main.rs:286-290`). Core routes run on the core port.
Admin routes run on the admin port. The public JSON-RPC method list and payment
transcript are documented in [Public payment API](public-api.md).

The image is `layerx-core-boundary` from `docker/platform-core/Dockerfile`. The build produces `/usr/local/bin/layerx-core-boundary` and the runtime image sets `ENTRYPOINT` to that binary (`docker/platform-core/Dockerfile:5`, `docker/platform-core/Dockerfile:9-11`). The Dockerfile creates user `4020:4020` and `USER 4020:4020` (`docker/platform-core/Dockerfile:8-10`). The node StatefulSet runs the same binary as container `core-boundary` with `runAsUser: 4021` and `runAsGroup: 4020` (`services/platform/hosted/node/deployment.yaml:106-119`). Those two identities differ.

In that pod the container waits until `/run/layerx/node/core.env` is readable, sources it, requires `LAYERX_CORE_SEQUENCER_ID` and `LAYERX_CORE_TREASURY_ASSET`, then `exec`s the binary (`services/platform/hosted/node/deployment.yaml:109-118`). It listens on `0.0.0.0:9443` and `0.0.0.0:9444`, connects LNI at `/run/layerx/node/layerxd.lni.sock`, talks to the node program listener at `http://127.0.0.1:9401` and the replica at `http://127.0.0.1:9402`, and uses supervisor socket `/run/layerx/node/supervisor.sock` (`services/platform/hosted/node/deployment.yaml:121-138`). HTTPS probes are `/readyz` and `/livez` on `core-tls` (`services/platform/hosted/node/deployment.yaml:140-141`). Service `layerx-pending-core` targets container port 9443; Service `layerx-pending-core-admin` targets 9444 (`services/platform/hosted/node/deployment.yaml:243-251`). The sibling `layerxd` container binds `--program-port 9401` and `--replica-port 9402` (`services/platform/hosted/node/deployment.yaml:55-58`).

The hosted beta treats core as dependency `Core` and the admin listener as `CoreAdmin` (`services/platform/hosted/testnet/src/main.rs:162-166`, `services/platform/hosted/testnet/src/main.rs:191-192`). `LAYERX_TESTNET_CORE_URL` and `LAYERX_TESTNET_CORE_ADMIN_URL` are required (`services/platform/hosted/testnet/src/main.rs:831-838`). The core probe is `GET /readyz` and accepts only HTTP 200 (`services/platform/hosted/testnet/src/main.rs:1032-1036`, `services/platform/hosted/testnet/src/main.rs:1099`). The admin probe is a TLS handshake with no HTTP request (`services/platform/hosted/testnet/src/main.rs:1040-1044`, `services/platform/hosted/testnet/src/main.rs:1100`).

`make platform-test-core` runs `cargo test --offline --manifest-path services/platform/Cargo.toml --locked -p layerx-platform-core` (`services/platform/Makefile.inc:144-145`). `platform-test-trusted-boundary` depends on that target (`services/platform/Makefile.inc:159`).

---

## Config keys

| Variable | Role |
| --- | --- |
| `LAYERX_CORE_LISTEN` | Core TLS bind; default `0.0.0.0:9443` (`services/platform/hosted/core/src/main.rs:169-174, 279`) |
| `LAYERX_CORE_ADMIN_LISTEN` | Admin TLS bind; default `0.0.0.0:9444` (`services/platform/hosted/core/src/main.rs:169-174, 280`) |
| `LAYERX_CORE_TLS_CERT_DER` | Core server certificate DER (`services/platform/hosted/core/src/main.rs:193-222, 281-285`) |
| `LAYERX_CORE_TLS_KEY_DER` | Core PKCS#8 key DER (`services/platform/hosted/core/src/main.rs:193-222, 281-285`) |
| `LAYERX_CORE_ADMIN_TLS_CERT_DER` | Admin server certificate DER (`services/platform/hosted/core/src/main.rs:286-290`) |
| `LAYERX_CORE_ADMIN_TLS_KEY_DER` | Admin PKCS#8 key DER (`services/platform/hosted/core/src/main.rs:286-290`) |
| `LAYERX_CORE_CLIENT_CA_DER` | Optional core client CA DER; unset means no client authentication (`services/platform/hosted/core/src/main.rs:193-219, 248-252`) |
| `LAYERX_CORE_NETWORK_ID` | Required non-zero `u32` (`services/platform/hosted/core/src/main.rs:253-257`) |
| `LAYERX_CORE_LNI_SOCKET` | Unix socket to `layerxd` LNI (`services/platform/hosted/core/src/main.rs:291`) |
| `LAYERX_CORE_NODE_URL` | Loopback `http://` host:port with no path (`services/platform/hosted/core/src/main.rs:225-245, 293`) |
| `LAYERX_CORE_NODE_BEARER_TOKEN_FILE` | Bearer secret for node HTTP (`services/platform/hosted/core/src/main.rs:294`) |
| `LAYERX_CORE_REPLICA_URL` | Parsed by the same `parse_node_url` as the node URL; the error strings in that function name `LAYERX_CORE_NODE_URL` (`services/platform/hosted/core/src/main.rs:225-245, 298`) |
| `LAYERX_CORE_REPLICA_BEARER_TOKEN_FILE` | Bearer secret for replica HTTP (`services/platform/hosted/core/src/main.rs:299`) |
| `LAYERX_CORE_ADMIN_TOKEN_FILE` | Admin `Authorization: Bearer` secret (`services/platform/hosted/core/src/main.rs:300`) |
| `LAYERX_CORE_TREASURY_SIGNER_SOCKET` | Absolute unix socket of the treasury signer; connected at startup, the treasury public key and DID come from its `public-key` answer and startup fails before either listener opens when it is unset or unreachable (`services/platform/hosted/core/src/main.rs:259-261`, `services/platform/hosted/core/src/main.rs:302`, `services/platform/hosted/core/src/custody.rs`) |
| `LAYERX_CORE_TREASURY_ASSET` | Non-zero 32-byte hex asset id (`services/platform/hosted/core/src/main.rs:261-267`) |
| `LAYERX_CORE_SEQUENCER_ID` | 32-byte hex sequencer id (`services/platform/hosted/core/src/main.rs:268-271`) |
| `LAYERX_CORE_SUPERVISOR_SOCKET` | Unix socket for admin reset (`services/platform/hosted/core/src/main.rs:305`) |
| `LAYERX_CORE_STATE_DIR` | Creates `journal/` mode `0o700` (`services/platform/hosted/core/src/main.rs:272-277, 306`) |
| `LAYERX_CORE_FEE_LIMIT` | SEND fee limit; default `1000` (`services/platform/hosted/core/src/main.rs:307`) |
| `LAYERX_CORE_RECEIPT_DEADLINE_MS` | Receipt poll deadline; default `15000` (`services/platform/hosted/core/src/main.rs:308-311`) |

Secret files are read, trailing CR/LF stripped, and refused when empty or longer than 4096 bytes (`services/platform/hosted/core/src/main.rs:152-162`). The treasury seed is not one of them: the core reads no treasury key material at all and asks the signer socket for every signature, bounding each reply at 4096 bytes and verifying the answer under the public key the socket announced at connect (`services/platform/hosted/core/src/custody.rs:25`, `services/platform/hosted/core/src/custody.rs:127-165`, `services/platform/hosted/core/src/custody.rs:218-243`). Node and replica URLs must be plaintext `http://` on `127.0.0.1` or `localhost` with a port and no path (`services/platform/hosted/core/src/main.rs:225-245`).

---

## Public routes

Served on the core plane (`services/platform/hosted/core/src/main.rs:1158-1256`). Query strings are refused with `400 invalid_request` except on relay targets (`services/platform/hosted/core/src/main.rs:1180-1182`, `services/platform/hosted/core/src/main.rs:1173-1178`).

| Method | Path | Input | Success |
| --- | --- | --- | --- |
| `GET` | `/livez` | none | `200` `{"live":true}` (`services/platform/hosted/core/src/main.rs:1184`) |
| `GET` | `/readyz` | none | readiness document below (`services/platform/hosted/core/src/main.rs:1185`, `services/platform/hosted/core/src/main.rs:974-1017`) |
| `GET` | `/v1/sequencer` | none | `200` handshake `network_id`, `sequencer_public_key`, `chain_head_sequence`, `latest_sealed_batch` (`services/platform/hosted/core/src/main.rs:1020-1035`, `services/platform/hosted/core/src/main.rs:1186`) |
| `POST` | `/v1/activities` | `Content-Type: application/octet-stream` body, or `application/json` `{"activity":"<hex>"}`; optional `Idempotency-Key` | LNI submit then receipt (`services/platform/hosted/core/src/main.rs:851-892`, `services/platform/hosted/core/src/main.rs:1187-1226`) |
| `POST` | `/v1/programs/call` | same body types; `Idempotency-Key` required and must equal hex of the activity idempotency key | submit as Programs ordinal `3` (`services/platform/hosted/core/src/main.rs:851-853`, `services/platform/hosted/core/src/main.rs:876-888`) |
| `POST` | `/v1/programs/deploy` | `application/octet-stream` only; `Idempotency-Key` required and matching | Programs ordinal `1` (`services/platform/hosted/core/src/program_lifecycle.rs:7-13`, `services/platform/hosted/core/src/main.rs:1194-1222`) |
| `POST` | `/v1/programs/upgrade` | same as deploy | Programs ordinal `2` (`services/platform/hosted/core/src/program_lifecycle.rs:10`, `services/platform/hosted/core/src/main.rs:1194-1222`) |
| `POST` | `/v1/programs/wind-down` | same as deploy | Programs ordinal `7` (`services/platform/hosted/core/src/program_lifecycle.rs:11`, `services/platform/hosted/core/src/main.rs:1194-1222`) |
| `POST` | `/v1/programs/simulate` | octet-stream or JSON `{"activity":"<hex>"}` | simulation document; `committed` is `false` (`services/platform/hosted/core/src/main.rs:829-848`, `services/platform/hosted/core/src/main.rs:1228`, `services/platform/hosted/core/src/main.rs:804-826`) |
| `GET` | `/v1/state` | none | wrapped relay of `/v1/protocol/account-state/head` (`services/platform/hosted/core/src/main.rs:1229-1238`) |
| `GET` | `/v1/accounts/{id}` or `/balance` | nonzero 32-byte hex account id | account snapshot with canonical value and native proof material |
| `GET` | `/v1/dids/{did}/sequence` | valid DID | authenticated identity sequence snapshot |
| `GET` | `/v1/dids/{did}/accounts` | valid DID | complete bounded LNI minor-5 account enumeration requested at `VerificationLevel::STATE_PROVEN`; each entry carries the `verification` label its own proof achieved and the listing carries the weakest of them; an empty or unproven listing is `503 did_account_listing_unavailable` (`services/platform/hosted/core/src/public_reads.rs:134-217`) |
| `GET` | `/v1/assets` or `/v1/assets/{id}` | no selector or one nonzero Asset id | complete bounded list or one version-3 record; a record whose `symbol` is outside 1..=16 ASCII bytes is `502 invalid_asset_symbol` (`services/platform/hosted/core/src/public_reads.rs:62-131`) |
| `POST` | `/v1/fees/estimate` | JSON `canonical_hex` | committed-schedule estimate or typed unavailable refusal |
| `GET` | `/v1/node-info` | none | protocol/network handshake and current heads |
| `GET` | `/v1/batches/{number}` | canonical nonzero decimal batch number | signed batch header |
| `GET` | `/v1/checkpoints/{id}` | nonzero 32-byte hex checkpoint id | checkpoint evidence |
| `GET` | `/v1/proofs/{activity|receipt}/{id}` | nonzero activity id | canonical value, proof, and signed header |
| `GET` | `/v1/proofs/account/{activity}/{account}` | two nonzero 32-byte hex ids | exact verified native account proof |
| `GET` | `/v1/receipts/<hex>` | 32-byte lowercase hex activity id | `{"activity_id","receipt"}` (`services/platform/hosted/core/src/main.rs:895-915`, `services/platform/hosted/core/src/main.rs:1239-1240`) |
| `GET` | `/v1/programs/receipts/by-idempotency/<key>` | 64 lowercase hex chars | node lookup then sequencer-signature check (`services/platform/hosted/core/src/main.rs:918-971`, `services/platform/hosted/core/src/main.rs:1161-1168`) |
| `GET` | `/v1/protocol/account-state/head` | optional query | node HTTP relay (`services/platform/hosted/core/src/main.rs:1139-1147`, `services/platform/hosted/core/src/main.rs:1092-1120`) |
| `GET` | `/v1/programs/account-state/changes` | optional query | node HTTP relay (`services/platform/hosted/core/src/main.rs:1142-1143`) |
| `GET` | `/v1/receipts/<id>/account-state` | `id` 64 hex chars | node HTTP relay (`services/platform/hosted/core/src/main.rs:1144-1145`) |
| `GET` | `/v1/programs/<id>/account-state` | `id` 64 hex chars | node HTTP relay (`services/platform/hosted/core/src/main.rs:1144-1145`) |
| `GET` | `/v1/batches/<id>/receipt-authority` | `id` 64 hex chars; query allowed | node HTTP relay (`services/platform/hosted/core/src/main.rs:1145`) |

`POST` activity bodies that are empty or longer than `1_048_576` bytes are `400 invalid_argument` (`services/platform/hosted/core/src/main.rs:873-875`). Simulate uses bound `LNI_FRAME_BYTES` `1_212_416` (`services/platform/hosted/core/src/main.rs:46`, `services/platform/hosted/core/src/main.rs:843-845`). Wrong method on the named public paths is `405 method_not_allowed`; anything else is `404 not_found` (`services/platform/hosted/core/src/main.rs:1242-1255`). Deploy/upgrade/wind-down without `application/octet-stream` is `415 activity_content_type_required` (`services/platform/hosted/core/src/main.rs:1195-1200`, `services/platform/hosted/core/src/main.rs:854-858`).

`unavailable_capability` lists `/v1/programs/receipts/by-idempotency/` as a 503 path (`services/platform/hosted/core/src/main.rs:1150-1156`, `services/platform/hosted/core/src/main.rs:1170-1171`). `core_route` matches that prefix first and serves `GET` (`services/platform/hosted/core/src/main.rs:1161-1168`). Those two facts stand together.

---

## Admin routes

Served on the admin plane (`services/platform/hosted/core/src/main.rs:1649-1692`). `GET /livez` and `GET /readyz` run before bearer checks (`services/platform/hosted/core/src/main.rs:1650-1653`). Every other admin request requires `Authorization: Bearer` equal in length to the admin token with constant-time compare (`services/platform/hosted/core/src/main.rs:1409-1417`, `services/platform/hosted/core/src/main.rs:1655-1656`).

| Method | Path | Input | Success |
| --- | --- | --- | --- |
| `GET` | `/livez` | none | `200` `{"live":true}` (`services/platform/hosted/core/src/main.rs:1651`) |
| `GET` | `/readyz` | none | same readiness as the core plane (`services/platform/hosted/core/src/main.rs:1652`) |
| `POST` | `/admin/v1/testnet/fund` | `Content-Type: application/json`; `Idempotency-Key`; body `funding_id`, `did`, `public_key`, `amount` with `deny_unknown_fields` (`services/platform/hosted/core/src/main.rs:107-114`, `services/platform/hosted/core/src/main.rs:1669-1683`) | `200` `{"funding_id","state":"funded","transaction_id"}` or `202` pending (`services/platform/hosted/core/src/main.rs:1544-1565`) |
| `POST` | `/admin/v1/testnet/reset` | JSON `{}`; `Idempotency-Key` (`services/platform/hosted/core/src/main.rs:1685-1689`) | `200` `{"state":"reset","reset_id"}` (`services/platform/hosted/core/src/main.rs:1615-1617`) |

Missing bearer is `401 unauthorized` (`services/platform/hosted/core/src/main.rs:1655-1656`). Query strings are `400 invalid_request` (`services/platform/hosted/core/src/main.rs:1658-1659`). Non-POST on the two admin paths is `405 method_not_allowed`; other paths are `404 not_found` (`services/platform/hosted/core/src/main.rs:1661-1667`). Missing JSON content type is `400 content_type_required`. Missing or invalid `Idempotency-Key` is `400 idempotency_key_required` or `400 invalid_idempotency_key` (`services/platform/hosted/core/src/main.rs:1669-1676`). Reset body other than `{}` is `400 invalid_argument` (`services/platform/hosted/core/src/main.rs:1686-1688`).

---

## LNI connection and submission

The client connects to `LAYERX_CORE_LNI_SOCKET` with interface `Version::V1_4`, protocol `STATE_COMMITMENT_PROTOCOL_VERSION`, and the configured network id (`services/platform/hosted/core/src/main.rs:482-502`). Frame limit is `1_212_416` bytes, deadline 5s, reconnect attempts 1 (`services/platform/hosted/core/src/main.rs:472-479`, `services/platform/hosted/core/src/main.rs:494-499`). Connection failure is `503 node_unavailable` with `retry_after_seconds` 5 (`services/platform/hosted/core/src/main.rs:692-695`).

`submit_activity` decodes signed envelopes against the authenticated submission registry. Asset register (1), account_open (4), send (5), receive (6), grant_issue (7), grant_revoke (8), mint (10), and burn (11) are admitted. Pause/unpause (2/3) are excluded, and reserved Asset ordinal 9 returns `422 asset_ordinal_reserved`. Programs deploy (1), upgrade (2), call (3), transfer (5), account registration (6), and wind-down (7) are admitted. Decode failure is `400 invalid_activity`; strict Asset PAY decoding failure is `400 invalid_asset_activity`. A program route that does not match module, ordinal, and protocol 3 is `400 program_route_mismatch`. Program calls must decode as `NativeProgramCall` (`400 invalid_program_call`); Programs transfer/account operations use strict PAY decoding plus their account validator (`400 invalid_program_account_operation`); lifecycle payloads use `program_lifecycle::validate` (`400 invalid_program_lifecycle`). Every admitted type then follows the same signer, fee-limit, native submission, receipt verification, and finality path as SEND. Authority must be a 32-byte key or 33-byte key with prefix `1`.

Submit is `client.submit_signed` (`services/platform/hosted/core/src/main.rs:696-709`):

| `SubmitError` | HTTP |
| --- | --- |
| `CoreRefusal` | `422 submission_refused` (`services/platform/hosted/core/src/main.rs:699-704`) |
| `UnavailableCapability` | `503 capability_unavailable` retry 30 (`services/platform/hosted/core/src/main.rs:706`) |
| `Disconnected` | `503 node_unavailable` retry 5 (`services/platform/hosted/core/src/main.rs:707`) |
| any other | `400 invalid_activity` (`services/platform/hosted/core/src/main.rs:708`) |

The activity id comes from `Submission::Acknowledged` or `Submission::Unknown` (`services/platform/hosted/core/src/main.rs:711-714`). The boundary then polls receipt lookup until `LAYERX_CORE_RECEIPT_DEADLINE_MS`. A verified receipt is `200` with `state` `completed` or `refused`. Timeout is `202` with `state` `pending`. Lookup error is `503 receipt_unavailable` retry 5 (`services/platform/hosted/core/src/main.rs:715-738`). Program ordinals 1, 2, and 7 return `terminal_payload` and `call_graph` as empty strings (`services/platform/hosted/core/src/main.rs:717-722`).

---

## Receipt fact lookup

Raw LNI uses message tag `5` for the request, `6` for the response, `25` for error (`services/platform/hosted/core/src/main.rs:50-52`, `services/platform/hosted/core/src/main.rs:514-560`). Missing `Capability::ReceiptLookup` is an error string that becomes `503 node_unavailable` on the HTTP receipt route (`services/platform/hosted/core/src/main.rs:520-521`, `services/platform/hosted/core/src/main.rs:911-913`). Selector is byte `1` plus the 32-byte activity id (`services/platform/hosted/core/src/main.rs:523-525`). Empty payload is "not found"; HTTP maps that to `404 not_found` (`services/platform/hosted/core/src/main.rs:557-558`, `services/platform/hosted/core/src/main.rs:910`). Uppercase hex in the path is `400 invalid_argument` (`services/platform/hosted/core/src/main.rs:896-901`).

`receipt_facts` decodes a protocol receipt. Module 9 operation 0 uses `program_lifecycle::verify_receipt`; every other receipt uses `verify_outcome` (`services/platform/hosted/core/src/main.rs:563-600`, `services/platform/hosted/core/src/program_lifecycle.rs:69-94`). The HTTP receipt route returns hex of the raw lookup bytes without that verification (`services/platform/hosted/core/src/main.rs:906-909`).

Idempotency receipt lookup GETs the node at `/v1/programs/receipts/by-idempotency/<key>` with the node bearer (`services/platform/hosted/core/src/main.rs:932-936`). Node `404` is `404 receipt_not_found`. Other node statuses are `503 receipt_unavailable` retry 5. Invalid JSON, hex, sequencer signature, protocol version other than 3, module other than 9, module version other than 4, or activity-id mismatch is `502 receipt_invalid` (`services/platform/hosted/core/src/main.rs:937-970`).

---

## Readiness

`GET /readyz` (`services/platform/hosted/core/src/main.rs:974-1017`):

1. LNI `Client::connect` and handshake. Failure is `503 node_unavailable` retry 5 (`services/platform/hosted/core/src/main.rs:975-976`, `services/platform/hosted/core/src/main.rs:1013-1016`).
2. Replica `GET /v1/batches/<64 zeros>/receipt-authority?receipt_digest=<64 zeros>` must return `200` or `404`; otherwise `503 replica_unavailable` retry 5 (`services/platform/hosted/core/src/main.rs:977-983`).
3. `journal_lock` must be acquired; poison is `503 journal_unavailable` retry 5 (`services/platform/hosted/core/src/main.rs:985-986`).
4. `journal_write` of `journal/ready.json` with `sync_all` on the file and the parent directory. Write failure is `503 journal_unavailable` retry 5 (`services/platform/hosted/core/src/main.rs:988-999`, `services/platform/hosted/core/src/main.rs:1298-1316`).

Success is `200` with `ready`, `network_id` (handshake, decimal string), `wire_version` `"3"`, `synchronous_receipts` `true`, `state_snapshot` `true` (`services/platform/hosted/core/src/main.rs:49`, `services/platform/hosted/core/src/main.rs:1001-1010`).

---

## Simulation

`POST /v1/programs/simulate` decodes a signed Programs ordinal 3 call (`services/platform/hosted/core/src/main.rs:742-753`). Other activity types are `400 not_program_call`. LNI `client.simulate` (`services/platform/hosted/core/src/main.rs:761-785`):

| `SimulateError` | HTTP |
| --- | --- |
| `CoreRefusal` with `class == 3` | `503 capability_unavailable` retry 30 (`services/platform/hosted/core/src/main.rs:764-770`) |
| `CoreRefusal` otherwise | `422 simulation_refused` (`services/platform/hosted/core/src/main.rs:771-772`) |
| `UnavailableCapability` or `InterfaceVersion` | `503 capability_unavailable` retry 30 (`services/platform/hosted/core/src/main.rs:775-776`) |
| `Disconnected` or `Transport` | `503 node_unavailable` retry 5 (`services/platform/hosted/core/src/main.rs:778-779`) |
| `MalformedRequest` | `400 invalid_activity` (`services/platform/hosted/core/src/main.rs:781`) |
| any other | `503 node_unavailable` retry 5 (`services/platform/hosted/core/src/main.rs:782-784`) |

Activity-id mismatch, sequencer-signature failure, or missing protocol receipt is `503 node_unavailable` (`services/platform/hosted/core/src/main.rs:788-798`). Success sets `committed` to `false` on the result and on `simulation_evidence` (`services/platform/hosted/core/src/main.rs:804-825`).

---

## State reads and registry

Relay GETs the node with `Authorization: Bearer` from `LAYERX_CORE_NODE_BEARER_TOKEN_FILE` (`services/platform/hosted/core/src/main.rs:1080-1089`). HTTP `200`, `404`, and `503` JSON bodies are returned as-is; any other node status or non-JSON body is `503 node_unavailable` retry 5 (`services/platform/hosted/core/src/main.rs:1098-1119`). `/v1/state` wraps a `200` body in the `{ok,result,trace}` envelope (`services/platform/hosted/core/src/main.rs:1123-1132`).

These paths are not relayed. They return `503 capability_unavailable` with `retry_after_seconds` 3600 (`services/platform/hosted/core/src/main.rs:1150-1156`, `services/platform/hosted/core/src/main.rs:1170-1171`):

| Path |
| --- |
| `/v1/accounts` |
| `/v1/programs/registry` |
| `/v1/programs/registry/` prefix |
| `/v1/programs/activities/` prefix |

---

## Typed capability refusals

| Path or call | Code | Status on core plane | Retry seconds |
| --- | --- | --- | --- |
| `/v1/accounts`, `/v1/programs/registry`, `/v1/programs/registry/*`, `/v1/programs/activities/*` | `capability_unavailable` | 503 | 3600 (`services/platform/hosted/core/src/main.rs:1170-1171`) |
| `SubmitError::UnavailableCapability` | `capability_unavailable` | 503 | 30 (`services/platform/hosted/core/src/main.rs:706`) |
| `SimulateError::UnavailableCapability`, `InterfaceVersion`, or `CoreRefusal` class 3 | `capability_unavailable` | 503 | 30 (`services/platform/hosted/core/src/main.rs:769-776`) |
| `ReadError::UnavailableCapability` on treasury account | `capability_unavailable` | 503 before admin remap | 30 (`services/platform/hosted/core/src/main.rs:1688`) |

---

## Admin treasury SEND

Library path: `build_send` compiles an owner-authorised Asset SEND (ordinal 5)
with `layerx-intents`, signs the envelope, and returns canonical bytes. It
delegates to `build_send_with_identity_sequence`, which takes the envelope
actor sequence as its own argument
(`services/platform/hosted/core/src/lib.rs:110-219`). Source and destination accounts are
`agent:<did>:main` (`services/platform/hosted/core/src/lib.rs:53-61`). The treasury DID is
`did:layerx:<public key hex>` (`services/platform/hosted/core/src/lib.rs:331-334`). Amount
0 and expiry not after `not_before` are construction errors
(`services/platform/hosted/core/src/lib.rs:124-129`). The SEND payload carries
`SendRequest::account_sequence`, the source account's sequence
(`services/platform/hosted/core/src/lib.rs:24-35`); the envelope carries the identity
sequence passed separately, and `build_send` passes the account sequence for
both. The envelope signature is produced through
`layerx_crypto::disclosure::bind` and `layerx_crypto::signer::sign_disclosed`
with a `LocalSigner`; a disclosure refusal, a signer refusal, or a signer that
does not answer synchronously is a construction error
(`services/platform/hosted/core/src/lib.rs:221-239`). The separate owner authorization
over the SEND payload is unchanged (`services/platform/hosted/core/src/lib.rs:241-262`).

HTTP path `fund` / `fund_send` (`services/platform/hosted/core/src/main.rs:1705-1816`):

1. JSON `FundingCommand`: `funding_id`, `did`, `public_key`, `amount` (`services/platform/hosted/core/src/main.rs:107-114`). Parse failure is `400 invalid_argument`.
2. Validation: `funding_id` matches `valid_key`; `did` starts with `did:` and length ≤ 512; `public_key` is 64 hex chars; `did` equals `did:layerx:` plus lowercase public key; `amount != 0`; `did` is not the treasury DID; `main_account` succeeds. Failure is `400 invalid_argument` (`services/platform/hosted/core/src/main.rs:1709-1719`).
3. LNI connect; failure `503 node_unavailable` retry 5
   (`services/platform/hosted/core/src/main.rs:1726-1729`).
4. The treasury identity sequence is read from the node's preparation snapshot
   for the treasury DID (`Client::preparation_state`, correlation 3) and used
   as the envelope actor sequence
   (`services/platform/hosted/core/src/main.rs:1731-1739`). DID derivation failure is
   `503 treasury_unavailable` retry 60; a preparation read failure is
   `503 treasury_identity_unavailable` retry 5. The signer never substitutes a
   guessed identity sequence.
5. `treasury_sequence` reads and decodes the treasury main account at
   `VerificationLevel::STATE_PROVEN` and requires `treasury_asset` balance ≥
   amount (`services/platform/hosted/core/src/main.rs:1669-1703`). A native refusal or
   decode failure is `422 treasury_account_unavailable` retry 60; unavailable
   capability is `503 capability_unavailable` retry 30; insufficient balance is
   `422 insufficient_treasury_balance` retry 60; account derivation failure is
   `503 treasury_unavailable` retry 60.
6. `build_send_with_identity_sequence` uses the identity sequence for the
   envelope, that account sequence for the SEND payload, an idempotency key
   equal to SHA-256 of `layerx-core-fund\0` plus the HTTP key, a validity
   interval from 60 seconds before `now` through 300 seconds after it, and the
   configured fee limit (`services/platform/hosted/core/src/main.rs:1662-1667`,
   `services/platform/hosted/core/src/main.rs:1740-1761`). Construction failure is
   `422 send_unbuildable`.
7. `submit_signed` uses the treasury public key
   (`services/platform/hosted/core/src/main.rs:1762-1780`). Same submission mapping as
   public activities, except other errors are `422 send_unbuildable`.
8. Receipt: result 0 → `200` `state: funded`; non-zero → `422 send_refused`;
   timeout → `202` `state: pending`; lookup error → `503 receipt_unavailable`
   (`services/platform/hosted/core/src/main.rs:1787-1816`).

Durable idempotency is the on-disk journal under `LAYERX_CORE_STATE_DIR/journal`. The file name is SHA-256 of `scope`, a 0 byte, and the idempotency key (`services/platform/hosted/core/src/main.rs:1267-1276`). The request digest is SHA-256 of method, path, and body (`services/platform/hosted/core/src/main.rs:1278-1285`). Writes use a `0o600` temp file, `sync_all`, `rename`, then directory `sync_all` (`services/platform/hosted/core/src/main.rs:1298-1316`). Same digest replays the stored status and body. A different digest for the same key is `409 idempotency_conflict` (`services/platform/hosted/core/src/main.rs:1340-1365`). Journal lock poison or I/O is `503 journal_unavailable` retry 5 (`services/platform/hosted/core/src/main.rs:1335-1336`, `services/platform/hosted/core/src/main.rs:1367-1369`, `services/platform/hosted/core/src/main.rs:1390-1391`, `services/platform/hosted/core/src/main.rs:1403-1404`). Scope `fund` and `reset` first persist `409 outcome_unknown` then overwrite with the real outcome (`services/platform/hosted/core/src/main.rs:1372-1406`). Admin work also takes `admin_lock`; poison is `503 admin_unavailable` retry 5 (`services/platform/hosted/core/src/main.rs:1912-1915`).

Reset writes `reset\n` on the supervisor Unix socket and parses JSON (`services/platform/hosted/core/src/main.rs:1590-1639`). Connect/I/O failure is `503 supervisor_unavailable` retry 30. A `state: reset` reply with `reset_id` is `200`. A typed supervisor `error` is `503` with that code. Any other reply is `503 reset_failed` retry 30.

`admin_result` rewrites any status ≥ 500 to `422` for fund and reset outcomes (`services/platform/hosted/core/src/main.rs:1642-1646`, `services/platform/hosted/core/src/main.rs:1682-1689`). `handle_connection` then rewrites any remaining admin status ≥ 500 to `422` except `/readyz` and `/livez` (`services/platform/hosted/core/src/main.rs:1710-1714`). Core-plane journal and lock failures stay `503` (`services/platform/hosted/core/src/main.rs:985-999`, `services/platform/hosted/core/src/main.rs:1335-1336`). Admin-plane journal, lock, node, capability, receipt, and supervisor failures that start as 5xx are returned as `422` (`services/platform/hosted/core/src/main.rs:1642-1646`, `services/platform/hosted/core/src/main.rs:1710-1714`; `services/platform/hosted/core/tests/boundary.rs:1710-1731`).

---

## Failure HTTP statuses

Core-plane refusals keep the status in this table. Admin-plane rows that start as 5xx are rewritten to 422 as cited above.

| Code | Status | Retry | Source |
| --- | --- | --- | --- |
| `invalid_request` | 400 | never | parse / query (`services/platform/hosted/core/src/main.rs:410-424`, `services/platform/hosted/core/src/main.rs:1180-1181`, `services/platform/hosted/core/src/main.rs:1706-1707`) |
| `invalid_argument` | 400 | never | body, hex, funding fields (`services/platform/hosted/core/src/main.rs:833-844`, `services/platform/hosted/core/src/main.rs:1472-1484`) |
| `content_type_required` | 400 | never | (`services/platform/hosted/core/src/main.rs:841`, `services/platform/hosted/core/src/main.rs:871`, `services/platform/hosted/core/src/main.rs:1669-1670`) |
| `activity_content_type_required` | 415 | never | (`services/platform/hosted/core/src/main.rs:854-858`, `services/platform/hosted/core/src/main.rs:1195-1200`) |
| `idempotency_key_required` | 400 | never | (`services/platform/hosted/core/src/main.rs:876-878`, `services/platform/hosted/core/src/main.rs:1672-1673`) |
| `invalid_idempotency_key` | 400 | never | (`services/platform/hosted/core/src/main.rs:929-930`, `services/platform/hosted/core/src/main.rs:1332-1333`, `services/platform/hosted/core/src/main.rs:1675-1676`) |
| `invalid_activity` | 400 | never | (`services/platform/hosted/core/src/main.rs:672`, `services/platform/hosted/core/src/main.rs:708`) |
| `program_route_mismatch` | 400 | never | (`services/platform/hosted/core/src/main.rs:673-678`, `services/platform/hosted/core/src/main.rs:1211-1215`) |
| `invalid_program_call` | 400 | never | (`services/platform/hosted/core/src/main.rs:683-684`) |
| `invalid_program_lifecycle` | 400 | never | (`services/platform/hosted/core/src/main.rs:686-687`) |
| `authority_unsupported` | 400 | never | (`services/platform/hosted/core/src/main.rs:690-691`) |
| `not_program_call` | 400 | never | (`services/platform/hosted/core/src/main.rs:747-750`) |
| `protocol_idempotency_mismatch` | 409 | never | (`services/platform/hosted/core/src/main.rs:886-887`) |
| `idempotency_conflict` | 409 | never | (`services/platform/hosted/core/src/main.rs:1365`) |
| `outcome_unknown` | 409 | 5 | journal placeholder for fund/reset (`services/platform/hosted/core/src/main.rs:1377-1378`) |
| `method_not_allowed` | 405 | never | (`services/platform/hosted/core/src/main.rs:1242-1254`, `services/platform/hosted/core/src/main.rs:1661-1664`) |
| `not_found` | 404 | never | (`services/platform/hosted/core/src/main.rs:1255`, `services/platform/hosted/core/src/main.rs:910`) |
| `unauthorized` | 401 | never | admin bearer (`services/platform/hosted/core/src/main.rs:1655-1656`) |
| `submission_refused` | 422 | never | (`services/platform/hosted/core/src/main.rs:699-704`, `services/platform/hosted/core/src/main.rs:1523-1528`) |
| `simulation_refused` | 422 | never | (`services/platform/hosted/core/src/main.rs:771-772`) |
| `send_unbuildable` | 422 | never | (`services/platform/hosted/core/src/main.rs:1514-1516`, `services/platform/hosted/core/src/main.rs:1532-1534`) |
| `send_refused` | 422 | never | (`services/platform/hosted/core/src/main.rs:1552-1557`) |
| `treasury_account_unavailable` | 422 | 60 | (`services/platform/hosted/core/src/main.rs:1447-1461`) |
| `insufficient_treasury_balance` | 422 | 60 | (`services/platform/hosted/core/src/main.rs:1465-1466`) |
| `node_unavailable` | 503 core; 422 admin except `/readyz` `/livez` | 5 | (`services/platform/hosted/core/src/main.rs:692-695`, `services/platform/hosted/core/src/main.rs:1642-1646`, `services/platform/hosted/core/src/main.rs:1710-1714`) |
| `capability_unavailable` | 503 core; 422 admin | 30 or 3600 | see capability table |
| `registry_unavailable` | 503 | 5 | (`services/platform/hosted/core/src/main.rs:669-670`) |
| `receipt_unavailable` | 503 core; 422 admin | 5 | (`services/platform/hosted/core/src/main.rs:736-737`, `services/platform/hosted/core/src/main.rs:1568-1569`) |
| `journal_unavailable` | 503 core; 422 admin | 5 | (`services/platform/hosted/core/src/main.rs:985-999`, `services/platform/hosted/core/src/main.rs:1335-1336`) |
| `replica_unavailable` | 503 | 5 | (`services/platform/hosted/core/src/main.rs:982-983`) |
| `receipt_not_found` | 404 | never | (`services/platform/hosted/core/src/main.rs:935`) |
| `receipt_invalid` | 502 | never | (`services/platform/hosted/core/src/main.rs:939-969`) |
| `admin_unavailable` | 503 then 422 | 5 | (`services/platform/hosted/core/src/main.rs:1678-1679`) |
| `supervisor_unavailable` | 503 then 422 | 30 | (`services/platform/hosted/core/src/main.rs:1604-1606`) |
| `reset_failed` | 503 then 422 | 30 | (`services/platform/hosted/core/src/main.rs:1635-1637`) |
| `treasury_unavailable` | 503 then 422 | 60 | (`services/platform/hosted/core/src/main.rs:1436-1437`) |
| `treasury_identity_unavailable` | 503 then 422 | 5 | preparation snapshot for the treasury identity sequence (`services/platform/hosted/core/src/main.rs:1733-1738`) |
| `treasury_signer_unavailable` | 503 then 422 | 5 | treasury signer refused, answered out of contract or was unreachable while signing the funding SEND (`services/platform/hosted/core/src/main.rs:1670-1681`, `services/platform/hosted/core/src/main.rs:1772`) |
| `invalid_asset_symbol` | 502 | never | committed asset record with a symbol outside 1..=16 ASCII bytes (`services/platform/hosted/core/src/public_reads.rs:62-73`) |

---

## Real-node boundary tests

`services/platform/hosted/core/tests/boundary.rs` drives the real `layerx-core-boundary` binary over TLS against a real `layerxd` sequencer and authority replica started from `build/bin` (`services/platform/hosted/core/tests/boundary.rs:1-3`, `services/platform/hosted/core/tests/boundary.rs:1372-1376`). The harness requires root so `layerxd` runs as uid `65534` (`services/platform/hosted/core/tests/boundary.rs:1223-1226`, `services/platform/hosted/core/tests/boundary.rs:43-44`).

`cluster_artifacts` returns a `TestState` guard together with the daemon, genesis-builder and migration paths; every `Cluster` owns that guard, and dropping it removes the harness root under the process temporary directory (`services/platform/hosted/core/tests/boundary.rs:1239-1247`, `services/platform/hosted/core/tests/boundary.rs:1982-2017`). Setting `LAYERX_TEST_RETAIN_STATE` keeps the tree and prints its path instead. A removal failure panics unless the test is already panicking, so a failing test keeps its evidence and a passing one cannot leak state (`services/platform/hosted/core/tests/boundary.rs:1266-1286`).

`boundary_refuses_typed_and_journals_while_the_daemon_is_down` starts the boundary without a sequencer (`services/platform/hosted/core/tests/boundary.rs:1548-1592`). It proves `/livez` is 200 on both planes; `/readyz`, `/v1/sequencer`, `/v1/state`, account-state head, and receipt lookup are `503 node_unavailable`; `/v1/programs/registry` is `503 capability_unavailable`; lifecycle GETs are `405`; lifecycle POST JSON is `415`; lifecycle POST octet-stream without idempotency is `400`; a signed SEND POST is `503 node_unavailable`; a client certificate chained to the configured CA is accepted on `/livez`; a certificate from another CA fails the TLS handshake (`services/platform/hosted/core/tests/boundary.rs:1459-1556`, `services/platform/hosted/core/tests/boundary.rs:1734-1754`). Admin refusals without a node are `401` without bearer, `400` without content type or idempotency key, `422 node_unavailable` for fund (5xx rewritten), `422 supervisor_unavailable` for reset (`services/platform/hosted/core/tests/boundary.rs:1757-1826`).

`boundary_serves_the_real_sequencer_over_the_lni` requires `/readyz` 200 with `ready`, string `network_id`, `wire_version` `"3"`, `synchronous_receipts`, `state_snapshot` (`services/platform/hosted/core/tests/boundary.rs:1594-1604`, `services/platform/hosted/core/tests/boundary.rs:1436-1456`). It reads `/v1/sequencer` and `/v1/state` as 200, missing account-state as 404, missing receipt as `404 not_found` (`services/platform/hosted/core/tests/boundary.rs:1829-1858`). Simulation of a program call returns 200 with `committed: false`, does not advance chain head, and does not consume account sequence (`services/platform/hosted/core/tests/boundary.rs:636-696`).

That test then posts `/admin/v1/testnet/fund` against a fresh genesis. The status is `422` because the tested treasury is unfunded: `treasury_account_unavailable` or `insufficient_treasury_balance` (`services/platform/hosted/core/tests/boundary.rs:1611-1626`). The same idempotency key replays that body; chain head does not move (`services/platform/hosted/core/tests/boundary.rs:1627-1637`). A public `/v1/activities` SEND from that treasury is either `200` with `state: refused` or `422 submission_refused`; it is not completed (`services/platform/hosted/core/tests/boundary.rs:1639-1676`). Stopping the sequencer makes `/readyz` `503 node_unavailable` (`services/platform/hosted/core/tests/boundary.rs:1685-1688`).

`establish_receipt_head` first submits an unaffordable SEND (`fee_limit` 1000) and requires `422 submission_refused` without advancing the chain, with stderr containing `submission refused class 4 result -602` (`services/platform/hosted/core/tests/boundary.rs:2375-2414`). A later SEND with `fee_limit` 0 returns `200` `state: refused` and is fetchable at `/v1/receipts/<id>` (`services/platform/hosted/core/tests/boundary.rs:2416-2447`).

`readiness_requires_replica_and_writable_journal` proves `/readyz` 200, then renaming the journal directory yields `503 journal_unavailable`, restore yields 200, and stopping the replica yields `503 replica_unavailable` (`services/platform/hosted/core/tests/boundary.rs:1693-1706`).

`admin_dependency_refusals_are_four_xx_and_survive_restart` journals `422 node_unavailable` for fund, replays it after process restart, and maps missing supervisor to `422 supervisor_unavailable` (`services/platform/hosted/core/tests/boundary.rs:1709-1731`).

`supervisor_reset_rebuilds_genesis_and_replays_once` posts reset, gets `200` `state: reset`, rebuilds genesis, increments generation, and replays the same idempotency key without a second reset (`services/platform/hosted/core/tests/boundary.rs:2285-2333`).

`lifecycle_routes_submit_real_signed_activities_and_verify_state_receipts` submits deploy, upgrade, and wind-down over the real node, verifies state receipts, and replays idempotent 200 bodies (`services/platform/hosted/core/tests/boundary.rs:375-472`, `services/platform/hosted/core/tests/boundary.rs:475-544`).

`services/platform/hosted/core/tests/send.rs` asserts `build_send_with_identity_sequence`
embeds native `agent:<did>:main` account ids in the payload and authorization,
binds the envelope to the identity sequence (11) independently of the payload's
account sequence (0), and that the signed envelope starts with protocol bytes
`[0, 3]` (`services/platform/hosted/core/tests/send.rs:1-37`).

[Home](../index.md)
