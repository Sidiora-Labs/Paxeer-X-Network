# Public RPC

Paxeer X Network serves its EVM JSON-RPC (chain ID 125, consensus chain ID
`hyperpax_125-1`) at two kinds of public name.

## Router

| Surface | URL |
| --- | --- |
| Unified JSON-RPC | `https://api-mainnet-beta.paxeer.network/rpc` |

The router carries both the Paxeer X and the LayerX interface. JSON-RPC is
served on the `/rpc` path only; the router root is not an RPC endpoint.

```bash
curl -s https://api-mainnet-beta.paxeer.network/rpc \
  -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}'
```

## Node names

Each name fronts one full node over HTTPS, with WebSocket on `/ws`.

| Node | HTTPS JSON-RPC | WebSocket |
| --- | --- | --- |
| 1 | `https://api1.mainnet-beta.paxeer.network` | `wss://api1.mainnet-beta.paxeer.network/ws` |
| 2 | `https://api2.mainnet-beta.paxeer.network` | `wss://api2.mainnet-beta.paxeer.network/ws` |
| 3 | `https://api3.mainnet-beta.paxeer.network` | `wss://api3.mainnet-beta.paxeer.network/ws` |
| 4 | `https://api4.mainnet-beta.paxeer.network` | `wss://api4.mainnet-beta.paxeer.network/ws` |
| 5 | `https://api5.mainnet-beta.paxeer.network` | `wss://api5.mainnet-beta.paxeer.network/ws` |
| 6 | `https://api6.mainnet-beta.paxeer.network` | `wss://api6.mainnet-beta.paxeer.network/ws` |
| 7 | `https://api7.mainnet-beta.paxeer.network` | `wss://api7.mainnet-beta.paxeer.network/ws` |
| 8 | `https://api8.mainnet-beta.paxeer.network` | `wss://api8.mainnet-beta.paxeer.network/ws` |
| 9 | `https://api9.mainnet-beta.paxeer.network` | `wss://api9.mainnet-beta.paxeer.network/ws` |
| 10 | `https://api10.mainnet-beta.paxeer.network` | `wss://api10.mainnet-beta.paxeer.network/ws` |
| 11 | `https://api11.mainnet-beta.paxeer.network` | `wss://api11.mainnet-beta.paxeer.network/ws` |
| 12 | `https://api12.mainnet-beta.paxeer.network` | `wss://api12.mainnet-beta.paxeer.network/ws` |
| 13 | `https://api13.mainnet-beta.paxeer.network` | `wss://api13.mainnet-beta.paxeer.network/ws` |
| 14 | `https://api14.mainnet-beta.paxeer.network` | `wss://api14.mainnet-beta.paxeer.network/ws` |
| 15 | `https://api15.mainnet-beta.paxeer.network` | `wss://api15.mainnet-beta.paxeer.network/ws` |
| 16 | `https://api16.mainnet-beta.paxeer.network` | `wss://api16.mainnet-beta.paxeer.network/ws` |

Names are numbered, not ranked. Pick any one and fall back to another if it
stops answering; compare `eth_blockNumber` across two names when you need the
newest head.
