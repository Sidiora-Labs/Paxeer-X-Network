# x-websearch

The Paxeer X Network web search sidecar. It serves `GET /health` and `GET /readyz` for free, `GET /search` and `GET /fetch` behind the 402LXP payment gate, and `GET /content/<digest>` unpaid. Its configuration is one JSON file passed as `--config <path>`. The loader refuses unknown fields, missing fields, placeholders and key material, and names the field it refused.

## Kernel relay configuration

The kernel relay starts only when the configuration has a `kernel` object. It follows program web requests through the gateway and answers each one. It exchanges signatures with the peer attestors, and once the registered threshold agrees, it posts the observation to the kernel web module (activity type `0x000B0001`) with `lx_sendActivity`.

| Key | Meaning |
| --- | --- |
| `kernel.endpoint` | The gateway agent RPC URL. The relay reads `lx_getProgramEvents` there and posts observation activities to it. Plain http is accepted only for loopback or `localhost`. |
| `kernel.poll_interval_ms` | The time from the start of one relay step to the start of the next, from 1 to 60000. It is also the base of the retry backoff. |
| `kernel.topics` | The program event topics the relay reads. Each one must be a program web request topic (`PAXEERX_WEB_REQUEST_V1`), and none may repeat. |
| `kernel.submitter_did` | The registered receiver DID that observation activities are posted as. |
| `kernel.fee_limit` | The fee limit of each observation activity, as a canonical decimal string greater than zero. |
| `kernel_network_id` | The kernel network id that observations and activities are bound to. |
| `gateway.authorization_file` | Required for the relay. It must be an absolute path to an owner-only file (mode `0600`, one link, at most 256 bytes) holding `LayerX-Key <id>:lxp_live_<64 lowercase hex>`. The file must not be named `.env`. |
| `gateway.sequencer_public_key` | The sequencer key that every observation receipt is verified against before the request counts as committed. |
| `evm.endpoint` | The chain the registered attestor set and threshold are read from on every step. |
| `peers` | The peer attestors whose program signatures are collected. |

The relay reads its keys from the files that these environment variables name, never from the configuration:

- `X_WEBSEARCH_ATTESTOR_KEY_FILE`: the secp256k1 attestor key that signs each answer.
- `X_WEBSEARCH_RECEIVER_KEY_FILE`: the Ed25519 key of `kernel.submitter_did`, which signs each observation activity.

### State, retries and lag

- `<data_dir>/kernel/kernel-cursor` holds the next event sequence. `<data_dir>/kernel/kernel-relay.json` journals every request with its stage, and every signed activity before it is sent. A restart resumes from the journal and looks up a sent activity's receipt before it sends that activity again, so no observation is posted twice.
- A failed try waits `poll_interval_ms` doubled for each failure in a row, capped at 300000 ms. A failed try is a transient fetch, search, store or signing failure, an unavailable gateway, or an activity whose outcome is still unknown. A request that the payload itself makes unanswerable is recorded as refused and is not retried.
- An activity is signed again at the next account sequence only when both of these hold: one full validity window has passed since it expired, and its account sequence is still unused.
- `GET /readyz` carries `relay_lag`, which has these fields:
  - `pending`: journalled requests that are not finished.
  - `oldest_pending_ms`: how long the oldest of those has waited.
  - `retrying`: how many are waiting out a backoff.
  - `next_sequence`: the next event sequence the relay reads.
