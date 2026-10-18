# account.ts

## account.ts Responsibility

Provides the `Account` class and `AccountState` interface for paxeer-network: wallet connection, account validation, paxeer-network Key lifecycle, sub-accounts, and settle. Emits status events and drives assets/contract usage.

## account.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| AccountState | interface | State shape | Account status, ids, address, sub-accounts |
| Account | class | Core entity | Account lifecycle, login, keys, sub-accounts, settle |

## AccountState Responsibility

Describes the current account state: connection status, account/sub-account ids, address, chain, and sub-account list.

## AccountState Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| status | AccountStatusEnum | Yes | NotConnected / Connected / NotSignedIn / SignedIn / DisabledTrading / EnableTrading / EnableTradingWithoutConnected |
| validating | boolean | Yes | Whether account is being validated |
| accountId | string | No | Active account ID (root or sub) |
| mainAccountId | string | No | Root account ID |
| subAccountId | string | No | Active sub-account ID |
| userId | string | No | User ID from backend |
| address | string | No | Wallet address |
| chainNamespace | ChainNamespace | No | EVM / Solana |
| isNew | boolean | No | Newly registered account |
| connectWallet | { name, chainId } | No | Connected wallet info |
| subAccounts | SubAccount[] | No | List of sub-accounts with holding |

## Account Responsibility

Manages paxeer-network account lifecycle: set address/wallet, check existence, register, create/switch paxeer-network Key, sub-accounts, and settle. Uses ConfigStore, PaxeerNetworkKeyStore, WalletAdapterManager, IContract, and Assets.

## Account Input and Output

- **Input**: ConfigStore, PaxeerNetworkKeyStore, WalletAdapter[], optional IContract. Later: setAddress(address, wallet), createAccount(), createPaxeerNetworkKey(), etc.
- **Output**: State via stateValue/getters; events via on/once/off; async results (e.g. AccountStatusEnum, createAccount response).

## Account Dependencies and Call Relationships

- **Upstream**: App or provider constructs Account with configStore, keyStore, wallet adapters.
- **Downstream**: ConfigStore, PaxeerNetworkKeyStore, WalletAdapterManager, IContract, Assets, Signer (BaseSigner + keyStore), API (fetch via apiBaseUrl).
- **Related**: SubAccount, AccountState, EVENT_NAMES, WalletAdapter.

## Account Execution and State Flow

1. Constructor: init BaseContract or custom IContract, Assets, WalletAdapterManager, AdditionalInfoRepository; bind statusChanged to update internal state.
2. setAddress(address, wallet): validate inputs; optionally emit switchAccount; persist address in keyStore; save additionalInfo; emit statusChanged (Connected); switchWallet on WalletAdapterManager; emit validateStart; _checkAccount (exist, paxeer-network key, sub-accounts); emit validateEnd and statusChanged.
3. _checkAccount: _checkAccountExist → set accountId/mainAccountId or NotSignedIn; get paxeerNetworkKey from keyStore; _checkPaxeerNetworkKeyState → if ACTIVE and not expired, _restoreSubAccount and EnableTrading else DisabledTrading/cleanKey.
4. createAccount: _getRegisterationNonce → walletAdapter.generateRegisterAccountMessage → POST /v1/register_account → setAccountId, emit statusChanged (DisabledTrading, isNew).
5. createPaxeerNetworkKey: generateApiKey (generateSecretKey, BasePaxeerNetworkKeyPair, generateAddPaxeerNetworkKeyMessage, POST /v1/paxeer-network_key) → keyStore.setKey, emit EnableTrading.
6. settle: _getSettleNonce → walletAdapter.generateSettleMessage → signData → POST /v1/settle_pnl.

## Account Errors and Boundaries

| Scenario | Condition | Behavior | Handling |
|----------|-----------|----------|----------|
| Missing address/wallet/chainId | setAddress | throw SDKError | Caller must pass valid args |
| paxeer-network key missing or inactive | _checkAccount | status DisabledTrading | Create/import paxeer-network Key |
| paxeer-network key expired | _checkPaxeerNetworkKeyState | cleanKey, DisabledTrading | Re-create key |
| API failure | _simpleFetch | res.success false, throw or return status | Caller handles errors |
| mainAccountId undefined | createSubAccount, updateSubAccount, etc. | throw Error | Ensure signed-in main account |

## Account Extension and Modification Points

- **State shape**: Extend `AccountState` and all places that build/assign `nextState`.
- **Validation**: _checkAccount, _checkPaxeerNetworkKeyState, validatePaxeerNetworkKeyScope (deprecated).
- **API base**: _simpleFetch uses configStore apiBaseUrl; change URL or add interceptors there.
- **Events**: EVENT_NAMES and _ee; add new events or listeners where state changes.

## Account Example

```typescript
import { Account, AccountState, DefaultConfigStore, LocalStorageStore, EVENT_NAMES } from "@paxeer-network/core";

const configStore = new DefaultConfigStore({ networkId: "testnet" });
const keyStore = new LocalStorageStore("testnet");
const account = new Account(configStore, keyStore, [evmAdapter, solanaAdapter]);

account.on(EVENT_NAMES.statusChanged, (state: AccountState) => {
  console.log(state.status, state.accountId);
});

await account.setAddress("0x...", {
  provider,
  chain: { id: 421614, namespace: "EVM" },
  wallet: { name: "MetaMask" },
});
// After validation: state.status may be EnableTrading

await account.createPaxeerNetworkKey(365, { scope: "..." });
await account.settle();
```
