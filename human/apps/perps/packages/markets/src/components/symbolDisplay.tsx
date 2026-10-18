import { memo } from "react";
import { useSymbolsInfo } from "@paxeer-network/hooks";
import { Text, FormattedTextProps, TokenIcon, Flex } from "@paxeer-network/ui";
import { RwaDotTooltip } from "./rwaDotTooltip";
import { SymbolBadge } from "./symbolBadge";

type SymbolDisplayProps = FormattedTextProps & {
  showBadge?: boolean;
  /** Market row data used for RWA status fields. */
  record?: any;
};

export const SymbolDisplay = memo((props: SymbolDisplayProps) => {
  const { children, size = "xs", showBadge = true, record, ...rest } = props;
  const symbol = children as string;

  const symbolsInfo = useSymbolsInfo();
  const info = symbolsInfo[symbol];
  const displayName = info("display_symbol_name")?.trim?.();

  const suffix =
    record?.isRwa || showBadge ? (
      <Flex as="span" display="inlineFlex" gapX={0} itemAlign="center">
        {record?.isRwa && <RwaDotTooltip record={record} />}
        {showBadge && <SymbolBadge symbol={symbol} />}
      </Flex>
    ) : null;

  if (displayName) {
    return (
      <Flex gapX={1} className={props.className}>
        {props.showIcon && <TokenIcon size={size as any} symbol={symbol} />}
        <Text
          size="xs"
          weight="semibold"
          className="oui-whitespace-nowrap oui-break-normal"
        >
          {displayName}
        </Text>
        {suffix}
      </Flex>
    );
  }

  return (
    <Text.formatted
      size="xs"
      rule="symbol"
      weight="semibold"
      suffix={suffix}
      {...rest}
    >
      {symbol}
    </Text.formatted>
  );
});
