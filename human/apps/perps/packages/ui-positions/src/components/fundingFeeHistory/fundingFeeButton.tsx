import { FC, useState } from "react";
import { useBoolean } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { MarginMode } from "@paxeer-network/types";
import {
  SimpleDialog,
  SimpleSheet,
  Text,
  useScreen,
} from "@paxeer-network/ui";
import { FundingFeeHistoryUI } from "./fundingFeeHistory.ui";

type FundingFeeButtonProps = {
  fee?: number;
  symbol: string;
  start_t: string;
  marginMode?: MarginMode | 0 | 1;
} & (
  | {
      feeType?: "closed";
      end_t: string;
    }
  | {
      feeType: "unsettled";
      end_t?: string;
    }
);

export const FundingFeeButton: FC<FundingFeeButtonProps> = ({
  fee,
  symbol,
  start_t,
  end_t,
  feeType = "closed",
  marginMode,
}) => {
  const { t } = useTranslation();
  const [isOpen, { setTrue, setFalse }] = useBoolean(false);
  const [requestEndTime, setRequestEndTime] = useState(end_t);
  const { isMobile } = useScreen();

  const openFundingFeeHistory = () => {
    setRequestEndTime(end_t ?? Date.now().toString());
    setTrue();
  };

  const history = isOpen && requestEndTime && (
    <FundingFeeHistoryUI
      total={fee}
      symbol={symbol}
      start_t={start_t}
      end_t={requestEndTime}
      feeType={feeType}
      marginMode={marginMode}
    />
  );

  return (
    <>
      <button onClick={openFundingFeeHistory}>
        <Text.numeral
          rule="price"
          coloring
          showIdentifier
          ignoreDP
          className="oui-border-b oui-border-line-16 oui-border-dashed oui-py-0.5"
        >
          {fee ?? "--"}
        </Text.numeral>
      </button>
      {isMobile ? (
        <SimpleSheet
          open={isOpen}
          onOpenChange={setFalse}
          title={t("funding.fundingFee")}
          classNames={{
            body: "oui-max-h-[80vh] oui-py-0",
          }}
        >
          {history}
        </SimpleSheet>
      ) : (
        <SimpleDialog
          open={isOpen}
          onOpenChange={setFalse}
          title={t("funding.fundingFee")}
          classNames={{
            content: "lg:oui-max-w-[640px]",
            body: "oui-max-h-[80vh] oui-bg-base-8 lg:oui-py-0",
          }}
        >
          {history}
        </SimpleDialog>
      )}
    </>
  );
};
