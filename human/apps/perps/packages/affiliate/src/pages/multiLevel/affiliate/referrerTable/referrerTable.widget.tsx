import { FC } from "react";
import { useReferrerTableScript } from "./referrerTable.script";
import { ReferrerTableUI } from "./referrerTable.ui";

export const ReferrerTableWidget: FC = () => {
  const state = useReferrerTableScript();
  return <ReferrerTableUI {...state} />;
};
