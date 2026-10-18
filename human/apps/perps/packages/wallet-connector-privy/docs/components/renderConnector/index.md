# renderConnector

## Overview

Connector selection UI: **RenderConnector** composes **PrivyConnectArea**, **EVMConnectArea** (wagmi), **SOLConnectArea** (Solana), and **AbstractConnectArea**, and orders them (Privy first, then by selected wallet type). Each area is shown only when the corresponding connector and chain type are enabled.

## Files

| File | Description |
|------|-------------|
| [renderConnector](./renderConnector.md) | RenderConnector – scrollable list of connect areas. |
| [privyConnector](./privyConnector.md) | PrivyConnectArea – email, Google, Twitter, Telegram login. |
| [wagmiConnector](./wagmiConnector.md) | EVMConnectArea – list of Wagmi connectors. |
| [solanaConnector](./solanaConnector.md) | SOLConnectArea – list of Solana wallet adapters. |
| [abstractConnector](./abstractConnector.md) | AbstractConnectArea – single Abstract connect button. |
