# useAccount

## useAccount Responsibility

Hook that exposes account state and account actions (create paxeer-network key, switch account, create account, create/update sub-account, refresh sub-account balances). Requires `PaxeerNetworkProvider` with `configStore` and `keyStore`. Used by components that need to show account status or trigger account/sub-account operations.

## useAccount Input and Output

- **Input**: None (reads `PaxeerNetworkContext` and uses `useAccountInstance`, `useTrack`, `useWS` internally).
- **Output**: Object with `account`, `state`, `isSubAccount`, `isMainAccount`, `subAccount`, `switchAccount`, `createPaxeerNetworkKey`, `createAccount`.

## useAccount Return Shape

| Property | Type | Description |
|----------|------|-------------|
| account | Account instance | Raw account from core (from useAccountInstance). |
| state | AccountState | Current account state (from core). |
| isSubAccount | boolean | True when current account is a sub-account. |
| isMainAccount | boolean | True when current account is main account. |
| subAccount.refresh | () => void | Refreshes sub-account balances. |
| subAccount.create | (description?: string) => Promise | Creates a sub-account. |
| subAccount.update | (value) => Promise | Updates sub-account (subAccountId, description). |
| switchAccount | (accountId: string) => Promise | Switches to another account; closes private WS. |
| createPaxeerNetworkKey | (remember: boolean) => Promise | Creates paxeer-network key (remember: 365 vs 30 days), restores sub-accounts, tracks signin. |
| createAccount | () => Promise | Creates account. |

## useAccount Dependencies and Callers

- **Upstream**: `PaxeerNetworkContext` (configStore, keyStore), `useAccountInstance`, `useEventEmitter`, `useTrack`, `useWS`.
- **Downstream**: Any component that needs account state or account actions (e.g. header, account modal, API key manager).

## useAccount Execution Flow

1. Read `configStore` and `keyStore` from `PaxeerNetworkContext`; throw if missing.
2. Get `account` from `useAccountInstance()` and `state` from `account.stateValue`.
3. Subscribe to `account.on("change:status")` to keep `state` in sync.
4. Build callbacks for `createPaxeerNetworkKey` (with track + restoreSubAccount), `switchAccount` (close private WS then switch), `createAccount`, `createSubAccount`, `updateSubAccount`, `refreshSubAccountBalances`.
5. Compute `isSubAccount` from `state.accountId !== state.mainAccountId`.
6. Return the object above.

## useAccount Errors and Boundaries

| Scenario | Condition | Behavior | Handling |
|----------|-----------|----------|----------|
| Missing config | configStore undefined | Throws SDKError: "configStore is not defined, please use PaxeerNetworkProvider" | Wrap app with PaxeerNetworkProvider. |
| Missing keyStore | keyStore undefined | Throws SDKError: "keyStore is not defined, please use PaxeerNetworkProvider and provide keyStore" | Provide keyStore to PaxeerNetworkProvider. |

## useAccount Example

```tsx
import { useAccount } from "@paxeer-network/hooks";

function AccountHeader() {
  const { state, createPaxeerNetworkKey, switchAccount, subAccount } = useAccount();

  if (state.status !== AccountStatusEnum.Connected) {
    return (
      <button onClick={() => createPaxeerNetworkKey(true)}>Connect / Create Key</button>
    );
  }

  return (
    <div>
      <span>Account: {state.accountId}</span>
      <button onClick={() => subAccount.refresh()}>Refresh balances</button>
      <button onClick={() => switchAccount("other-account-id")}>Switch</button>
    </div>
  );
}
```
