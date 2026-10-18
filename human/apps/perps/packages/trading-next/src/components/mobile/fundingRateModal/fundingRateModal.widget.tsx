import React from "react";
import { i18n } from "@paxeer-network/i18n";
import { registerSimpleDialog, registerSimpleSheet } from "@paxeer-network/ui";
import { useFundingRateModalScript } from "./fundingRateModal.script";
import { FundingRateModal } from "./fundingRateModal.ui";

export const FundingRateModalWidget: React.FC<{ symbol: string }> = (props) => {
  const state = useFundingRateModalScript(props);
  return <FundingRateModal {...state} />;
};

export const FundingRateDialogId = "FundingRateDialogId";
export const FundingRateSheetId = "FundingRateSheetId";

registerSimpleDialog(FundingRateDialogId, FundingRateModalWidget, {
  size: "md",
  classNames: { content: "oui-border oui-border-line-6" },
  title: () => i18n.t("funding.fundingRate"),
});

registerSimpleSheet(FundingRateSheetId, FundingRateModalWidget, {
  classNames: { content: "oui-text-sm" },
  title: () => i18n.t("funding.fundingRate"),
});
