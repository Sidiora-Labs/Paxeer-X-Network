import { FC } from "react";
import {
  useRefereesTableScript,
  UseRefereesTableScriptProps,
} from "./refereesTable.script";
import { RefereesTableUI } from "./refereesTable.ui";

export const RefereesTableWidget: FC<UseRefereesTableScriptProps> = (props) => {
  const state = useRefereesTableScript(props);
  return <RefereesTableUI {...state} />;
};
