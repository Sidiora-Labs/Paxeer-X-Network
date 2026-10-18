import { FC, useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { OrderEntity, OrderSide } from "@paxeer-network/types";
import {
  Button,
  Text,
  CloseIcon,
  Flex,
  Divider,
  Badge,
  ThrottledButton,
  Box,
} from "@paxeer-network/ui";
import { commify, commifyOptional, Decimal } from "@paxeer-network/utils";
import { SymbolBadge } from "../../desktop/symbolBadge";

export const ConfirmHeader: FC<{
  onClose?: () => void;
  title: string;
  hideCloseIcon?: boolean;
}> = (props) => {
  const { hideCloseIcon = false } = props;
  return (
    <div className="oui-relative oui-w-full oui-border-b oui-border-line-4 oui-pb-3">
      <Text size={"base"}>{props.title}</Text>
      {!hideCloseIcon && (
        <button
          onClick={props.onClose}
          className="oui-absolute oui-end-0 oui-top-0 oui-p-2 oui-text-base-contrast-54 hover:oui-text-base-contrast-80"
        >
          <CloseIcon size={18} color="white" />
        </button>
      )}
    </div>
  );
};

export const ConfirmFooter: FC<{
  onConfirm?: () => Promise<any>;
  onCancel?: () => void;
  submitting?: boolean;
  disabled?: boolean;
}> = (props) => {
  const { t } = useTranslation();

  return (
    <Flex
      id="oui-positions-confirm-footer"
      gap={2}
      width={"100%"}
      className="oui-mt-3 oui-pb-1"
    >
      <Button
        id="oui-positions-confirm-footer-cancel-button"
        color={"secondary"}
        fullWidth
        onClick={props.onCancel}
        size="md"
      >
        {t("common.cancel")}
      </Button>
      <ThrottledButton
        id="oui-positions-confirm-footer-confirm-button"
        onClick={props.onConfirm}
        fullWidth
        loading={props.submitting}
        disabled={props.disabled}
        size="md"
      >
        {t("common.confirm")}
      </ThrottledButton>
    </Flex>
  );
};

export const OrderDetail = (props: {
  quantity: any;
  price: any;
  side: OrderSide;
  quoteDp: number;
  className?: string;
}) => {
  const { quantity, price, quoteDp, side } = props;
  const { t } = useTranslation();

  const total = useMemo(() => {
    if (price && quantity) {
      return new Decimal(price)
        .mul(quantity)
        .toFixed(quoteDp, Decimal.ROUND_DOWN);
    }
    return "--";
  }, [price, quantity]);

  return (
    <Flex
      direction={"column"}
      gap={1}
      width={"100%"}
      className="oui-text-sm oui-text-base-contrast-54"
      py={5}
    >
      <Flex justify={"between"} width={"100%"} gap={1}>
        <Text>{t("common.qty")}</Text>
        <Text color={side === OrderSide.BUY ? "success" : "danger"}>
          {quantity}
        </Text>
      </Flex>
      <Flex justify={"between"} width={"100%"} gap={1}>
        <Text>{t("common.price")}</Text>
        <Text.formatted
          intensity={98}
          suffix={<Text intensity={54}>USDC</Text>}
        >
          {price}
        </Text.formatted>
      </Flex>
      <Flex justify={"between"} width={"100%"} gap={1}>
        <Text>{t("common.notional")}</Text>
        <Text.formatted
          intensity={98}
          suffix={<Text intensity={54}>USDC</Text>}
        >
          {total}
        </Text.formatted>
      </Flex>
    </Flex>
  );
};

export const MarketCloseConfirm: FC<{
  base?: string;
  quantity?: string;
  onClose?: () => void;
  close?: () => void;
  onConfirm?: () => Promise<any>;
  submitting?: boolean;
  hideCloseIcon?: boolean;
  classNames?: {
    root?: string;
  };
}> = (props) => {
  const { t } = useTranslation();

  const onCancel = () => {
    const func = props?.onClose ?? props.close;
    func?.();
  };
  return (
    <Flex direction={"column"} className={props.classNames?.root}>
      <ConfirmHeader
        onClose={onCancel}
        title={t("positions.marketClose")}
        hideCloseIcon={props.hideCloseIcon}
      />
      <Text intensity={54} size="sm" className="oui-my-5">
        {t("positions.marketClose.description", {
          quantity: commifyOptional(props.quantity),
          base: props.base,
        })}
      </Text>
      <ConfirmFooter
        onCancel={onCancel}
        onConfirm={async () => {
          await props.onConfirm?.();
          onCancel();
        }}
        submitting={props.submitting}
      />
    </Flex>
  );
};

export const LimitConfirmDialog: FC<{
  base: string;
  quantity: string;
  price: string;
  onClose?: () => void;
  onConfirm: () => Promise<any>;
  order: OrderEntity;
  submitting: boolean;
  quoteDp?: number;
  hideCloseIcon?: boolean;
}> = (props) => {
  const { order, quoteDp, quantity, price } = props;
  const { side } = order;
  const { t } = useTranslation();

  const onCancel = () => {
    props.onClose?.();
  };

  return (
    <>
      <ConfirmHeader
        onClose={onCancel}
        title={t("positions.limitClose")}
        hideCloseIcon={props.hideCloseIcon}
      />
      <Box mt={5}>
        <Text intensity={54} size="sm">
          {t("positions.limitClose.description", {
            quantity: commify(props.quantity),
            base: props.base,
          })}
        </Text>
      </Box>

      <Flex gap={2} mb={4} mt={5} justify={"between"}>
        <Text.formatted
          rule="symbol"
          formatString="base"
          size="base"
          showIcon
          suffix={<SymbolBadge symbol={order.symbol} />}
        >
          {order.symbol}
        </Text.formatted>
        <Flex gap={1}>
          <Badge color="neutral" size="xs">
            {t("orderEntry.orderType.limit")}
          </Badge>
          <Badge
            color={side === OrderSide.BUY ? "success" : "danger"}
            size="xs"
          >
            {side === OrderSide.BUY ? t("common.buy") : t("common.sell")}
          </Badge>
        </Flex>
      </Flex>

      <Divider className="oui-w-full" />

      <OrderDetail
        className="oui-text-sm"
        price={price}
        quantity={quantity}
        side={order.side}
        quoteDp={quoteDp ?? 2}
      />
      <ConfirmFooter
        onCancel={onCancel}
        onConfirm={props.onConfirm}
        submitting={props.submitting}
      />
    </>
  );
};
