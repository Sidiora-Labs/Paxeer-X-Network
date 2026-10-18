import { FC } from "react";
import { useLatestWithdrawScript } from "./latest-withdraw-script";
import { LatestWithdrawUI } from "./latest-withdraw-ui";

export type LatestWithdrawWidgetProps = {
  vaultId: string;
};

export const LatestWithdrawWidget: FC<LatestWithdrawWidgetProps> = (props) => {
  const { vaultId } = props;
  const state = useLatestWithdrawScript({ vaultId });
  return <LatestWithdrawUI {...state} />;
};
