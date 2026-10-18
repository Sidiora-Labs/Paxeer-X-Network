# LinkDeviceMobile

## Overview

**LinkDeviceMobile** wraps children (e.g. address text) and shows a confirmation dialog to disconnect/link device when clicked. It clears `paxeer-network_link_device` from localStorage and calls `account.disconnect()`.

## Exports

- **LinkDeviceMobile** – React component with `PropsWithChildren`.

## Usage example

```tsx
<LinkDeviceMobile>
  <Text.formatted rule="address">{formatAddress(userAddress)}</Text.formatted>
</LinkDeviceMobile>
```
