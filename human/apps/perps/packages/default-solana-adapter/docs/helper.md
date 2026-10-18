# helper.ts

## helper.ts responsibility

Provides message-encoding functions for paxeer-network operations on Solana (add paxeer-network key, register account, internal transfer, withdraw, settle, dex request) and implements Solana vault deposit flow plus deposit quote fee. Also provides Ledger wallet detection and LZ message encoding utilities.

## helper.ts exports

| Name                    | Type      | Role                  | Description                                                         |
| ----------------------- | --------- | --------------------- | ------------------------------------------------------------------- |
| addPaxeerNetworkKeyMessage    | function  | Message builder       | Returns [message, toSignatureMessage] for add paxeer-network key           |
| registerAccountMessage  | function  | Message builder       | Returns [message, toSignatureMessage] for register account          |
| internalTransferMessage | function  | Message builder       | Returns [message, toSignatureMessage] for internal transfer         |
| withdrawMessage         | function  | Message builder       | Returns [message, toSignatureMessage] for withdraw                  |
| settleMessage           | function  | Message builder       | Returns [message, toSignatureMessage] for settle                    |
| dexRequestMessage       | function  | async Message builder | Returns [message, toSignatureMessage] for dex request               |
| getDepositQuoteFee      | function  | async Fee             | Returns deposit native fee (bigint) for vault                       |
| deposit                 | function  | async Tx              | Builds and sends vault deposit (SOL or SPL) transaction             |
| checkIsLedgerWallet     | function  | Predicate             | Returns true if userAddress is in Ledger wallet list (localStorage) |
| MsgType                 | enum      | Enum                  | Deposit = 0 (for LZ message type)                                   |
| LzMessage               | interface | Type                  | msgType + payload                                                   |
| encodeLzMessage         | function  | Encoder               | Encodes LzMessage to Buffer (1-byte type + payload)                 |

## addPaxeerNetworkKeyMessage input and output

- **Input**: AddPaxeerNetworkKeyInputs & { chainId: number } (publicKey, brokerId, expiration, timestamp, scope, tag, subAccountId).
- **Output**: [message, msgToSignTextEncoded] where message includes chainType "SOL", paxeerNetworkKey, scope, chainId, timestamp, expiration; signature is over keccak256(abi.encode(brokerIdHash, paxeerNetworkKeyHash, scopeHash, chainId, timestamp, expiration)).

## registerAccountMessage / internalTransferMessage / withdrawMessage / settleMessage

Each takes inputs extended with `chainId` and returns `[message, msgToSignTextEncoded]` with message containing chainType "SOL" where applicable. Signing payloads use solidityPackedKeccak256 or keccak256 of ABI-encoded fields as in the code.

## dexRequestMessage input and output

- **Input**: DexRequestInputs & { domain: SignatureDomain; chainId: number } (payloadType, nonce, receiver, amount, vaultId, token, dexBrokerId, chainId).
- **Output**: [message, msgToSignTextEncoded]; receiver is base58-decoded to bytes32, vaultId hex to bytes32, token and dexBrokerId hashed with keccak256.

## getDepositQuoteFee responsibility and flow

Gets native deposit fee for the vault by resolving OApp/peer PDAs, calling program.methods.oappQuote(depositParams) with LayerZero remaining accounts (three DVN groups via `appendThreeDvnQuoteRemainingAccounts`), prepending a compute budget of 600k CU, simulating the v0 transaction, and reading the first 8 bytes (bigint LE) from the program return log.

## getDepositQuoteFee parameters

| Name         | Type       | Description                                                           |
| ------------ | ---------- | --------------------------------------------------------------------- |
| vaultAddress | string     | Vault program id (public key)                                         |
| userAddress  | string     | User wallet public key                                                |
| connection   | Connection | Solana connection                                                     |
| depositData  | object     | tokenHash, brokerHash, accountId, tokenAddress, tokenAmount (strings) |

## deposit responsibility and flow

Builds vault deposit instruction (depositSol or deposit) with LayerZero send remaining accounts including three DVN groups (`appendThreeDvnDepositRemainingAccounts`), gets quote fee via getDepositQuoteFee, builds VersionedTransaction with lookup table and 800k CU limit, then sends via sendTransaction. Handles SOL vs SPL token accounts. Remaining-account layout should stay aligned with solana-vault `utils.ts` on each network.

## deposit parameters

| Name            | Type                                  | Description                                                 |
| --------------- | ------------------------------------- | ----------------------------------------------------------- |
| vaultAddress    | string                                | Vault program id                                            |
| userAddress     | string                                | User wallet public key                                      |
| connection      | Connection                            | Solana connection                                           |
| sendTransaction | WalletAdapterProps["sendTransaction"] | Send signed transaction                                     |
| depositData     | object                                | tokenHash, brokerHash, accountId, tokenAddress, tokenAmount |

## helper.ts dependency and usage

- **Upstream**: @coral-xyz/anchor (BN, Program), @solana/wallet-adapter-base, @solana/web3.js, bs58, ethereum-cryptography, ethers, @paxeer-network/core, @paxeer-network/types, ./constant, ./idl/solana_vault, ./solana.util.
- **Downstream**: walletAdapter.ts uses all message builders, getDepositQuoteFee, deposit, checkIsLedgerWallet.

## helper.ts errors and boundaries

| Scenario              | Condition                                  | Behavior                                                     | Handling           |
| --------------------- | ------------------------------------------ | ------------------------------------------------------------ | ------------------ |
| Lookup table missing  | getLookupTableAccount returns null         | Throws or early return "lookup table account error"          | Caller must handle |
| Simulate failure      | feeRes.value.err (e.g. AccountNotFound)    | Throws "Account gas is insufficient." or Error with err info | Caller must handle |
| No return log for fee | No log starting with "Program return: ..." | Throws "Error: get deposit fee error"                        | Caller must handle |

## helper.ts Example

```typescript
import {
  registerAccountMessage,
  getDepositQuoteFee,
  deposit,
  checkIsLedgerWallet,
} from "./helper";

const [msg, toSign] = registerAccountMessage({
  chainId: 101,
  brokerId: "broker",
  registrationNonce: 1,
  timestamp: Date.now(),
});

const fee = await getDepositQuoteFee({
  vaultAddress: "...",
  userAddress: "...",
  connection,
  depositData: {
    tokenHash: "0x...",
    brokerHash: "0x...",
    accountId: "0x...",
    tokenAddress: "...",
    tokenAmount: "1000000",
  },
});

const isLedger = checkIsLedgerWallet(userAddress);
```
