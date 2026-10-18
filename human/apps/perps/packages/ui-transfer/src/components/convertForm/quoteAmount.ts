import { Decimal } from "@paxeer-network/utils";

const isValidAmount = (value: string | undefined): value is string => {
  const amount = Number(value);
  return Number.isFinite(amount) && amount > 0;
};

export const getQuoteTargetAmount = (
  estimatedValue: string | undefined,
  estimatedAmount: string | undefined,
  decimals: number | undefined,
) => {
  if (isValidAmount(estimatedValue)) {
    return estimatedValue;
  }
  if (
    isValidAmount(estimatedAmount) &&
    Number.isInteger(decimals) &&
    typeof decimals === "number" &&
    decimals >= 0
  ) {
    return new Decimal(estimatedAmount)
      .div(new Decimal(10).pow(decimals))
      .toString();
  }
  return "-";
};

export const calculateQuoteRate = (
  inputAmount: number,
  estimatedUsdcValue: string,
) => new Decimal(estimatedUsdcValue).div(inputAmount).toString();

// CeFi manual swap quotes come back with an empty chainId and no slippage
// guarantee (slippageLimitPercent "0"), so the minimum-received row must not
// be presented for them.
export const isCeFiQuote = (chainId: string | undefined) =>
  typeof chainId === "string" && chainId.trim() === "";

// A pair's swap routing (CeFi vs on-chain) is only observable from a matched
// quote. While the quote reloads or the input is empty, fall back to the
// chainId remembered for the current pair, then to the token-metadata CeFi
// hint (on_chain_swap === false always routes via CeFi), so CeFi display
// rules hold even before the first quote lands.
//
// Do not "simplify" this into a plain on_chain_swap check. That field is a
// token-level capability flag, not a routing decision: false reliably
// implies CeFi, but true only means on-chain is possible, and the field is
// optional in some API shapes. The backend picks the actual route per
// quote, so the matched quote's chainId is the only source that reflects
// what execution will actually do; metadata is only ever a hint.
export const resolveIsCeFiQuote = (
  matchedQuoteChainId: string | undefined,
  hasMatchedQuote: boolean,
  rememberedChainId: string | undefined,
  metadataHintsCeFi?: boolean,
) => {
  if (hasMatchedQuote) {
    return isCeFiQuote(matchedQuoteChainId);
  }
  if (rememberedChainId !== undefined) {
    return isCeFiQuote(rememberedChainId);
  }
  return metadataHintsCeFi === true;
};

export const calculateMinimumReceived = (
  estimatedUsdcValue: string,
  slippageLimitPercent: string,
) =>
  new Decimal(estimatedUsdcValue)
    .mul(new Decimal(1).minus(new Decimal(slippageLimitPercent).div(100)))
    .toString();
