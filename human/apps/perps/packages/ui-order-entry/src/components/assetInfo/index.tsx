import { useGetEstLiqPrice } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { OrderSide, OrderType } from "@paxeer-network/types";
import { Flex, Text, Tips, cn, useScreen } from "@paxeer-network/ui";
import { FeesWidget } from "../fee";
import { SlippageUI } from "../slippage/slippage.ui";

export function AssetInfo(props: {
  symbol: string;
  canTrade: boolean;
  quote: string;
  estLiqPrice: number | null;
  estLiqPriceDistance: number | null;
  estLeverage: number | null;
  currentLeverage: number | null;
  slippage: string;
  dp: number;
  estSlippage: number | null;
  setSlippage: (slippage: string) => void;
  orderType: OrderType;
  disableFeatures?: ("slippageSetting" | "feesInfo")[];
  side: OrderSide;
}) {
  const { canTrade, disableFeatures, orderType, symbol } = props;
  const { t } = useTranslation();
  const { isMobile } = useScreen();

  const displayEstLiqPrice = useGetEstLiqPrice({
    estLiqPrice: props.estLiqPrice,
    symbol: symbol,
    side: props.side,
  });

  const estLiqTipsContent = (
    <div className="oui-min-w-[204px] oui-max-w-[280px] oui-text-2xs oui-leading-normal oui-text-base-contrast-80">
      <div className="oui-text-pretty">
        {t("common.liquidationPrice.tooltip")}
      </div>
      <div>
        <a
          href="https://paxeer-network.network/docs/introduction/trade-on-paxeer-network/perpetual-futures/liquidations"
          target="_blank"
          rel="noopener noreferrer"
          className="oui-text-primary oui-underline"
        >
          {t("common.liquidationPrice.tooltip.learnMore")}
        </a>
      </div>
    </div>
  );

  return (
    <div
      className={"oui-orderEntry-assetInfo oui-space-y-[2px] xl:oui-space-y-1"}
    >
      <Flex justify={"between"} className="oui-orderEntry-estLiqPrice">
        <Tips
          title={t("common.tips")}
          content={estLiqTipsContent}
          trigger={
            <Text
              size="2xs"
              className={cn(
                "oui-estLiqPrice-label oui-cursor-pointer",
                "oui-border-b oui-border-dashed oui-border-line-12",
              )}
            >
              {t("orderEntry.estLiqPrice")}
            </Text>
          }
        />
        <Text.numeral
          unit={props.quote}
          size={"2xs"}
          dp={props.dp}
          className={"oui-estLiqPrice-value oui-text-base-contrast-80"}
          unitClassName={"oui-ms-1 oui-text-base-contrast-36"}
        >
          {canTrade ? (displayEstLiqPrice ?? "--") : "--"}
        </Text.numeral>
      </Flex>

      {orderType === OrderType.MARKET &&
        !disableFeatures?.includes("slippageSetting") && (
          <SlippageUI
            slippage={props.slippage}
            setSlippage={props.setSlippage}
            estSlippage={props.estSlippage}
          />
        )}

      {!disableFeatures?.includes("feesInfo") && (
        <FeesWidget symbol={props.symbol} />
      )}
    </div>
  );
}
