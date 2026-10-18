import { useScreen } from "@paxeer-network/ui";
import { useOnboardingModal } from "@paxeer-network/ui-connector";
import { useChainMenuScript } from "./chainMenu.script";
import { ChainMenu } from "./chainMenu.ui";
import { ChainMenuUiMobile } from "./chainMenu.ui.mobile";

export const ChainMenuWidget = () => {
  const { isMobile } = useScreen();
  const { handleAccountStatus } = useOnboardingModal();
  const state = useChainMenuScript({
    onAccountValidated: handleAccountStatus,
  });

  if (isMobile) {
    return <ChainMenuUiMobile {...state} />;
  }
  return <ChainMenu {...state} />;
};
