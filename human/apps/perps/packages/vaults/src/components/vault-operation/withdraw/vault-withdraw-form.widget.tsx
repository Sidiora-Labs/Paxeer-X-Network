import { FC } from "react";
import { useVaultWithdrawFormScript } from "./vault-withdraw-form.script";
import { VaultWithdrawForm } from "./vault-withdraw-form.ui";

export type VaultWithdrawWidgetProps = {
  vaultId: string;
};

export const VaultWithdrawWidget: FC<VaultWithdrawWidgetProps> = (props) => {
  const { vaultId } = props;
  const state = useVaultWithdrawFormScript({ vaultId });
  return <VaultWithdrawForm {...state} />;
};
