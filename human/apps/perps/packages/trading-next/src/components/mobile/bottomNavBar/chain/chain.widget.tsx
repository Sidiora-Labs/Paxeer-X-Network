import React from "react";
import { useOnboardingModal } from "@paxeer-network/ui-connector";
import { useChainScript } from "./chain.script";
import { Chain } from "./chain.ui";

export const ChainWidget: React.FC = () => {
  const { handleAccountStatus } = useOnboardingModal();
  const state = useChainScript({
    onAccountValidated: handleAccountStatus,
  });
  return <Chain {...state} />;
};
