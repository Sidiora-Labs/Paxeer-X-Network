import { FC, useMemo } from "react";
import { useIndexPricesStream, useWithdraw } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { API } from "@paxeer-network/types";
import {
  Flex,
  Spinner,
  Text,
  Tooltip,
  modal,
  useScreen,
  useDocumentDirection,
  cn,
} from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";

export type AvailableQuantityProps = {
  token?: Partial<API.TokenInfo>;
  quantity?: string | number;
  maxQuantity?: string | number;
  onClick?: () => void;
  loading?: boolean;
  tooltipContent?: React.ReactNode;
  notional?: number;
  /**
   * Token label display mode.
   * - `false` (default): platform-side paxeer-network account assets — symbol only
   * - `true`: wallet-side chain assets — `display_name || symbol`
   */
  showDisplayName?: boolean;
};

type AvailableTooltipMessageProps = {
  tokenSymbol?: string;
  decimals?: number;
};

const AvailableTooltipMessage: FC<AvailableTooltipMessageProps> = ({
  tokenSymbol,
  decimals,
}) => {
  const { t } = useTranslation();
  const { maxAmount } = useWithdraw({
    token: tokenSymbol,
    decimals,
  });

  const amountText = useMemo(() => {
    if (maxAmount === undefined || maxAmount === null) return "--";
    return maxAmount.toString();
  }, [maxAmount]);

  return (
    <Text size="2xs" intensity={80}>
      {t("transfer.withdraw.available.tooltip", { amount: amountText })}
    </Text>
  );
};

export const AvailableQuantity: FC<AvailableQuantityProps> = (props) => {
  const {
    quantity,
    maxQuantity,
    token,
    loading,
    showDisplayName = false,
  } = props;
  const { t } = useTranslation();
  const { isMobile } = useScreen();
  const dir = useDocumentDirection();
  const isRTL = dir === "rtl";

  const { getIndexPrice } = useIndexPricesStream();

  const name = showDisplayName
    ? token?.display_name || token?.symbol || ""
    : token?.symbol || "";
  const dp = token?.precision ?? token?.decimals ?? 2;

  const notional = useMemo(() => {
    if (props.notional !== undefined && props.notional !== null) {
      return props.notional;
    }

    if (quantity && token?.symbol) {
      return new Decimal(quantity)
        .mul(getIndexPrice(token?.symbol) || 1)
        .toNumber();
    }
    return 0;
  }, [quantity, token?.symbol, props.notional]);

  return (
    <Flex px={2}>
      <Text size="2xs" intensity={36}>
        $
        <Text.numeral dp={2} padding={false} rm={Decimal.ROUND_DOWN}>
          {notional}
        </Text.numeral>
      </Text>

      <Flex
        gapX={2}
        itemAlign="center"
        className={cn("oui-ms-auto", isRTL && "oui-flex-row-reverse")}
      >
        <Flex
          gapX={1}
          itemAlign="center"
          className={cn(isRTL && "oui-flex-row-reverse")}
        >
          {props.tooltipContent ? (
            isMobile ? (
              <button
                type="button"
                className="oui-p-0"
                onClick={() => {
                  if (token?.symbol) {
                    const anyToken = token as any;
                    modal.alert({
                      title: t("common.tips"),
                      message: (
                        <AvailableTooltipMessage
                          tokenSymbol={token.symbol}
                          decimals={
                            anyToken?.token_decimal ??
                            token.decimals ??
                            token.precision
                          }
                        />
                      ),
                    });
                  } else {
                    modal.alert({
                      title: t("common.tips"),
                      message: props.tooltipContent,
                    });
                  }
                }}
              >
                <Text
                  size="2xs"
                  intensity={36}
                  className="oui-cursor-pointer oui-border-b oui-border-dashed oui-border-line-12"
                >
                  {`${t("common.available")}: `}
                </Text>
              </button>
            ) : (
              <Tooltip
                content={props.tooltipContent}
                className="oui-max-w-[274px]"
              >
                <Text
                  size="2xs"
                  intensity={36}
                  className="oui-cursor-pointer oui-border-b oui-border-dashed oui-border-line-12"
                >
                  {`${t("common.available")}: `}
                </Text>
              </Tooltip>
            )
          ) : (
            <Text size="2xs" intensity={36}>
              {`${t("common.available")}: `}
            </Text>
          )}

          {loading ? (
            <Spinner size="sm" />
          ) : (
            <Text.numeral
              size="2xs"
              intensity={36}
              rm={Decimal.ROUND_DOWN}
              dp={dp}
              padding={false}
              data-testid="oui-testid-withdraw_deposit-dialog-available-value"
            >
              {maxQuantity!}
            </Text.numeral>
          )}

          <Text size="2xs" intensity={36}>
            {` ${name}`}
          </Text>
        </Flex>

        <Text
          size="2xs"
          color="primary"
          className={cn("oui-cursor-pointer oui-select-none")}
          onClick={props.onClick}
        >
          {t("common.max")}
        </Text>
      </Flex>
    </Flex>
  );
};
