# Paxeer Frontend Integration Guide

Public REST + WebSocket reference for three backend services:

| Service              | What it serves                                            | Default port  |
| -------------------- | --------------------------------------------------------- | ------------- |
| **Symbol Service**   | Per-symbol price, candles, orderbook, trades, perp data   | one per asset |
| **Markets**          | Cross-symbol market table + platform totals               | `3020`        |
| **Chain Explorer**   | On-chain blocks + transactions stream                     | `4500`        |

All HTTP responses are `application/json` unless noted. All times are UTC.

---

## Table of contents

1. [WebSocket protocol (shared by all services)](#websocket-protocol-shared-by-all-services)
2. [Symbol Service](#symbol-service)
3. [Markets Service](#markets-service)
4. [Chain Explorer Service](#chain-explorer-service)

---

## WebSocket protocol (shared by all services)

Every service exposes its WebSocket on the **same host:port as its HTTP API**.
Connect with a plain WebSocket — no auth, no subprotocol, path `/`.

### Client → Server frames

```json
{ "event": "subscribe",   "topic": "<topic>" }
{ "event": "unsubscribe", "topic": "<topic>" }
{ "event": "ping" }
```

Some snapshot providers accept additional fields on `subscribe`. The only one
currently honored is `limit` on kline topics (see [kline](#klinesymbolinterval)).

### Server → Client frames

#### Control frames

```json
{ "event": "welcome",      "ts": 1747526400000, "snapshotId": "lqz3a7-9f2b1c0d4e5f" }
{ "event": "subscribed",   "topic": "BTC@orderbook", "ts": ..., "snapshotId": "..." }
{ "event": "unsubscribed", "topic": "BTC@orderbook", "ts": ... }
{ "event": "pong",         "ts": ... }
{ "event": "error",        "message": "<code>", "ts": ..., "topic"?: "..." }
```

Possible `error.message` values: `invalid_json`, `missing_topic`,
`unknown_event`, `subscribe_rate_limit`, `too_many_subscriptions`.

#### Data frames

Every data frame on every topic has this envelope:

```json
{
  "topic":      "<topic>",
  "ts":         <ms since epoch>,
  "snapshotId": "<process id>",
  "seq":        <integer>,
  "snapshot":   true | undefined,
  "data":       { ... topic-specific payload ... }
}
```

### Stream consistency contract

- **`snapshotId`** — opaque string, constant for the lifetime of the server
  process. If it changes mid-stream, the server has restarted. Drop local state
  and resubscribe.
- **`seq`** — monotonically increasing integer **per topic**, dense (every
  delta increments by exactly 1).
- **`snapshot: true`** is sent once per subscribe, immediately after the
  `subscribed` ack. It carries the `seq` of the last broadcast at snapshot-build
  time. Apply the snapshot, then accept deltas with `seq > snapshot.seq`.
- **Gap detection** — if `delta.seq > lastSeq + 1`, frames were dropped. Treat
  local state as stale: fetch the matching REST endpoint to resync, then resume
  with deltas where `seq > restSnapshot.seq` once you re-subscribe.

### Connection lifetime

- Server sends WebSocket pings every 30s; respond with pongs (your library
  handles this automatically). Dead sockets are dropped on the next interval.
- Per-socket caps: max 200 subscriptions, max 50 subscribe operations / second.

---

## Symbol Service

One instance per tradable symbol (BTC, ETH, SOL, …). Each instance owns one
symbol — the `<SYMBOL>` token in every topic and path is fixed per instance,
matching the symbol that instance runs.

Base URL: `http(s)://<symbol-host>:<symbol-port>`
WebSocket: `ws(s)://<symbol-host>:<symbol-port>`

### REST — Market data

#### `GET /price`

Current price.

```json
{
  "symbol":    "BTC",
  "price":     77173.0,
  "timestamp": "2026-05-18T00:00:00.000Z",
  "open":      76800.0,
  "high":      77310.0,
  "low":       76590.0
}
```

#### `GET /stats`

Rolling 24h spot statistics.

```json
{
  "symbol":            "BTC",
  "volume_24h":        12345678.9,
  "open_interest":     98765432.1,
  "est_funding_rate":  0.00012,
  "last_funding_rate": 0.00009
}
```

#### `GET /perp_stats`

Same shape as `/stats` plus perp-only fields.

```json
{
  "symbol":            "BTC",
  "volume_24h":        ...,
  "open_interest":     ...,
  "est_funding_rate":  ...,
  "last_funding_rate": ...,
  "mark_price":        77182.4,
  "index_price":       77173.0,
  "basis_bps":         1.21,
  "long_short_ratio":  1.03,
  "liq_volume_24h":    ...
}
```

#### `GET /trades/:symbol?limit=100`

Most recent spot trades for that symbol, newest first.
`limit` is clamped to `[1, 1000]`, default `100`.

```json
{
  "symbol": "BTC",
  "count":  100,
  "trades": [
    { "id": "1747526400000-0", "side": "BUY",  "price": 77175, "size": 0.42, "trade_time": 1747526400123 }
  ]
}
```

#### `GET /perp_trades/:symbol?limit=100`

Most recent perp trades, newest first. Same limits as `/trades`.

```json
{
  "symbol": "BTC",
  "count":  100,
  "trades": [
    {
      "id":          "1747526400000-0",
      "side":        "BUY",
      "price":       77182.4,
      "contracts":   25,
      "notional":    250,
      "liquidation": false,
      "trade_time":  1747526400123
    }
  ]
}
```

#### `GET /health`

```json
{ "status": "ok", "backfill": "complete", "lastCandle": "2026-05-18T00:00:00.000Z", "symbol": "BTC" }
```

### REST — TradingView UDF datafeed

These five endpoints implement the TradingView UDF spec for the symbol this
instance runs. Use them only if you're driving a TradingView Charting Library.

- `GET /config` — UDF datafeed config.
- `GET /time` — server time as plain text seconds since epoch.
- `GET /symbols?symbol=<SYMBOL>` — symbol metadata.
- `GET /search?query=<q>&limit=<n>` — symbol search.
- `GET /history?symbol=<SYMBOL>&resolution=<r>&from=<ts>&to=<ts>&countback=<n>`
  — historical candles. `resolution` is in TradingView format (`1`, `2`, `3`,
  `5`, `15`, `30`, `60`, `240`, `480`, `D`, `W`). Returns
  `{ s: "ok", t:[…], o:[…], h:[…], l:[…], c:[…], v:[…] }` or
  `{ s: "no_data" }` / `{ s: "error", errmsg: "..." }`.
- `GET /quotes?symbols=<SYMBOL>` — real-time quote.

### WebSocket topics

All topic names use the instance's symbol (`BTC`, `ETH`, …) in place of `<SYMBOL>`.

#### `<SYMBOL>@orderbook`

Full L2 book every tick. Asks ascending, bids descending. Up to 100 levels per side.

```json
{
  "topic": "BTC@orderbook",
  "ts": 1747526400123,
  "snapshotId": "...",
  "seq": 4271,
  "data": {
    "symbol": "BTC",
    "asks": [[77177, 316.39], [77178, 119.4], ...],
    "bids": [[77173, 329.42], [77172, 192.4], ...]
  }
}
```

Each level is `[price, size]`.

#### `<SYMBOL>@trade`

One frame per spot trade.

```json
{
  "topic": "BTC@trade",
  "ts": 1747526400123,
  "snapshotId": "...",
  "seq": 9912,
  "data": {
    "symbol":     "BTC",
    "side":       "BUY",
    "price":      77175,
    "size":       0.42,
    "trade_time": 1747526400123
  }
}
```

Subscribe-time snapshot carries the most recent 100 trades:

```json
{
  "topic": "BTC@trade",
  "ts": ...,
  "snapshotId": "...",
  "seq": ...,
  "snapshot": true,
  "data": { "symbol": "BTC", "trades": [ ...newest first... ] }
}
```

#### `<SYMBOL>@stats`

Rolling stats, broadcast every 30s and on subscribe.

```json
{
  "topic": "BTC@stats",
  "ts": ...,
  "snapshotId": "...",
  "seq": ...,
  "data": {
    "symbol":            "BTC",
    "volume_24h":        12345678.9,
    "open_interest":     98765432.1,
    "est_funding_rate":  0.00012,
    "last_funding_rate": 0.00009
  }
}
```

#### `<SYMBOL>@kline_<interval>`

Live OHLCV per timeframe. Valid intervals:
`1m`, `2m`, `3m`, `5m`, `15m`, `30m`, `1h`, `4h`, `8h`, `1D`, `1W`.

Subscribe message may include `"limit": N` to control snapshot depth
(default 1000, max 5000).

Snapshot frame:

```json
{
  "topic": "BTC@kline_1m",
  "ts": ...,
  "snapshotId": "...",
  "seq": ...,
  "snapshot": true,
  "data": {
    "symbol": "BTC",
    "interval": "1m",
    "candles": [
      [t, o, h, l, c, v, closed],
      ...
    ]
  }
}
```

Live frame (one per price tick while a candle is open):

```json
{
  "topic": "BTC@kline_1m",
  "ts": ...,
  "snapshotId": "...",
  "seq": ...,
  "data": {
    "symbol": "BTC",
    "interval": "1m",
    "candle": [t, o, h, l, c, v, closed]
  }
}
```

Candle tuple is `[t, o, h, l, c, v, closed]`:
- `t` = bucket start in **seconds** since epoch
- `o`, `h`, `l`, `c` = open/high/low/close
- `v` = volume
- `closed` = `1` when finalized, `0` while still open

On rollover, the server emits the previous candle with `closed: 1` followed
immediately by the new candle with `closed: 0`. Same `t` ⇒ update; new `t` ⇒
new bar.

#### `<SYMBOL>@perp_orderbook`

Perp L2 book, same shape as spot plus mark/index/basis metadata.

```json
{
  "topic": "BTC@perp_orderbook",
  "ts": ...,
  "snapshotId": "...",
  "seq": ...,
  "data": {
    "symbol":      "BTC",
    "mark_price":  77182.4,
    "index_price": 77173.0,
    "basis_bps":   1.21,
    "asks": [[77185, 25], [77190, 130], ...],
    "bids": [[77180, 40], [77175, 90], ...]
  }
}
```

Sizes are in **contracts** (integers), not base units.

#### `<SYMBOL>@perp_trade`

One frame per perp trade.

```json
{
  "topic": "BTC@perp_trade",
  "ts": ...,
  "snapshotId": "...",
  "seq": ...,
  "data": {
    "symbol":      "BTC",
    "side":        "BUY",
    "price":       77182.4,
    "contracts":   25,
    "notional":    250,
    "liquidation": false,
    "trade_time":  1747526400123
  }
}
```

Subscribe-time snapshot mirrors `<SYMBOL>@trade` (most recent 100, newest first).

#### `<SYMBOL>@perp_stats`

Broadcast every 30s and on subscribe.

```json
{
  "topic": "BTC@perp_stats",
  "ts": ...,
  "snapshotId": "...",
  "seq": ...,
  "data": {
    "symbol":            "BTC",
    "volume_24h":        ...,
    "open_interest":     ...,
    "est_funding_rate":  ...,
    "last_funding_rate": ...,
    "mark_price":        77182.4,
    "index_price":       77173.0,
    "basis_bps":         1.21,
    "long_short_ratio":  1.03,
    "liq_volume_24h":    ...
  }
}
```

---

## Markets Service

Single instance. Cross-symbol market table and platform totals.

Base URL: `http(s)://<markets-host>:3020`
WebSocket: `ws(s)://<markets-host>:3020`

### REST

#### `GET /markets`

All markets, one entry per symbol.

```json
{
  "ts": 1747526400123,
  "markets": [
    {
      "symbol":          "BTC",
      "symbol_full":     "Bitcoin",
      "price":           77173.0,
      "change_24h":      0.0123,
      "volume_24h":      1234567.8,
      "open_interest":   987654.3,
      "funding_8h":      0.00009,
      "est_funding_rate": 0.00012,
      "volume_7d":       9876543.2,
      "volume_30d":      45678901.2,
      "trades_count":    123456,
      "last_updated":    1747526399000,

      "perp_mark_price":          77182.4,
      "perp_index_price":         77173.0,
      "perp_basis_bps":           1.21,
      "perp_funding_rate":        0.00009,
      "perp_next_funding_ms":     1747555200000,
      "perp_long_short_ratio":    1.03,
      "perp_liq_volume_24h":      ...,
      "perp_oi_contracts":        ...,
      "perp_oi_usd":              ...,
      "perp_volume_24h_contracts": ...,
      "perp_volume_24h_usd":      ...
    },
    ...
  ]
}
```

#### `GET /markets/:symbol`

Single market record. `:symbol` is case-insensitive.

```json
{ "ts": ..., "market": { ...same shape as one element above... } }
```

`404` with `{ "error": "Unknown symbol: ..." }` if the symbol isn't tracked.

#### `GET /totals`

Platform totals derived from the same data as `/markets`.

```json
{
  "ts": ...,
  "totals": {
    "total_trades": 1234567,
    "volume_24h":   12345678.9,
    "volume_7d":    98765432.1,
    "volume_30d":   456789012.3,
    "total_oi":     9876543.2
  }
}
```

#### `GET /health`

```json
{ "ok": true, "service": "markets-aggregator", "symbols": 18, "uptimeSec": 12345 }
```

### WebSocket topics

#### `markets@all`

Full array of all market records, broadcast every 5s and on subscribe. Same
record shape as `GET /markets`'s `markets` array.

```json
{
  "topic": "markets@all",
  "ts": ...,
  "snapshotId": "...",
  "seq": ...,
  "data": [ { "symbol": "BTC", ... }, { "symbol": "ETH", ... }, ... ]
}
```

#### `markets@totals`

Platform totals, broadcast every 5s and on subscribe. Same shape as
`GET /totals`'s `totals` object.

```json
{
  "topic": "markets@totals",
  "ts": ...,
  "snapshotId": "...",
  "seq": ...,
  "data": {
    "total_trades": 1234567,
    "volume_24h":   12345678.9,
    "volume_7d":    98765432.1,
    "volume_30d":   456789012.3,
    "total_oi":     9876543.2
  }
}
```

#### `<SYMBOL>@market`

Single-market record for the named symbol. Same record shape as a `markets@all`
element. Broadcast every 5s when at least one subscriber is connected.

```json
{
  "topic": "BTC@market",
  "ts": ...,
  "snapshotId": "...",
  "seq": ...,
  "data": { "symbol": "BTC", "price": 77173.0, ... }
}
```

---

## Chain Explorer Service

Single instance. On-chain block and transaction stream.

Base URL: `http(s)://<chain-host>:4500`
WebSocket: `ws(s)://<chain-host>:4500`

### REST

#### `GET /blocks?limit=N`

Recent blocks, newest first. `limit` clamped to `[1, 50]`, default `20`.

```json
{
  "blocks": [
    {
      "block_number": 1234567,
      "time":         "2026-05-18T00:00:00.123Z",
      "proposer":     "AB12CD34...",
      "hash":         "0x...",
      "transactions": 142
    }
  ]
}
```

#### `GET /transactions?limit=N`

Recent transactions, newest first. `limit` clamped to `[1, 200]`, default `50`.

```json
{
  "transactions": [
    {
      "hash":   "0x...",
      "action": "OROB Fill",
      "block":  1234567,
      "time":   "2026-05-18T00:00:00.123Z",
      "user":   "pax1abc...",
      "symbol": "BTC",
      "side":   "buy",
      "price":  77175.2,
      "size":   0.42
    }
  ]
}
```

`action` is one of: `"Place Order"`, `"Cancel Order"`, `"Modify Order"`,
`"OROB Fill"`, `"Oracle Updated"`, `"Sealed Batch Executed"`.

#### `GET /health`

```json
{ "status": "ok", "ts": ..., "chain_connected": true, "blocks_seen": 12345, "txs_seen": 678901 }
```

### WebSocket topics

#### `chain@blocks`

One frame per new block. Subscribe-time snapshot is an array of the most
recent 20 blocks.

Live frame:

```json
{
  "topic": "chain@blocks",
  "ts": ...,
  "snapshotId": "...",
  "seq": ...,
  "data": {
    "block_number": 1234567,
    "time":         "2026-05-18T00:00:00.123Z",
    "proposer":     "AB12CD34...",
    "hash":         "0x...",
    "transactions": 142
  }
}
```

Snapshot frame (same envelope, but `data` is an array and `snapshot: true`):

```json
{
  "topic": "chain@blocks",
  "ts": ...,
  "snapshotId": "...",
  "seq": ...,
  "snapshot": true,
  "data": [ { "block_number": ..., ... }, ... ]
}
```

#### `chain@transactions`

One frame per transaction. Subscribe-time snapshot is an array of the most
recent 50 transactions.

Live frame:

```json
{
  "topic": "chain@transactions",
  "ts": ...,
  "snapshotId": "...",
  "seq": ...,
  "data": {
    "hash":   "0x...",
    "action": "OROB Fill",
    "block":  1234567,
    "time":   "2026-05-18T00:00:00.123Z",
    "user":   "pax1abc...",
    "symbol": "BTC",
    "side":   "buy",
    "price":  77175.2,
    "size":   0.42
  }
}
```

Snapshot frame: same envelope with `snapshot: true` and `data` as an array of
recent transactions.
