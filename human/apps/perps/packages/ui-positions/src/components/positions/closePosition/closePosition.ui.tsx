import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { OrderType } from "@paxeer-network/types";
import {
  Button,
  cn,
  Divider,
  Flex,
  Input,
  inputFormatter,
  PopoverContent,
  PopoverRoot,
  PopoverTrigger,
  Select,
  SimpleDialog,
  Text,
} from "@paxeer-network/ui";
import { ClosePositionScriptReturn } from "./closePosition.script";
import {
  ConfirmFooter,
  LimitConfirmDialog,
  MarketCloseConfirm,
} from "./components/confirmDialog";
import { QuantitySlider } from "./components/quantitySlider";

export const DesktopClosePosition: FC<ClosePositionScriptReturn> = (props) => {
  const {
    position,
    base,
    base_dp,
    quote_dp,
    price,
    quantity,
    submitting,
    dialogOpen,
    setDialogOpen,
    isMarketClose,
    popoverOpen,
    setPopoverOpen,
    maxQty,
    quantityErrorMsg,
    priceErrorMsg,
    isEntirePosition,
  } = props;

  const { t } = useTranslation();

  // const disabled = isMarketClose ? !quantity : !price || !quantity;

  const quantityForm = (
    <Flex width="100%" gap={2} mb={1}>
      <Input.tooltip
        ref={props.quantityInputRef}
        prefix={t("common.quantity")}
        suffix={
          isEntirePosition ? (
            <Text
              size="2xs"
              intensity={54}
              className="oui-cursor-pointer oui-px-3"
              onClick={props.onEntirePosition}
            >
              {t("tpsl.entirePosition")}
            </Text>
          ) : (
            base
          )
        }
        align="right"
        size="md"
        fullWidth
        autoComplete="off"
        formatters={[
          inputFormatter.numberFormatter,
          inputFormatter.dpFormatter(base_dp),
        ]}
        triggerClassName="oui-w-full"
        tooltip={quantityErrorMsg}
        color={quantityErrorMsg ? "danger" : undefined}
        value={isEntirePosition ? "" : quantity}
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
      <Button
        onClick={props.onMax}
        size="md"
        variant="outlined"
        className={cn(
          "oui-text-2xs",
          isEntirePosition
            ? "oui-border-primary-light oui-text-primary-light hover:oui-bg-primary-light/20"
            : "oui-border-line-12 oui-bg-base-6 oui-text-base-contrast-54 hover:oui-bg-base-5",
        )}
      >
        {t("common.position")}
      </Button>
    </Flex>
  );

  const suffix = (
    <Select.options
      variant="text"
      size="md"
      options={[
        {
          label: t("orderEntry.orderType.limit"),
          value: OrderType.LIMIT,
        },
        {
          label: t("orderEntry.orderType.market"),
          value: OrderType.MARKET,
        },
      ]}
      classNames={{
        // set the width of the trigger to the width of the content
        trigger: "oui-w-[--radix-select-content-available-width]",
      }}
      value={props.type}
      onValueChange={(value) => {
        props.updateOrderType(value as OrderType);
      }}
      contentProps={{
        align: "end",
        className: "oui-border oui-border-line-6",
      }}
    />
  );

  const disabledInput = isMarketClose;

  const priceForm = (
    <Input.tooltip
      suffix={suffix}
      size="md"
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
      placeholder={disabledInput ? "--" : ""}
      disabled={disabledInput}
      classNames={{
        prefix: "oui-text-base-contrast-54",
        suffix: "oui-text-base-contrast-54",
        root: cn(
          "oui-w-full oui-outline-line-12",
          priceErrorMsg ? "oui-outline-danger" : undefined,
          // when disabled, override the outline color
          disabledInput ? "focus-within:oui-outline-line-12" : undefined,
        ),
      }}
    />
  );

  return (
    <>
      <PopoverRoot open={popoverOpen} onOpenChange={setPopoverOpen}>
        <PopoverTrigger
          asChild
          onClick={() => {
            setPopoverOpen(true);
          }}
        >
          <Button variant="outlined" size="sm" color="secondary">
            {t("positions.column.close")}
          </Button>
        </PopoverTrigger>
        <PopoverContent
          className={cn(
            "oui-w-[360px] oui-p-5",
            popoverOpen ? "oui-visible" : "oui-invisible",
          )}
          align="end"
          side="top"
          onOpenAutoFocus={(e) => e.preventDefault()}
        >
          <Flex gapY={2} direction="column">
            {quantityForm}
            <QuantitySlider
              value={props.sliderValue}
              onValueChange={props.onSliderValueChange}
              base_dp={base_dp}
              max={maxQty}
              onMax={props.onMax}
            />
            <Divider my={2} intensity={8} className="oui-w-full" />
            {priceForm}
            <ConfirmFooter
              onConfirm={props.onDoubleConfirm}
              onCancel={props.onClosePopover}
              submitting={submitting}
              disabled={props.disabled}
            />
          </Flex>
        </PopoverContent>
      </PopoverRoot>

      <SimpleDialog open={dialogOpen} onOpenChange={setDialogOpen} size="sm">
        {isMarketClose ? (
          <MarketCloseConfirm
            base={base}
            quantity={quantity}
            submitting={submitting}
            onClose={props.onCloseDialog}
            onConfirm={props.onConfirm}
            classNames={{
              root: "oui-items-start",
            }}
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
