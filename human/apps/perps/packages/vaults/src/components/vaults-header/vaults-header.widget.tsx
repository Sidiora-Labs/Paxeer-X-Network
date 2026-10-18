import { FC } from "react";
import { useScreen } from "@paxeer-network/ui";
import { VaultsHeaderDesktop } from "./vaults-header.desktop";
import { VaultsHeaderMobile } from "./vaults-header.mobile";
import { useVaultsHeaderScript } from "./vaults-header.script";

export const VaultsHeaderWidget: FC = () => {
  const state = useVaultsHeaderScript();
  const { isMobile } = useScreen();
  return isMobile ? (
    <VaultsHeaderMobile {...state} />
  ) : (
    <VaultsHeaderDesktop {...state} />
  );
};
