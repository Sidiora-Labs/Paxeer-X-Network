import { useMemo } from "react";
import { useIndexPrice } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { API } from "@paxeer-network/types";
import { Decimal, toNonExponential } from "@paxeer-network/utils";
import { feeDecimalsOffset } from "../../../utils";
import { TransactionInfo } from "./useSwapEnquiry";

export type UseSwapFee = ReturnType<typeof useSwapFee>;

type FeeQty = {
  value: string | number;
  dp: number;
  symbol?: string;
};

export function useSwapFee(options: {
  nativeToken?: API.TokenInfo;
  isNativeToken?: boolean;
  src?: API.TokenInfo;
  depositFee?: bigint;
  transactionInfo: TransactionInfo;
  needSwap: boolean;
  needCrossSwap: boolean;
}) {
  const {
    nativeToken,
    isNativeToken,
    src,
    depositFee = 0,
    transactionInfo,
    needSwap,
    needCrossSwap,
  } = options;

  const nativeSymbol = nativeToken?.symbol;
  const srcSymbol = src?.symbol;

  const {
    fee = "0",
    swapFee = "0",
    bridgeFee = "0",
    dstGasFee: destinationGasFee = "0",
    markPrices,
  } = transactionInfo;

  const { from_token: markPrice, native_token: nativeMarkPrice } = markPrices;

  const { data: symbolPrice } = useIndexPrice(`SPOT_${nativeSymbol}_USDC`);

  const { t } = useTranslation();

  const feeInfo = useMemo(() => {
    let feeAmount = "";
    let feeQtys: FeeQty[] = [];

    const nativeDp = feeDecimalsOffset(
      // swap precision
      nativeToken?.precision,
    );

    // swap precision
    const srcDp = feeDecimalsOffset(src?.precision);

    const dstGasFee = needCrossSwap
      ? destinationGasFee
      : new Decimal(depositFee.toString())
          .div(new Decimal(10).pow(18))
          .toString();

    if (!needSwap && !needCrossSwap) {
      feeQtys = [
        {
          value: dstGasFee,
          dp: nativeDp,
        },
      ];

      feeAmount = new Decimal(dstGasFee).mul(symbolPrice || 0).toString();
    }

    if (needSwap || needCrossSwap) {
      // if native token, Destination gas fee、fee (Swap fee + Bridge fee ) will use a same symbol unit
      if (isNativeToken) {
        const totalFeeQty = new Decimal(dstGasFee).plus(fee);

        feeQtys = [
          {
            value: totalFeeQty.toString(),
            dp: nativeDp,
          },
        ];

        feeAmount = totalFeeQty.mul(nativeMarkPrice ?? 1).toString();
      } else {
        feeQtys = [
          {
            value: dstGasFee,
            dp: nativeDp,
            symbol: nativeSymbol,
          },
          {
            value: fee,
            dp: srcDp,
            symbol: srcSymbol,
          },
        ];

        feeAmount = new Decimal(dstGasFee)
          .mul(nativeMarkPrice ?? 1)
          .plus(new Decimal(fee).mul(markPrice ?? 1))
          .toString();
      }
    }

    feeQtys = feeQtys.filter(
      (item) => !!item.value && Number(item.value) !== 0,
    );

    const feeDetails = [
      {
        title: t("transfer.deposit.destinationGasFee"),
        description: t("transfer.deposit.destinationGasFee.description"),
        value: dstGasFee,
        dp: nativeDp,
        symbol: nativeSymbol,
      },
      {
        title: t("transfer.swapDeposit.swapFee"),
        description: t("transfer.swapDeposit.swapFee.description"),
        value: swapFee,
        dp: srcDp,
        symbol: srcSymbol,
      },
      {
        title: t("transfer.swapDeposit.bridgeFee"),
        description: t("transfer.swapDeposit.bridgeFee.description"),
        value: bridgeFee,
        dp: srcDp,
        symbol: srcSymbol,
      },
    ]
      .filter(
        // alway show Destination gas fee
        (item, index) =>
          index === 0 || (!!item.value && Number(item.value) !== 0),
      )
      .map((item) => {
        const value = new Decimal(item.value || 0).todp(item.dp).toNumber();
        return {
          ...item,
          value: toNonExponential(value),
        };
      });

    return {
      feeAmount,
      feeQtys,
      feeDetails,
    };
  }, [
    depositFee,
    symbolPrice,
    nativeSymbol,
    srcSymbol,
    transactionInfo,
    needSwap,
    needCrossSwap,
    t,
  ]);

  return {
    ...feeInfo,
    nativeSymbol,
  };
}
