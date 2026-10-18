# availableToClaim.ui

## Overview

UI for “Available to claim”: header with “Claim” link and two stat cards (ORDER and esORDER) with icons and formatted values.

## Exports

### AvailableToClaim (component)

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| order | number \| undefined | No | ORDER amount. |
| esOrder | number \| undefined | No | esORDER amount. |
| goToClaim | (e: any) => void | Yes | Click handler for Claim link. |

Uses `PaxeerNetworkIcon`, `EsPaxeerNetworkIcon`, `JumpIcon` from components.

## Usage example

```tsx
<AvailableToClaim order={100} esOrder={50} goToClaim={() => window.open(...)} />
```
