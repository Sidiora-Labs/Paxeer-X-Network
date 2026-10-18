import { FC } from "react";
import { Text, TokenIcon } from "@paxeer-network/ui";
import { SymbolBadge } from "../../desktop/symbolBadge";

export interface SymbolInfoProps {
  symbol: string;
}

export const SymbolInfo: FC<SymbolInfoProps> = ({ symbol }) => {
  return (
    <div className="oui-flex oui-items-center oui-gap-2">
      <TokenIcon symbol={symbol} className="oui-size-5" />
      <Text.formatted
        rule="symbol"
        formatString="base"
        size="base"
        weight="semibold"
        intensity={98}
        suffix={<SymbolBadge symbol={symbol} />}
      >
        {symbol}
      </Text.formatted>
    </div>
  );
};
