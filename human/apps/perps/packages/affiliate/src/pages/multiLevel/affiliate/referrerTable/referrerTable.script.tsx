import { useState } from "react";

export type ReferrerTableTab = "commission" | "referees" | "codes";

export const useReferrerTableScript = () => {
  const [activeTab, _setActiveTab] = useState<ReferrerTableTab>("commission");
  const setActiveTab = (tab: ReferrerTableTab) => _setActiveTab(tab);
  return {
    activeTab,
    setActiveTab,
  };
};

export type ReferrerTableScriptReturns = ReturnType<
  typeof useReferrerTableScript
>;
