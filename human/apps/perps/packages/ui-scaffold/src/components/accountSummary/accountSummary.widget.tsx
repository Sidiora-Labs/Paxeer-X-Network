import React from "react";
import { AccountSummary } from "./accountSummary.ui";
import type { AccountSummaryList } from "./accountSummary.ui";
import { useTotalValueBuilderScript } from "./useWidgetBuilder.script";

export const AccountSummaryWidget: React.FC = () => {
  const state = useTotalValueBuilderScript();
  return (
    <AccountSummary
      {...state}
      elementKeys={state.elementKeys as AccountSummaryList}
    />
  );
};
