import { FC } from "react";
import { VaultInfo } from "../../types/vault";
import { useVaultCardScript } from "./vaultCard.script";
import { VaultCard } from "./vaultCard.ui";

export type VaultCardWidgetProps = {
  vault: VaultInfo;
};

export const VaultCardWidget: FC<VaultCardWidgetProps> = (props) => {
  const { vault } = props;
  const state = useVaultCardScript(vault);

  return <VaultCard {...state} />;
};
