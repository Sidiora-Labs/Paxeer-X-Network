import React from "react";
import { NumeralProps, Text } from "@paxeer-network/ui";
import { useSymbolContext } from "../../../provider/symbolContext";

export type TickName = "quote_dp" | "base_dp";

export const NumeralWithCtx: React.FC<
  React.PropsWithChildren<
    Omit<NumeralProps, "precision" | "tick"> & { tick?: TickName }
  >
> = (props) => {
  const { tick = "quote_dp", children, ...rest } = props;
  const symbolInfo = useSymbolContext();

  if (!symbolInfo) {
    // TODO i18n
    throw new Error("NumeralWithCtx must be used inside SymbolProvider");
  }

  return (
    <Text.numeral as={"span" as any} dp={symbolInfo[tick]} {...rest}>
      {children}
    </Text.numeral>
  );
};
