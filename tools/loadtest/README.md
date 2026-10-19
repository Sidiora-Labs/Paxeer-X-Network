# pax-load
[![Tests](https://github.com/paxeer-network/pax-load/actions/workflows/build-and-test.yml/badge.svg)](https://github.com/paxeer-network/pax-load/actions/workflows/build-and-test.yml)

A load testing tool for Pax Chain that generates transactions and measures performance.

## Quick Start

### 1. Build

```bash
make build
```

### 2. Create Config

```bash
cp profiles/local.json my-config.json
```

Edit `my-config.json`:
```json
{
  "endpoints": ["http://localhost:8545"],
  "chainId": 713714,
  "scenarios": [
    {"name": "EVMTransfer", "weight": 100}
  ],
  "accounts": {
    "count": 100,
    "newAccountRate": 0.1
  },
  "settings": {
    "workers": 5,
    "tps": 100,
    "statsInterval": "10s",
    "bufferSize": 1000,
    "trackUserLatency": true
  }
}
```

### 3. Run

```bash
./paxload --config my-config.json
```

## Command Line Options

| Flag | Default | Description |
|------|---------|-------------|
| `--config, -c` | | Config file path (required) |
| `--workers, -w` | 1 | Workers per endpoint |
| `--tps, -t` | 0 | Transactions per second (0 = unlimited) |
| `--stats-interval, -s` | 10s | Stats logging interval |
| `--buffer-size, -b` | 1000 | Buffer size per worker |
| `--dry-run` | false | Simulate without sending |
| `--debug` | false | Log each transaction |
| `--track-receipts` | false | Track transaction receipts |
| `--track-blocks` | false | Track block statistics |
| `--track-user-latency` | false | Track user latency metrics |
| `--prewarm` | false | Prewarm accounts before test |

## Examples

### Basic Load Test
```bash
./paxload --config my-config.json --workers 5 --tps 100
```

### High Throughput Test
```bash
./paxload --config my-config.json --workers 20 --buffer-size 2000
```

### Debug Mode
```bash
./paxload --config my-config.json --debug --dry-run
```

### With Receipt Tracking
```bash
./paxload --config my-config.json --track-receipts --track-blocks
```

### User Latency Monitoring
```bash
./paxload --config my-config.json --track-user-latency --stats-interval 5s
```

## Configuration

### Basic Structure
```json
{
  "endpoints": ["http://localhost:8545"],
  "chainId": 713714,
  "scenarios": [...],
  "accounts": {...},
  "settings": {...}
}
```

### Scenarios
```json
"scenarios": [
  {"name": "EVMTransfer", "weight": 50},
  {"name": "ERC20", "weight": 30},
  {"name": "ERC721", "weight": 20}
]
```

### Account Management
```json
"accounts": {
  "count": 100,
  "newAccountRate": 0.1
}
```

### Settings
```json
"settings": {
  "workers": 5,
  "tps": 100,
  "statsInterval": "10s",
  "bufferSize": 1000,
  "trackUserLatency": true
}
```

**Settings Precedence**: CLI flags > Config file settings > Default values

Available settings:
- `workers`: Number of workers per endpoint
- `tps`: Transactions per second (0 = unlimited)
- `statsInterval`: Stats logging interval (e.g., "10s", "5m")
- `bufferSize`: Buffer size per worker
- `dryRun`: Simulate without sending transactions
- `debug`: Enable debug logging
- `trackReceipts`: Track transaction receipts
- `trackBlocks`: Track block statistics
- `trackUserLatency`: Track user latency metrics
- `prewarm`: Prewarm accounts before test

## Available Scenarios

- **EVMTransfer**: Simple ETH transfers
- **ERC20**: ERC20 token operations
- **ERC20Noop**: ERC20 no-op transactions
- **ERC20Conflict**: ERC20 conflicting transactions
- **ERC721**: NFT operations

## Output

### Standard Metrics
```
throughput tps=133.00, txs=1330, latency(avg=8ms p50=5ms p99=27ms max=494ms)
```

### User Latency (with --track-user-latency)
```
user latency height=5191 txs=32 min=1s p50=2s max=5s
```

### Block Stats (with --track-blocks)
```
blocks height=5191 time(p50=2s p99=5s max=8s) gas(p50=21000 p99=50000 max=100000)
```

## Development

### Build
```bash
make build
```

### Test
```bash
make test
```

### Lint
```bash
make lint
```

### Clean
```bash
make clean
```

## Troubleshooting

### Connection Issues
- Check endpoint URLs in config
- Verify network connectivity
- Try `--dry-run` to test config

### Low Performance
- Increase `--workers`
- Increase `--buffer-size`

### Memory Issues
- Reduce `--buffer-size`
- Reduce worker count
- Disable receipt tracking
