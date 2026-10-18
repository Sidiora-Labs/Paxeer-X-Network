import { FC, useCallback, useEffect, useMemo, useState } from "react";
import { useEventEmitter } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { WS_WalletStatusEnum } from "@paxeer-network/types";
import { Box, toast } from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import { useSwap } from "../hooks/useSwap";
import { SwapMode, SwapProcessStatus } from "../types";
import { ProcessStatus } from "./processStatus";
import { SwapProps } from "./swap";
import { SwapDetail } from "./swapDetail";
import { ViewFAQs } from "./viewFAQs";

export const SingleSwap: FC<SwapProps> = (props) => {
  const {
    transactionData: transaction,
    slippage,
    mode,
    dst,
    src,
    chain,
    nativeToken,
    depositFee,
  } = props;
  const { t } = useTranslation();

  const [status, setStatus] = useState<SwapProcessStatus>(
    SwapProcessStatus.NONE,
  );

  const [view, setView] = useState<"processing" | "details">("details");
  const [tx, setTx] = useState<any>();

  const ee = useEventEmitter();

  const { swap: doSingleSwap, status: swapStatus } = useSwap();

  const swapInfo = useMemo(() => {
    const info: any = {
      price: transaction.price,
      slippage,
      time: chain?.est_txn_mins,
      received: dst.amount,
      dstGasFee: new Decimal(depositFee!.toString())
        ?.div(new Decimal(10).pow(18))
        ?.toString(),
      swapFee: transaction.fees_from,
    };

    return info;
  }, [transaction, chain?.est_txn_mins, mode, dst, depositFee]);

  useEffect(() => {
    if (swapStatus === WS_WalletStatusEnum.COMPLETED) {
      setStatus(SwapProcessStatus.Done);
    }

    if (swapStatus === WS_WalletStatusEnum.FAILED) {
      setStatus(SwapProcessStatus.DepositFailed);
    }
  }, [swapStatus]);

  const doSwap = useCallback(() => {
    setView("processing");

    if (!transaction) return Promise.reject("No transaction data");
    if (!chain || !chain.depositor) return Promise.reject("No chain data");

    setStatus(SwapProcessStatus.Depositing);

    return doSingleSwap(
      chain.depositor,
      {
        fromToken: transaction.infos.from_token,
        fromAmount: transaction.infos.from_amount,
        toToken: transaction.infos.to_token,
        minToAmount: transaction.infos.min_to_amount,
        paxeerNetworkNativeFees: depositFee,
      },
      { dst, src },
    )
      .then((res: any) => {
        setTx(res);
        toast.success(t("transfer.deposit.requested"));
        ee.emit("deposit:requested");
      })
      .catch((error: any) => {
        setStatus(SwapProcessStatus.DepositFailed);
        toast.error(error?.message || "Error");
      });
  }, [chain, depositFee, dst, ee, mode, src, t, transaction]);

  const statusUrl = useMemo(() => {
    if (status < SwapProcessStatus.Depositing || !tx) {
      return;
    }
    return `${chain?.explorer_base_url}/tx/${tx.hash}`;
  }, [status, tx, chain]);

  return (
    <Box intensity={800}>
      <SwapDetail
        viewMode={view}
        onConfirm={doSwap}
        info={swapInfo}
        src={props.src}
        dst={props.dst}
        mode={mode}
        markPrice={transaction.mark_prices?.from_token ?? 1}
        nativePrice={transaction.mark_prices.native_token}
        nativeToken={nativeToken}
      />

      {view === "processing" && (
        <ProcessStatus
          mode={SwapMode.Single}
          status={status}
          statusUrl={statusUrl}
          onComplete={props.onComplete}
          brokerName={props.brokerName}
        />
      )}
      <ViewFAQs />
    </Box>
  );
};
