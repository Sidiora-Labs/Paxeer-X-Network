# sid-snapshot

Builds the SID holder list for the v6.11 migration: a JSON array of
`{"address": "0x…", "balance": "<base units>"}` entries, sorted by address,
in the shape of `node/testdata/sid-holders.json`.

```
python3 snapshot.py \
  --source "$EXPLORER_DSN" \
  --source "$OLD_PAXSCAN_DSN_OR_CSV" \
  --rpc "$PAXEER_RPC_URL" \
  --out sid-holders.json
```

- `--source` takes a Postgres DSN (Blockscout `token_transfers` table, needs
  `psycopg2`) or a CSV with columns
  `transaction_hash,log_index,from_address,to_address,amount`. Repeat it for
  each source; transfers are merged by `(transaction_hash, log_index)` and a
  transfer the sources disagree on stops the run.
- `--rpc` reconciles every holder against `balanceOf` on the SID proxy; the
  on-chain value wins and each mismatch is printed to stderr.
- The run prints `holders <count> sum <total>`.

`test.sh` runs the tool over CSV fixtures and a local JSON-RPC responder.
