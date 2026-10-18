# RenderConnector (index)

## Overview

**RenderConnector** is the main connector selection UI used inside **ConnectDrawer** when the user is not connected. It renders a scrollable list of connect areas: **PrivyConnectArea** (if Privy enabled), then **EVMConnectArea**, **SOLConnectArea**, and **AbstractConnectArea** in an order that prioritizes the currently selected wallet type (from `targetWalletType` or `storageChain`).

## Exports

- **RenderConnector** – Function component; no props.

## Behavior

- Reads `connectorWalletType` and `walletChainTypeConfig` to show/hide each area.
- Calls `useWallet().connect` with the appropriate **ConnectProps** when user selects an option.
- Closes the connect drawer after Privy connect.
