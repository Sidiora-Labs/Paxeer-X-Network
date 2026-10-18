import React from "react";
// import { OrderEntry } from "./orderEntry.ui";
import { InjectableOrderEntry } from "./orderEntry.injectable";
import {
  OrderEntryScriptInputs,
  useOrderEntryScript,
} from "./orderEntry.script";

export const OrderEntryWidget: React.FC<
  OrderEntryScriptInputs & {
    containerRef?: React.RefObject<HTMLDivElement>;
    disableFeatures?: ("slippageSetting" | "feesInfo")[];
  }
> = (props) => {
  const state = useOrderEntryScript(props);
  return (
    <InjectableOrderEntry
      {...state}
      containerRef={props.containerRef}
      disableFeatures={props.disableFeatures}
    />
  );
};
