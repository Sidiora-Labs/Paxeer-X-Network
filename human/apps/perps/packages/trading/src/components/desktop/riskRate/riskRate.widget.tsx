import React from "react";
import { useRiskRateScript } from "./riskRate.script";
import { RiskRate } from "./riskRate.ui";

export const RiskRateWidget: React.FC = () => {
  const state = useRiskRateScript();
  return <RiskRate {...state} />;
};
