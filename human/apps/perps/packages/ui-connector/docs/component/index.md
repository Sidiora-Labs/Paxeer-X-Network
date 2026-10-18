# component

UI components and hooks for wallet connection and auth guards: modal/sheet content, auth guard variants, steps, and builder hook.

## Files

| File | Language | Description |
|------|----------|-------------|
| [useWalletConnectorBuilder.ts](./useWalletConnectorBuilder.md) | TypeScript | Hook: account state, sign-in/enable-trading, referral code validation |
| [authGuard.tsx](./authGuard.md) | React/TSX | Auth guard: shows fallback (connect/sign-in/enable trading/switch chain) until status and network are satisfied |
| [walletConnectorContent.tsx](./walletConnectorContent.md) | React/TSX | Wallet connect dialog/sheet content: steps, referral code, remember me, actions |
| [useAuthGuard.ts](./useAuthGuard.md) | TypeScript | Hook: returns whether current user meets required account status and network |
| [walletConnector.tsx](./walletConnector.md) | React/TSX | Wallet connector widget and modal/sheet registration |
| [step.tsx](./step.md) | React/TSX | Step item UI for wallet connector flow |
| [authGuardEmpty.tsx](./authGuardEmpty.md) | React/TSX | Auth guard wrapper with empty state as fallback |
| [authGuardTooltip.tsx](./authGuardTooltip.md) | React/TSX | Tooltip that shows auth hint by account/network status |
| [authGuardDataTable.tsx](./authGuardDataTable.md) | React/TSX | DataTable with auth guard empty view |
