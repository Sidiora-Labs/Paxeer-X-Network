# shareButton.script

## Overview

Hook that builds the share PnL modal payload (entity, refCode, leverage) and provides `showModal` to open the modal by id.

## Exports

### `ShareButtonScriptReturn`

Return type of `useShareButtonScript`: `{ iconSize?, sharePnLConfig?, showModal }`.

### `useShareButtonScript(props)`

**Props:** `order`, `sharePnLConfig?`, `modalId`, `iconSize?`. Uses `useReferralInfo`, `useLeverageBySymbol`. `showModal()` calls `modal.show(modalId, { pnl: { entity, refCode, leverage, ...sharePnLConfig } })`.

## Usage example

Used by `ShareButtonWidget`; typically not used directly.
