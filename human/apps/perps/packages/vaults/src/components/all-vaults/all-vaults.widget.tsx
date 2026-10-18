import { FC } from "react";
import { useScreen } from "@paxeer-network/ui";
import { useVaultInfoState } from "../../store/vaultsStore";
import { AllVaultsDesktop } from "./all-vaults.desktop";
import { AllVaultsMobile } from "./all-vaults.mobile";

export const AllVaultsWidget: FC = () => {
  const { data } = useVaultInfoState();
  const { isMobile } = useScreen();
  return isMobile ? (
    <AllVaultsMobile vaults={data} />
  ) : (
    <AllVaultsDesktop vaults={data} />
  );
};
