# assets.ts

## assets.ts Responsibility

Provides the `Assets` class for balance, allowance, approve, deposit, withdraw, internal transfer, and convert. Uses Account (walletAdapter, signer, accountId), ConfigStore (apiBaseUrl, brokerId), and IContract (vault/USDC ABIs and addresses).

## assets.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| Assets | class | Core entity | Balance, approve, deposit, withdraw, internalTransfer, convert |

## Assets Responsibility

Handles on-chain and API operations for user assets: read balance/allowance, approve, deposit to vault, withdraw request, internal transfer, and manual convert. Abstracts EVM vs Solana and Abstract chain via account.walletAdapter and contract info.

## Assets Input and Output

- **Input**: Constructor(configStore, contractManager, account). Methods take typed inputs (address, decimals, amount, vaultAddress, chainId, token, etc.).
- **Output**: Promises returning strings (balance, allowance), void (approve), or API response objects (withdraw, internalTransfer, convert). getDepositData returns contractMethod, userAddress, fromAddress, depositData, contractData, abi.

## Assets Constructor and Key Methods

| Method | Purpose |
|--------|---------|
| getBalance(address, options) | Token balance for user (or AGW address on Abstract) |
| getNativeBalance(options) | Native token balance |
| getAllowance(inputs) | Allowance for token + vault |
| approve(inputs) | ERC-20 approve for vault (or max) |
| getDepositFee(inputs) | Vault getDepositFee call |
| estimateDepositGasFee / estimateNativeDepositGasFee | Gas estimate for deposit |
| getDepositData(inputs) | Build deposit payload (accountIdHashStr, brokerHash, tokenHash, tokenAmount) |
| deposit(inputs) | Send vault deposit transaction |
| depositNativeToken(inputs) | Deposit native token (amount + fee as value) |
| withdraw(inputs) | POST /v1/withdraw_request with signed message |
| internalTransfer(inputs) | POST /v2/internal_transfer with signed message |
| convert(inputs) | POST /v1/asset/manual_convert (non-USDC to USDC) |

## Assets Dependencies and Call Relationships

- **Upstream**: Account (provides walletAdapter, signer, stateValue.address, accountIdHashStr, getAdditionalInfo).
- **Downstream**: ConfigStore (get apiBaseUrl, brokerId), IContract (getContractInfoByEnv, ABIs, addresses), WalletAdapter (call, sendTransaction, estimateGasFee, parseUnits, formatUnits, generate*Message), fetch for API.

## Assets Errors and Boundaries

| Scenario | Condition | Behavior | Handling |
|----------|-----------|----------|----------|
| No walletAdapter | getBalance, approve, deposit, etc. | Return "0" or throw "walletAdapter is undefined" | Connect wallet first |
| Missing decimals | approve, getDepositData, withdraw, internalTransfer | throw "decimals is required" | Pass decimals |
| Missing brokerId | getDepositData | throw "[Assets]:brokerId is required" | Set brokerId in config |
| API error | withdraw, internalTransfer, convert | throw ApiError(res.message, res.code) | Catch and handle |

## Assets Extension and Modification Points

- **Chains**: isSolana(), isAbstract(), getAgwGlobalAddress(); add new chain branches in getDepositData and withdraw if needed.
- **Deposit/withdraw payload**: getDepositData, getWithdrawalNonce, generateWithdrawMessage; adjust for new contract or API fields.
- **Convert**: /v1/asset/manual_convert payload and validation (e.g. converted_asset not USDC).

## Assets Example

```typescript
// account already set up with walletAdapter
const assets = new Assets(configStore, contractManager, account);

const balance = await assets.getBalance(usdcAddress, { decimals: 6 });
await assets.approve({ address: usdcAddress, vaultAddress, decimals: 6, isSetMaxValue: true });
const depositData = await assets.getDepositData({ amount: "100", decimals: 6 });
const fee = await assets.getDepositFee({ amount: "100", chain, decimals: 6 });
await assets.deposit({ amount: "100", fee: BigInt(0), decimals: 6, vaultAddress });

await assets.withdraw({
  chainId: 421614,
  token: "USDC",
  amount: "10",
  decimals: 6,
  allowCrossChainWithdraw: false,
});
await assets.internalTransfer({ token: "USDC", amount: "5", receiver: "0x...", decimals: 6 });
```
