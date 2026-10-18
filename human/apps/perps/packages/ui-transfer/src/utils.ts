import { i18n } from "@paxeer-network/i18n";
import { API } from "@paxeer-network/types";

export function sortTokensWithUSDCFirst(tokens: API.TokenInfo[] = []) {
  const sortedTokens = [...tokens];
  sortedTokens.sort((a, b) => {
    if (a.symbol === "USDC") return -1;
    if (b.symbol === "USDC") return 1;
    return 0;
  });
  return sortedTokens;
}

/** Prefer USDC, then USDC.e, then USDbC, then first token. Single pass, no extra object allocation. */
export const getUSDCToken = (
  tokens: API.TokenInfo[] = [],
): API.TokenInfo | undefined => {
  let first: API.TokenInfo | undefined;
  let usdce: API.TokenInfo | undefined;
  let usdbc: API.TokenInfo | undefined;

  for (const token of tokens) {
    if (first === undefined) first = token;
    if (token.symbol === "USDC") return token;
    if (token.symbol === "USDC.e") usdce = token;
    if (token.symbol === "USDbC") usdbc = token;
  }

  return usdce ?? usdbc ?? first;
};

export const feeDecimalsOffset = (origin?: number): number => {
  return (origin ?? 2) + 3;
};

// export const priceDecimalsOffset = (origin?: number): number => {
//   return Math.abs((origin ?? 2) - 5);
// };

export function checkIsAccountId(accountId: string) {
  const regex = /^0x[a-fA-F0-9]{64}$/;
  return regex.test(accountId);
}

export function getTransferErrorMessage(errorCode: number) {
  if (errorCode === 34) {
    return i18n.t("transfer.internalTransfer.error.transferInProgress");
  }

  if (errorCode === 17) {
    return i18n.t("transfer.internalTransfer.error.withdrawalInProgress");
  }

  if (errorCode === 35) {
    return i18n.t("transfer.internalTransfer.error.accountIdNotExist");
  }

  if (errorCode === 37) {
    return i18n.t("transfer.internalTransfer.error.transferToSelf");
  }

  if (errorCode === 46) {
    return i18n.t("transfer.internalTransfer.error.transferToSubAccount");
  }

  // TODO: when api return 500 status, not throw error code
  // if (errorCode === -1000) {
  //   return i18n.t("transfer.internalTransfer.error.scopeInsufficient");
  // }

  return i18n.t("transfer.internalTransfer.error.default");
}

export const DEPOSIT_ERROR_CODE_MAP = {
  AccessControlBadConfirmation: "0x6697b232",
  AccessControlUnauthorizedAccount: "0xe2517d3f",
  AccountIdInvalid: "0xc7ee9ce6",
  AddressZero: "0x9fabe1c1",
  BalanceNotEnough: "0x4b3815a6",
  BrokerNotAllowed: "0x59d9b863",
  CeffuAddressMismatch: "0xf67c6d78",
  DepositExceedLimit: "0xd969df24",
  EnumerableSetError: "0xa65b249b",
  InvalidSwapSignature: "0x06a0cf4a",
  InvalidTokenAddress: "0x1eb00b06",
  NativeTokenDepositAmountMismatch: "0xfa7c7537",
  NotRebalanceEnableToken: "0xad674ae6",
  NotZeroCodeLength: "0x623793c9",
  OnlyCrossChainManagerCanCall: "0x833d33e7",
  ReentrancyGuardReentrantCall: "0x3ee5aeb5",
  SwapAlreadySubmitted: "0x0b693144",
  TokenNotAllowed: "0xa29c4986",
  ZeroCodeLength: "0x30773dbb",
  ZeroDeposit: "0x56316e87",
  ZeroDepositFee: "0x93d3bb4d",
};

export function getDepositKnownErrorMessage(message: string) {
  if (!message) {
    return "";
  }

  for (const key of Object.keys(DEPOSIT_ERROR_CODE_MAP)) {
    const value =
      DEPOSIT_ERROR_CODE_MAP[key as keyof typeof DEPOSIT_ERROR_CODE_MAP];

    if (message.includes(value)) {
      return `${key} (${value})`;
    }
  }

  return "";
}
