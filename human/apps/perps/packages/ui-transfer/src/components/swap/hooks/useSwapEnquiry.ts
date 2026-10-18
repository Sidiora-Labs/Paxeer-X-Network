import { useCallback, useEffect, useRef, useState } from "react";
import { useBoolean, useDebouncedCallback } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { toast } from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import { DST, MarkPrices } from "../types";
import { useWooCrossSwapQuery } from "./useCrossSwapQuery";
import { useWooSwapQuery } from "./useSwapQuery";

export type TransactionInfo = {
  price: number;
  markPrices: MarkPrices;
  dstGasFee?: string;
  swapFee?: string;
  bridgeFee?: string;
  fee: string;
};

type useSwapEnquiryProps = {
  quantity: string;
  dst: DST;
  queryParams: any;
  needSwap: boolean;
  needCrossSwap: boolean;
};

export function useSwapEnquiry(props: useSwapEnquiryProps) {
  const { quantity, dst, queryParams, needSwap, needCrossSwap } = props;
  const { t } = useTranslation();
  const { query: wooSwapQuery } = useWooSwapQuery();
  const { query: wooCrossSwapQuery } = useWooCrossSwapQuery();

  const queryParamsRef = useRef(queryParams);
  queryParamsRef.current = queryParams;

  const [amount, setAmount] = useState<string>("");
  const [warningMessage, setWarningMessage] = useState<string>("");
  const [querying, { setTrue: queryStart, setFalse: queryStop }] =
    useBoolean(false);

  const [transactionInfo, setTransactionInfo] = useState<TransactionInfo>({
    price: 0,
    markPrices: {
      from_token: 0,
      native_token: 0,
    },
    fee: "",
    // dstGasFee: "",
    // swapFee: "",
    // bridgeFee: "",
  });

  const enquiry = useCallback(() => {
    if (needCrossSwap) {
      return wooCrossSwapQuery(queryParamsRef.current);
    }

    if (needSwap) {
      return wooSwapQuery(queryParamsRef.current);
    }

    return Promise.reject("no need to enquiry");
  }, [needSwap, needCrossSwap]);

  const enquirySuccessHandle = (res: any) => {
    if (res.mark_prices) {
      const dstGasFee = needCrossSwap ? res.dst_infos.gas_fee : "0";
      const swapFee = needCrossSwap ? res.fees_from.woofi : res.fees_from;
      const bridgeFee = needCrossSwap ? res.fees_from.stargate : undefined;
      const fee = needCrossSwap ? res.fees_from.total : res.fees_from;

      setTransactionInfo({
        price: res.price,
        markPrices: res.mark_prices,
        dstGasFee,
        swapFee,
        bridgeFee,
        fee: fee,
      });
    }

    // set amount
    if (res.route_infos) {
      const amountValue = needCrossSwap
        ? res.route_infos.dst.amounts[1]
        : res.route_infos.amounts[1];

      setAmount(
        new Decimal(amountValue).div(Math.pow(10, dst.decimals!)).toString(),
      );
    }

    setWarningMessage("");
    return res;
  };

  const cleanTransactionInfo = (data?: any) => {
    setTransactionInfo({
      fee: "0",
      markPrices: {
        from_token: 0,
        native_token: 0,
      },
      price: 0,
      swapFee: "0",
      bridgeFee: "0",
      dstGasFee: "0",
      ...data,
    });

    setAmount("");
  };

  const enquiryErrorHandle = (error: Error) => {
    if (error.message === "contract call failed") {
      // Not enough liquidity on the cross-chain bridge.
      setWarningMessage(t("transfer.swapDeposit.notEnoughLiquidity"));
      // clean previous data
      cleanTransactionInfo();
    } else {
      setWarningMessage("");
      if (error?.message) {
        toast.error(error.message);
      }
    }
  };

  const debouncedEnquiry = useDebouncedCallback(() => {
    queryStart();
    return enquiry()
      .then(enquirySuccessHandle, enquiryErrorHandle)
      .finally(() => {
        queryStop();
      });
  }, 300);

  useEffect(() => {
    // if not need swap and cross swap
    if (!needSwap && !needCrossSwap) {
      cleanTransactionInfo({
        price: 1,
      });
      setAmount(quantity);
      return;
    }

    const qty = Number(quantity);

    if (isNaN(qty) || qty <= 0) {
      cleanTransactionInfo();
      return;
    }

    debouncedEnquiry();
  }, [needSwap, needCrossSwap, quantity]);

  return {
    enquiry,
    querying,
    warningMessage,
    transactionInfo,
    amount,
    cleanTransactionInfo,
  };
}
