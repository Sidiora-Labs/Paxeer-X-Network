import { FC } from "react";
import { VaultsIntroductionDesktop } from "./vaults-introduction.desktop";
import { useVaultsIntroductionScript } from "./vaults-introduction.script";

export const VaultsIntroductionWidget: FC = () => {
  const state = useVaultsIntroductionScript();
  return <VaultsIntroductionDesktop {...state} />;
};
