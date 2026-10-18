import { FC } from "react";
import {
  useCommissionTableScript,
  UseCommissionTableScriptProps,
} from "./commissionTable.script";
import { CommissionTableUI } from "./commissionTable.ui";

export const CommissionTableWidget: FC<UseCommissionTableScriptProps> = (
  props,
) => {
  const state = useCommissionTableScript(props);
  return <CommissionTableUI {...state} />;
};
