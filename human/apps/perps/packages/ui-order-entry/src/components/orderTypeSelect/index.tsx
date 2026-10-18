import { useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { OrderSide, OrderType } from "@paxeer-network/types";
import { cn, modal, Text, Tooltip, useScreen } from "@paxeer-network/ui";
import {
  OrderTypeAdvancedSelectInjectabled,
  OrderTypeMobileSelectInjectabled,
} from "../orderEntry.injectabled";

export const OrderTypeSelect = (props: {
  type: OrderType;
  onChange: (type: OrderType) => void;
  side: OrderSide;
  canTrade: boolean;
  /** When true, Market order type is disabled (e.g. symbol in POST_ONLY mode). */
  marketOrderDisabled?: boolean;
  /** Tooltip text when hovering over the disabled Market button. */
  marketOrderDisabledTooltip?: string;
  /** Active custom order-type id (null for a real OrderType). */
  selectedExtraId?: string | null;
  onExtraSelect?: (id: string | null) => void;
}) => {
  const { t } = useTranslation();
  const { isMobile } = useScreen();
  const { marketOrderDisabled = false, marketOrderDisabledTooltip } = props;

  const allOptions = useMemo(() => {
    return [
      { label: t("orderEntry.orderType.limitOrder"), value: OrderType.LIMIT },
      { label: t("orderEntry.orderType.marketOrder"), value: OrderType.MARKET },
      {
        label: t("orderEntry.orderType.stopLimit"),
        value: OrderType.STOP_LIMIT,
      },
      {
        label: t("orderEntry.orderType.stopMarket"),
        value: OrderType.STOP_MARKET,
      },
      {
        label: t("orderEntry.orderType.scaledOrder"),
        value: OrderType.SCALED,
      },
      {
        label: t("orderEntry.orderType.trailingStop"),
        value: OrderType.TRAILING_STOP,
      },
    ];
  }, [t]);

  const advancedOptions = useMemo(() => {
    return [
      {
        label: t("orderEntry.orderType.stopLimit"),
        value: OrderType.STOP_LIMIT,
      },
      {
        label: t("orderEntry.orderType.stopMarket"),
        value: OrderType.STOP_MARKET,
      },
      { label: t("orderEntry.orderType.scaledOrder"), value: OrderType.SCALED },
      {
        label: t("orderEntry.orderType.trailingStop"),
        value: OrderType.TRAILING_STOP,
      },
    ];
  }, [t]);

  // Single source of truth: built-in types are exactly what appears in the
  // type dropdown. Anything else is treated as a plugin custom-type id.
  const builtInOrderEntryTypes = useMemo(
    () => new Set(allOptions.map((o) => o.value as string)),
    [allOptions],
  );
  const isBuiltInOrderEntryType = (value: string): value is OrderType =>
    builtInOrderEntryTypes.has(value);

  // Must run on every render; do not place after `if (!isMobile) return` or hook order breaks when isMobile toggles.
  const mobileOptions = useMemo(() => allOptions, [allOptions]);

  if (!isMobile) {
    const baseButtonClassName =
      "oui-flex oui-flex-1 oui-items-center oui-justify-center oui-gap-x-1 oui-rounded oui-px-3 oui-py-0.5 oui-text-xs oui-font-semibold oui-h-8";

    const selectedButtonClassName = cn(
      baseButtonClassName,
      "oui-bg-base-5 oui-text-base-contrast",
    );
    const unselectedButtonClassName = cn(
      baseButtonClassName,
      "oui-bg-base-7 oui-text-base-contrast-36",
    );

    const handleChange = (type: OrderType) => {
      props.onExtraSelect?.(null);
      props.onChange(type);
    };

    const advancedItems = advancedOptions.map((o) => ({
      value: o.value as string,
      label: o.label,
    }));
    const advancedValue = props.selectedExtraId ?? props.type;
    const routeAdvancedChange = (value: string) => {
      if (isBuiltInOrderEntryType(value)) {
        props.onExtraSelect?.(null);
        props.onChange(value);
      } else {
        props.onExtraSelect?.(value);
      }
    };

    return (
      <div
        className="oui-flex oui-w-full oui-gap-1"
        data-testid="oui-testid-orderEntry-orderType-desktop"
      >
        <button
          type="button"
          className={
            !props.selectedExtraId && props.type === OrderType.LIMIT
              ? selectedButtonClassName
              : unselectedButtonClassName
          }
          aria-pressed={
            !props.selectedExtraId && props.type === OrderType.LIMIT
          }
          onClick={() => handleChange(OrderType.LIMIT)}
          disabled={!props.canTrade}
          data-testid="oui-testid-orderEntry-orderType-limit"
        >
          <Text size="xs">{t("orderEntry.orderType.limit")}</Text>
        </button>

        {marketOrderDisabled && marketOrderDisabledTooltip ? (
          <Tooltip
            content={marketOrderDisabledTooltip}
            className="oui-max-w-[275px]"
          >
            <span className="oui-inline-flex oui-flex-1">
              <button
                type="button"
                className={unselectedButtonClassName}
                aria-pressed={false}
                disabled
                data-testid="oui-testid-orderEntry-orderType-market"
              >
                <Text size="xs">{t("orderEntry.orderType.market")}</Text>
              </button>
            </span>
          </Tooltip>
        ) : (
          <button
            type="button"
            className={
              !props.selectedExtraId && props.type === OrderType.MARKET
                ? selectedButtonClassName
                : unselectedButtonClassName
            }
            aria-pressed={
              !props.selectedExtraId && props.type === OrderType.MARKET
            }
            onClick={() => handleChange(OrderType.MARKET)}
            disabled={!props.canTrade}
            data-testid="oui-testid-orderEntry-orderType-market"
          >
            <Text size="xs">{t("orderEntry.orderType.market")}</Text>
          </button>
        )}

        <div
          className="oui-flex-1"
          data-testid="oui-testid-orderEntry-orderType-advanced"
        >
          <OrderTypeAdvancedSelectInjectabled
            items={advancedItems}
            value={advancedValue}
            placeholder={t("trading.layout.advanced")}
            disabled={!props.canTrade}
            onValueChange={routeAdvancedChange}
          />
        </div>
      </div>
    );
  }

  const mobileItems = mobileOptions.map((o) => ({
    value: o.value as string,
    label: o.label,
  }));
  const mobileValue = props.selectedExtraId ?? props.type;
  const routeMobileChange = (value: string) => {
    if (!isBuiltInOrderEntryType(value)) {
      props.onExtraSelect?.(value);
      return;
    }
    if (
      marketOrderDisabled &&
      value === OrderType.MARKET &&
      marketOrderDisabledTooltip
    ) {
      modal.alert({
        title: t("common.tips"),
        message: marketOrderDisabledTooltip,
      });
      return;
    }
    props.onExtraSelect?.(null);
    props.onChange(value);
  };

  return (
    <OrderTypeMobileSelectInjectabled
      items={mobileItems}
      value={mobileValue}
      disabled={!props.canTrade}
      onValueChange={routeMobileChange}
    />
  );
};
