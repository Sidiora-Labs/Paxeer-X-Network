import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  Badge,
  Button,
  cn,
  Divider,
  Flex,
  Input,
  inputFormatter,
  SimpleDialog,
  SimpleSheet,
  Text,
  ThrottledButton,
} from "@paxeer-network/ui";
import { SymbolBadge } from "../desktop/symbolBadge";
import { ClosePositionScriptReturn } from "./closePosition.script";
import {
  LimitConfirmDialog,
  MarketCloseConfirm,
} from "./components/confirmDialog";
import { QuantitySlider } from "./components/quantitySlider";

export const MobileClosePosition: FC<ClosePositionScriptReturn> = (props) => {
  const {
    base,
    base_dp,
    quote,
    quote_dp,
    sheetOpen,
    setSheetOpen,
    dialogOpen,
    setDialogOpen,
    quantity,
    price,
    submitting,
    priceErrorMsg,
    quantityErrorMsg,
    disabled,
    isMarketClose,
    position,
    maxQty,
    isBuy,
  } = props;
  const { t } = useTranslation();

  const title = isMarketClose
    ? t("positions.marketClose")
    : t("positions.limitClose");

  const orderType = isMarketClose
    ? t("orderEntry.orderType.market")
    : t("orderEntry.orderType.limit");

  const orderSide = isBuy ? (
    <Badge color="success" size="xs">
      {t("common.buy")}
    </Badge>
  ) : (
    <Badge color="danger" size="xs">
      {t("common.sell")}
    </Badge>
  );

  const header = (
    <Flex width={"100%"} justify={"between"}>
      <Text.formatted
        rule={"symbol"}
        formatString="base"
        showIcon
        suffix={<SymbolBadge symbol={position.symbol} />}
      >
        {position.symbol}
      </Text.formatted>
      <Flex gap={1}>
        <Badge color="neutral" size="xs">
          {orderType}
        </Badge>
        {orderSide}
      </Flex>
    </Flex>
  );

  const lastPrice = (
    <Flex width={"100%"} justify={"between"}>
      <Text intensity={54}>{t("common.lastPrice")}</Text>
      <Text.numeral dp={quote_dp} suffix={<Text intensity={36}> {quote}</Text>}>
        {position.mark_price}
      </Text.numeral>
    </Flex>
  );

  const inputForm = !isMarketClose && (
    <>
      <Flex width={"100%"} direction={"column"} gap={2}>
        <Input.tooltip
          prefix={t("common.price")}
          suffix={quote}
          align="right"
          fullWidth
          autoComplete="off"
          formatters={[
            inputFormatter.numberFormatter,
            inputFormatter.dpFormatter(quote_dp),
          ]}
          triggerClassName="oui-w-full"
          tooltip={priceErrorMsg}
          color={priceErrorMsg ? "danger" : undefined}
          value={price}
          onValueChange={props.updatePriceChange}
          classNames={{
            prefix: "oui-text-base-contrast-54",
            suffix: "oui-text-base-contrast-54",
            root: cn(
              "oui-w-full oui-outline-line-12",
              priceErrorMsg ? "oui-outline-danger" : undefined,
            ),
          }}
        />
        <Input.tooltip
          prefix={t("common.quantity")}
          suffix={base}
          align="right"
          fullWidth
          autoComplete="off"
          formatters={[
            inputFormatter.numberFormatter,
            inputFormatter.dpFormatter(base_dp),
          ]}
          triggerClassName="oui-w-full"
          tooltip={quantityErrorMsg}
          color={quantityErrorMsg ? "danger" : undefined}
          value={quantity}
          onBlur={() => props.formatQuantityToBaseTick(quantity)}
          onValueChange={props.updateQuantity}
          classNames={{
            prefix: "oui-text-base-contrast-54",
            suffix: "oui-text-base-contrast-54",
            root: cn(
              "oui-w-full oui-outline-line-12",
              quantityErrorMsg ? "oui-outline-danger" : undefined,
            ),
          }}
        />
      </Flex>
    </>
  );

  const slider = (
    <QuantitySlider
      value={props.sliderValue}
      onValueChange={props.onSliderValueChange}
      base_dp={base_dp}
      max={maxQty}
      onMax={props.onMax}
    />
  );

  const footer = (
    <Flex width={"100%"} gap={3} mt={2}>
      <Button fullWidth color="secondary" onClick={props.onCloseSheet}>
        {t("common.cancel")}
      </Button>

      <ThrottledButton
        fullWidth
        disabled={submitting || disabled}
        loading={submitting}
        onClick={(e) => {
          e.stopPropagation();
          e.preventDefault();
          props.onDoubleConfirm();
        }}
      >
        {t("common.confirm")}
      </ThrottledButton>
    </Flex>
  );

  return (
    <>
      <Button
        variant="outlined"
        color="secondary"
        size="sm"
        className="oui-border-base-contrast-36"
        onClick={(e) => {
          e.stopPropagation();
          e.preventDefault();
          setSheetOpen(true);
        }}
      >
        {title}
      </Button>

      <SimpleSheet title={title} open={sheetOpen} onOpenChange={setSheetOpen}>
        <Flex
          direction={"column"}
          gap={3}
          width={"100%"}
          itemAlign={"start"}
          className="oui-text-sm"
        >
          {header}
          <Divider className="oui-w-full" />
          {lastPrice}
          {inputForm}
          {slider}
          {footer}
        </Flex>
      </SimpleSheet>

      <SimpleDialog open={dialogOpen} onOpenChange={setDialogOpen} size="xs">
        {isMarketClose ? (
          <MarketCloseConfirm
            base={base}
            quantity={quantity}
            submitting={submitting}
            onClose={props.onCloseDialog}
            onConfirm={props.onConfirm}
            hideCloseIcon
          />
        ) : (
          <LimitConfirmDialog
            base={base}
            quoteDp={quote_dp}
            quantity={quantity}
            price={price}
            submitting={submitting}
            order={props.closeOrderData}
            onClose={props.onCloseDialog}
            onConfirm={props.onConfirm}
            hideCloseIcon
          />
        )}
      </SimpleDialog>
    </>
  );
};
