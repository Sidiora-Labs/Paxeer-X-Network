import React from "react";
import { useAvailableScript } from "./availableToClaim.script";
import { AvailableToClaim } from "./availableToClaim.ui";

export const AvailableToClaimWidget: React.FC = () => {
  const state = useAvailableScript();
  return <AvailableToClaim {...state} />;
};
