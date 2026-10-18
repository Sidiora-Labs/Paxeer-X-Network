import { useMemo } from "react";
import { useAccount, useTokenInfo } from "@paxeer-network/hooks";
import { ChainNamespace, NetworkId } from "@paxeer-network/types";
import { Decimal } from "@paxeer-network/utils";

export type UseDepositFeeReturn = ReturnType<typeof useDepositFee>;

export function useDepositFee(options: {
  nativeSymbol?: string;
  depositFee?: bigint;
  getIndexPrice: (token: string) => number;
  networkId: NetworkId;
}) {
  const { nativeSymbol, depositFee = 0, getIndexPrice, networkId } = options;
  const { account } = useAccount();

  const tokenInfo = useTokenInfo(nativeSymbol!, networkId);

  const feeProps = useMemo(() => {
    // deposit fee is native token, so evm decimals is 18, solana is 9
    const decimals =
      account.walletAdapter?.chainNamespace === ChainNamespace.solana ? 9 : 18;

    const dstGasFee = new Decimal(depositFee.toString())
      .div(new Decimal(10).pow(decimals))
      .toString();

    // If the USD fee is 0 while dstGasFee is non-zero, check the public WS
    // `indexprices` topic for `SPOT_${nativeSymbol}_USDC`. useWSObserver
    // normalizes that symbol to `PERP_${nativeSymbol}_USDC` for this lookup.
    const indexPrice = getIndexPrice(nativeSymbol!);

    const feeAmount = new Decimal(dstGasFee).mul(indexPrice || 0).toString();

    return {
      dstGasFee,
      feeQty: dstGasFee,
      feeAmount,
      dp: tokenInfo?.decimals || 8,
    };
  }, [depositFee, tokenInfo]);

  return feeProps;
}
