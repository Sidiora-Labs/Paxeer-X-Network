import { useState } from "react";
import { FundingTabName } from "../../../type";

export type FundingScriptReturn = ReturnType<typeof useFundingScript>;

export function useFundingScript() {
  const [activeTab, setActiveTab] = useState<FundingTabName>(
    FundingTabName.Overview,
  );

  return {
    activeTab,
    onTabChange: setActiveTab,
  };
}
