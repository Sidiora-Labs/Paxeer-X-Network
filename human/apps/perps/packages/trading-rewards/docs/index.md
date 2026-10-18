# trading-rewards (src)

## Overview

This directory contains the source code for the **Trading Rewards** module (`@paxeer-network/trading-rewards`). It provides layout, context, and page components for the trading rewards and affiliate rewards experience, including current epoch display, available-to-claim rewards, stake booster, and reward history.

## Top-level files

| File | Language | Description |
|------|----------|-------------|
| [entry.md](entry.md) | TypeScript | Package entry point; re-exports `TradingRewards` and layout. |
| [version.md](version.md) | TypeScript | Package version and `window.__PAXEER_NETWORK_VERSION__` registration. |
| [install.md](install.md) | TSX | Extension install (currently commented out). |

## Subdirectories

| Directory | Description |
|-----------|-------------|
| [layout/](layout/index.md) | Trading rewards layout: scaffold, sidebar paths, script, and widget. |
| [pages/](pages/index.md) | Rewards home page: provider, title, curEpoch, availableToClaim, stakeBooster, rewardHistory. |
