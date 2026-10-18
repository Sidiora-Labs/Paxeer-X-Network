import { FC } from "react";
import {
  useVaultDepositFormScript,
  VaultDepositWidgetProps,
} from "./vault-deposit-form.script";
import { VaultDepositForm } from "./vault-deposit-form.ui";

export const VaultDepositWidget: FC<VaultDepositWidgetProps> = (props) => {
  const { vaultId } = props;
  const state = useVaultDepositFormScript({ vaultId });
  return <VaultDepositForm {...state} />;
};
