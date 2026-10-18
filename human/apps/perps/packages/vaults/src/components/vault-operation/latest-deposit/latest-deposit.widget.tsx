import { FC } from "react";
import { useLatestDepositScript } from "./latest-deposit.script";
import { LatestDepositUI } from "./latest-deposit.ui";

export type LatestDepositWidgetProps = {
  vaultId: string;
};

export const LatestDepositWidget: FC<LatestDepositWidgetProps> = (props) => {
  const { vaultId } = props;
  const state = useLatestDepositScript({ vaultId });
  return <LatestDepositUI {...state} />;
};
