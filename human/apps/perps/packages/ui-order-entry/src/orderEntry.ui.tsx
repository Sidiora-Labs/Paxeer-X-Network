import React, { useCallback, useEffect, useId, useMemo, useState } from "react";
import {
  ERROR_MSG_CODES,
  OrderValidationResult,
  USDCBorrowLimitExceededError,
  useLocalStorage,
  useMemoizedFn,
  usePaxeerNetworkContext,
} from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { useOrderEntryFormErrorMsg } from "@paxeer-network/react-app";
import {
  PaxeerNetworkOrder,
  OrderSide,
  OrderType,
  PositionType,
} from "@paxeer-network/types";
import {
  Box,
  cn,
  Divider,
  Flex,
  modal,
  SimpleSheet,
  Switch,
  toast,
  useScreen,
} from "@paxeer-network/ui";
import { TPSLAdvancedWidget } from "@paxeer-network/ui-tpsl";
import { Decimal } from "@paxeer-network/utils";
import { AdditionalConfigButton } from "./components/additional/additionalConfigButton";
import {
  AdditionalInfo,
  AdditionalInfoProps,
} from "./components/additional/additionalInfo";
import { PinButton } from "./components/additional/pinButton";
import { AdvancedTPSLResult } from "./components/advancedTPSLResult";
import { orderConfirmDialogId } from "./components/dialog/confirm.ui";
import { MaxQtyConfirm } from "./components/dialog/maxQtyConfirm";
import {
  permissionlessMarketNoticeDesktopDialogId,
  permissionlessMarketNoticeDialogId,
} from "./components/dialog/permissionlessMarketNotice.ui";
import { scaledOrderConfirmDialogId } from "./components/dialog/scaledOrderConfirm";
import { OrderEntryHeader } from "./components/header";
import {
  OrderEntryAvailableInjectabled,
  OrderEntryBodyInjectabled,
  OrderEntryQuantitySliderInjectabled,
  OrderEntrySubmitSectionInjectabled,
} from "./components/orderEntry.injectabled";
import { OrderEntryProvider } from "./components/orderEntryProvider";
import { OrderInput } from "./components/orderInput";
import { ReduceOnlySwitch } from "./components/reduceOnlySwitch";
import { OrderTPSL } from "./components/tpsl";
import { type OrderEntryScriptReturn } from "./orderEntry.script";
import { getScaledPlaceOrderMessage } from "./utils";

export type OrderEntryProps = OrderEntryScriptReturn & {
  containerRef?: React.RefObject<HTMLDivElement>;
  disableFeatures?: ("slippageSetting" | "feesInfo")[];
};

export const OrderEntry: React.FC<OrderEntryProps> = (props) => {
  const {
    side,
    formattedOrder,
    setOrderValue,
    manualSetOrderValue,
    setOrderValues,
    setOrderValuesRaw,
    symbolInfo,
    maxQty,
    freeCollateral,
    helper,
    submit,
    metaState,
    bboStatus,
    bboType,
    onBBOChange,
    toggleBBO,
    disableFeatures,
    currentLtv,
    fillMiddleValue,
    soundAlert,
    setSoundAlert,
    currentFocusInput,
    walletAddress,
    isPermissionlessListing,
    symbol,
    usdcBorrowLimit,
  } = props;
  const [maxQtyConfirmOpen, setMaxQtyConfirmOpen] = useState(false);
  const [selectedExtraId, setSelectedExtraId] = useState<string | null>(null);

  const [permissionlessAcknowledgedKeys, setPermissionlessAcknowledgedKeys] =
    useLocalStorage<string[]>("paxeer-network-permissionless-market-notice", []);

  const { t } = useTranslation();

  const { isMobile } = useScreen();
  const [hasAdvancedTPSLResult, setHasAdvancedTPSLResult] =
    useState<boolean>(false);

  const { errors, validated } = metaState;

  const [errorMsgVisible, setErrorMsgVisible] = useState(false);

  const [needConfirm, setNeedConfirm] = useLocalStorage(
    "paxeerNetwork_order_confirm",
    true,
  );
  const [pinned, setPinned] = useLocalStorage(
    "paxeer-network-order-additional-pinned",
    true,
  );
  const [showTPSLAdvanced, setShowTPSLAdvanced] = useState(false);
  const [hidden, setHidden] = useLocalStorage("paxeer-network-order-hidden", false);

  const [slippage, setSlippage] = useLocalStorage("paxeer-network-slippage", "1", {
    parseJSON: ((value: string | null) => {
      return !value || value === '""' ? "1" : JSON.parse(value);
    }) as any,
  });

  const { notification } = usePaxeerNetworkContext();

  const soundAlertId = useId();

  const { getErrorMsg } = useOrderEntryFormErrorMsg(validated ? errors : null);

  const buttonLabel = useMemo(() => {
    // TODO: remove this for bug handling
    // if (isMobile && freeCollateral <= 0) {
    //   return t("common.deposit");
    // }
    return side === OrderSide.BUY
      ? t("orderEntry.buyLong")
      : t("orderEntry.sellShort");
  }, [side, t, isMobile]);

  useEffect(() => {
    if (validated) {
      setErrorMsgVisible(true);
    }
  }, [validated]);

  // set slippage
  useEffect(() => {
    if (disableFeatures?.includes("slippageSetting")) {
      return;
    }
    if (slippage) {
      manualSetOrderValue("slippage", Number(slippage));
    } else {
      manualSetOrderValue("slippage", undefined);
    }
  }, [slippage, disableFeatures, manualSetOrderValue]);

  useEffect(() => {
    const clickHandler = (event: MouseEvent) => {
      const target = event.target as HTMLElement;
      if (
        target.closest("#order-entry-submit-button")
        // || target.closest(".paxeer-network-order-entry")
      ) {
        return;
      }
      setErrorMsgVisible((visible) => {
        if (visible) {
          return false;
        }
        return visible;
      });
    };

    if (errorMsgVisible) {
      document.addEventListener("click", clickHandler);
    } else {
      document.removeEventListener("click", clickHandler);
    }

    return () => {
      document.removeEventListener("click", clickHandler);
    };
  }, [errorMsgVisible]);

  const onSubmit = useMemoizedFn(async () => {
    const isScaledOrder = formattedOrder.order_type === OrderType.SCALED;

    // TODO: in the future, we will be add `level` to `OrderValidationItem`, use `level` manager ui state
    const isSlPriceError =
      props.slPriceError?.sl_trigger_price?.type ===
      ERROR_MSG_CODES.SL_PRICE_ERROR;
    helper
      .validate(isSlPriceError ? props.slPriceError : undefined)
      .then(
        // validate success, it return the order
        // TODO: get order from other function
        (order: any) => order,
        // should catch validate error first, then submit
        (errors: OrderValidationResult) => {
          // slippage error message is not show input tooltip, so we need to manually show it by toast
          if (errors.slippage) {
            toast.error(t("orderEntry.slippage.error.max"));
          }

          // when switch order type, validated not changed, so we need to set it to true
          setErrorMsgVisible(true);

          return Promise.reject();
        },
      )
      .then((order) => {
        const projectedBorrow = helper.getProjectedUSDCBorrow(order);
        if (projectedBorrow !== null && projectedBorrow > usdcBorrowLimit) {
          throw new USDCBorrowLimitExceededError(
            projectedBorrow,
            usdcBorrowLimit,
          );
        }
        return order;
      })
      .then((order: any) => {
        const shouldShowPermissionlessNotice =
          isPermissionlessListing &&
          walletAddress &&
          !(permissionlessAcknowledgedKeys ?? []).includes(
            `${walletAddress}_${symbol}`,
          );

        if (shouldShowPermissionlessNotice) {
          return modal
            .show(
              isMobile
                ? permissionlessMarketNoticeDialogId
                : permissionlessMarketNoticeDesktopDialogId,
            )
            .then(() => {
              setPermissionlessAcknowledgedKeys([
                ...(Array.isArray(permissionlessAcknowledgedKeys)
                  ? permissionlessAcknowledgedKeys
                  : []),
                `${walletAddress}_${symbol}`,
              ]);
              return order;
            });
        }
        return Promise.resolve(order);
      })
      .then((order: any) => {
        if (isScaledOrder) {
          return modal.show(scaledOrderConfirmDialogId, {
            order,
            symbolInfo,
            size: isMobile ? "sm" : "md",
          });
        }
        if (needConfirm) {
          return modal.show(orderConfirmDialogId, {
            order: formattedOrder,
            symbolInfo,
          });
        }
        return true;
      })
      .then(() => {
        // validate success, submit order
        return submit({
          resetOnSuccess: false,
          usdcBorrowLimit,
        }).then((result: any) => {
          if (!result.success && result.message) {
            toast.error(result.message);
          } else if (result.success && isScaledOrder) {
            const message = getScaledPlaceOrderMessage(result);
            if (message) {
              toast.success(message);
            }
          }
        });
      })
      .catch((error) => {
        if (error instanceof USDCBorrowLimitExceededError) {
          modal.alert({
            title: t("orderEntry.usdcBorrowLimit.title"),
            message: t("orderEntry.usdcBorrowLimit.content", {
              limit: new Intl.NumberFormat("en-US", {
                maximumFractionDigits: 8,
              }).format(error.borrowLimit),
            }),
          });
          return;
        }
        // submit order error
        if (error?.message) {
          toast.error(error.message);
          // toast.error(`Error:${error.message}`);

          // if (error instanceof ApiError) {
          // toast.error(error.message);
        }
      });
  });

  const formattedMaxQty = useMemo(() => {
    return new Decimal(maxQty)
      .todp(symbolInfo.base_dp, Decimal.ROUND_DOWN)
      .toString();
  }, [maxQty, symbolInfo.base_dp]);

  const onMaxQtyConfirm = useCallback(() => {
    manualSetOrderValue("order_quantity", formattedMaxQty);
    // submit order when order_quantity updated
    requestAnimationFrame(() => {
      onSubmit();
    });
    setMaxQtyConfirmOpen(false);
  }, [manualSetOrderValue, formattedMaxQty, onSubmit]);

  const validateSubmit = async () => {
    // show a prompt reminding the user. If the user confirms, automatically disable Reduce Only and proceed with the action.
    if (formattedOrder.reduce_only && maxQty === 0) {
      return modal.confirm({
        title: t("orderEntry.reduceOnly.reminder"),
        content: t("orderEntry.reduceOnly.reminder.content"),
        okLabel: t("orderEntry.placeOrderNow"),
        onOk: async () => {
          manualSetOrderValue("reduce_only", false);
          // submit order when reduce only updated
          requestAnimationFrame(() => {
            props.resetMetaState();
            onSubmit();
          });
          return Promise.resolve(true);
        },
        onCancel: async () => {
          return Promise.resolve(false);
        },
      });
    } else if (maxQty > 0 && Number(formattedOrder.order_quantity) > maxQty) {
      setMaxQtyConfirmOpen(true);
    } else {
      onSubmit();
    }
  };

  const onShowTPSLAdvanced = () => {
    helper.validate().then(
      () => {
        setShowTPSLAdvanced(true);
      },
      (errors) => {
        const tpslKey = new Set(["tp_trigger_price", "sl_trigger_price"]);
        if (Object.keys(errors).every((key: string) => tpslKey.has(key))) {
          setShowTPSLAdvanced(true);
        }
      },
    );
    // modal.show(TPSLAdvancedDialogId, {
    //   order: formattedOrder,
    //   setOrderValue: setOrderValue,
    // });
    // setShowTPSLAdvanced(true);
  };

  const onSubmitAdvancedTPSL = (order: PaxeerNetworkOrder) => {
    if (order.side !== formattedOrder.side) {
      manualSetOrderValue("side", order.side);
    }
    setOrderValuesRaw({
      position_type: order.position_type,
      tp_order_type: order.tp_order_type,
      tp_pnl: order.tp_pnl,
      tp_offset: order.tp_offset,
      tp_offset_percentage: order.tp_offset_percentage,
      tp_offset_from_mark: order.tp_offset_from_mark,
      tp_offset_percentage_from_mark: order.tp_offset_percentage_from_mark,
      tp_ROI: order.tp_ROI,
      tp_trigger_price: order.tp_trigger_price,
      tp_order_price: order.tp_order_price,
      sl_order_type: order.sl_order_type,
      sl_trigger_price: order.sl_trigger_price,
      sl_order_price: order.sl_order_price,
      sl_pnl: order.sl_pnl,
      sl_offset: order.sl_offset,
      sl_offset_percentage: order.sl_offset_percentage,
      sl_offset_from_mark: order.sl_offset_from_mark,
      sl_offset_percentage_from_mark: order.sl_offset_percentage_from_mark,
      sl_ROI: order.sl_ROI,
    });
    setShowTPSLAdvanced(false);
    setHasAdvancedTPSLResult(true);
  };

  const onDeleteAdvancedTPSL = () => {
    setHasAdvancedTPSLResult(false);
    setOrderValues({
      tp_trigger_price: undefined,
      tp_order_price: undefined,
      tp_order_type: OrderType.MARKET,
      sl_trigger_price: undefined,
      sl_order_price: undefined,
      sl_order_type: OrderType.MARKET,
      tp_pnl: undefined,
      sl_pnl: undefined,
      tp_offset: undefined,
      tp_offset_percentage: undefined,
      tp_offset_from_mark: undefined,
      tp_offset_percentage_from_mark: undefined,
      sl_offset: undefined,
      sl_offset_percentage: undefined,
      sl_offset_from_mark: undefined,
      sl_offset_percentage_from_mark: undefined,
      position_type: PositionType.FULL,
    });
  };

  useEffect(() => {
    setHasAdvancedTPSLResult(false);
    // Clear plugin custom order type when switching symbols.
    setSelectedExtraId(null);
  }, [props.symbol]);

  const showReduceOnlySection =
    (isMobile &&
      formattedOrder.order_type !== OrderType.LIMIT &&
      formattedOrder.order_type !== OrderType.MARKET) ||
    !isMobile;

  const showSoundSection =
    (Boolean(notification?.orderFilled?.media) ||
      Boolean(notification?.orderFilled?.soundOptions?.length)) &&
    (notification?.orderFilled?.displayInOrderEntry ?? true);

  const additionalInfoProps: AdditionalInfoProps = {
    pinned,
    setPinned,
    needConfirm,
    setNeedConfirm,
    hidden,
    setHidden,
    onValueChange: manualSetOrderValue,
    orderTypeExtra: formattedOrder["order_type_ext"],
    showExtra:
      formattedOrder["order_type"] === OrderType.LIMIT && !props.tpslSwitch,
  };
  // Additional info （fok，ioc、post only， order confirm hidden）
  const extraButton = !pinned && (
    <AdditionalConfigButton {...additionalInfoProps} />
  );

  return (
    <OrderEntryProvider
      errors={errors}
      errorMsgVisible={errorMsgVisible}
      symbolInfo={symbolInfo}
      onFocus={props.onFocus}
      onBlur={props.onBlur}
      getErrorMsg={getErrorMsg}
      setOrderValue={setOrderValue}
      manualSetOrderValue={manualSetOrderValue}
      setOrderValues={setOrderValues}
      currentFocusInput={currentFocusInput.current}
      priceInputRef={props.priceInputRef}
      priceInputContainerRef={props.priceInputContainerRef}
      triggerPriceInputRef={props.triggerPriceInputRef}
      activatedPriceInputRef={props.activatedPriceInputRef}
      lastQuantityInputType={props.lastQuantityInputType}
      leverage={props.symbolLeverage}
    >
      <MaxQtyConfirm
        open={maxQtyConfirmOpen}
        onOpenChange={setMaxQtyConfirmOpen}
        maxQty={formattedMaxQty}
        onConfirm={onMaxQtyConfirm}
        base={symbolInfo.base}
      />
      <div
        className={cn(
          "oui-orderEntry",
          "oui-space-y-2 oui-text-base-contrast-54 xl:oui-space-y-3",
        )}
        ref={props.containerRef}
      >
        <OrderEntryHeader
          symbol={props.symbol}
          canTrade={props.canTrade}
          side={side}
          order_type={formattedOrder.order_type!}
          setOrderValue={manualSetOrderValue}
          symbolLeverage={props.symbolLeverage}
          marginMode={props.marginMode}
          marketOrderDisabled={props.isSymbolPostOnly}
          marketOrderDisabledTooltip={t(
            "orderEntry.orderType.symbolPostOnly.tooltip",
          )}
          selectedExtraId={selectedExtraId}
          onExtraSelect={setSelectedExtraId}
        />

        <OrderEntryBodyInjectabled
          symbol={props.symbol}
          side={side}
          selectedCustomTypeId={selectedExtraId}
        >
          <OrderEntryAvailableInjectabled
            currentLtv={currentLtv}
            canTrade={props.canTrade}
            quote={symbolInfo?.quote}
            freeCollateral={freeCollateral}
            marginMode={props.marginMode}
          />

          <OrderInput
            values={formattedOrder}
            priceInputContainerWidth={props.priceInputContainerWidth}
            fillMiddleValue={fillMiddleValue}
            bbo={{
              bboStatus,
              bboType,
              onBBOChange,
              toggleBBO,
            }}
          />

          <OrderEntryQuantitySliderInjectabled
            canTrade={props.canTrade}
            side={props.side}
            order_quantity={formattedOrder.order_quantity}
            maxQty={maxQty}
          />

          <OrderEntrySubmitSectionInjectabled
            buttonLabel={buttonLabel}
            side={side}
            canTrade={props.canTrade}
            isMutating={props.isMutating}
            onSubmit={validateSubmit}
            assetInfo={{
              canTrade: props.canTrade,
              quote: symbolInfo.quote,
              estLiqPrice: props.estLiqPrice,
              estLiqPriceDistance: props.estLiqPriceDistance,
              estLeverage: props.estLeverage,
              currentLeverage: props.currentLeverage,
              slippage,
              dp: symbolInfo.quote_dp,
              setSlippage,
              estSlippage: props.estSlippage,
              orderType: formattedOrder.order_type!,
              disableFeatures,
              symbol: props.symbol,
              side,
            }}
          />

          <Divider className="oui-w-full" />

          {/* TP SL switch and content */}
          {hasAdvancedTPSLResult ? (
            <AdvancedTPSLResult
              order={formattedOrder}
              symbolInfo={props.symbolInfo}
              errors={validated ? errors : null}
              onEdit={() => {
                setShowTPSLAdvanced(true);
              }}
              onDelete={() => {
                onDeleteAdvancedTPSL();
              }}
            />
          ) : (
            <OrderTPSL
              // onCancelTPSL={props.cancelTP_SL}
              // onEnableTP_SL={props.enableTP_SL}
              quote_dp={props.symbolInfo.quote_dp}
              switchState={props.tpslSwitch}
              onSwitchChanged={props.setTpslSwitch}
              orderType={formattedOrder.order_type!}
              errors={
                validated || props.slPriceError !== undefined
                  ? { ...errors, ...props.slPriceError }
                  : null
              }
              setOrderValue={manualSetOrderValue}
              reduceOnlyChecked={formattedOrder.reduce_only ?? false}
              onReduceOnlyChange={(checked) => {
                manualSetOrderValue("reduce_only", checked);
              }}
              values={{
                position_type:
                  formattedOrder.position_type ?? PositionType.PARTIAL,
                tp: {
                  trigger_price: formattedOrder.tp_trigger_price ?? "",
                  PnL: formattedOrder.tp_pnl ?? "",
                  Offset: formattedOrder.tp_offset ?? "",
                  "Offset%": formattedOrder.tp_offset_percentage ?? "",
                  OffsetFromMark: formattedOrder.tp_offset_from_mark ?? "",
                  PercentageFromMark:
                    formattedOrder.tp_offset_percentage_from_mark ?? "",
                  ROI: formattedOrder.tp_ROI ?? "",
                },
                sl: {
                  trigger_price: formattedOrder.sl_trigger_price ?? "",
                  PnL: formattedOrder.sl_pnl ?? "",
                  Offset: formattedOrder.sl_offset ?? "",
                  "Offset%": formattedOrder.sl_offset_percentage ?? "",
                  OffsetFromMark: formattedOrder.sl_offset_from_mark ?? "",
                  PercentageFromMark:
                    formattedOrder.sl_offset_percentage_from_mark ?? "",
                  ROI: formattedOrder.sl_ROI ?? "",
                },
              }}
              showTPSLAdvanced={onShowTPSLAdvanced}
              onChange={(key, value) => {
                setOrderValue(key, value);
              }}
            />
          )}

          {showReduceOnlySection && (
            <Flex
              justify={"between"}
              itemAlign={"center"}
              className={cn("oui-reduceOnly-container", "oui-mt-2")}
            >
              <ReduceOnlySwitch
                checked={formattedOrder.reduce_only ?? false}
                onCheckedChange={(checked) => {
                  manualSetOrderValue("reduce_only", checked);
                }}
              />
              {!showSoundSection && extraButton}
            </Flex>
          )}
          {showSoundSection && (
            <Flex
              justify={"between"}
              itemAlign={"center"}
              className="oui-soundAlert-container"
            >
              <Flex itemAlign={"center"} gapX={1}>
                <Switch
                  className={cn("oui-soundAlert-switch", "oui-h-[14px]")}
                  id={soundAlertId}
                  checked={soundAlert}
                  onCheckedChange={(checked) => setSoundAlert(checked)}
                />
                <label
                  htmlFor={soundAlertId}
                  className={cn("oui-soundAlert-label", "oui-text-xs")}
                >
                  {t("portfolio.setting.soundAlerts")}
                </label>
              </Flex>
              {extraButton}
            </Flex>
          )}
          {/* Additional info （fok，ioc、post only， order confirm hidden） */}
          {pinned && (
            <Box
              p={2}
              r={"md"}
              intensity={700}
              position={"relative"}
              className="oui-additional-container"
            >
              <AdditionalInfo {...additionalInfoProps} />
              <PinButton
                onClick={() => {
                  setPinned(false);
                }}
                className={cn(
                  "oui-additional-pin-btn",
                  "oui-group oui-absolute oui-end-2 oui-top-2",
                )}
                data-testid="oui-testid-orderEntry-pinned-button"
              />
            </Box>
          )}
        </OrderEntryBodyInjectabled>
      </div>

      <SimpleSheet
        open={showTPSLAdvanced}
        onOpenChange={setShowTPSLAdvanced}
        classNames={{
          body: cn(
            "oui-tpslAdvanced-body",
            "oui-h-full oui-pb-0 oui-border-none",
          ),
          overlay: cn("oui-tpslAdvanced-overlay", "!oui-bg-base-10/60"),
          content: cn(
            "oui-tpslAdvanced-sheet",
            "oui-rounded-[16px] oui-border-none !oui-p-0",
            isMobile
              ? "oui-inset-y-0 oui-end-0 oui-w-[280px]"
              : "!oui-bottom-[40px] oui-end-3 oui-top-[44px] !oui-h-auto oui-w-[360px]",
          ),
        }}
        contentProps={{ side: "right", closeable: false }}
      >
        {showTPSLAdvanced && (
          <TPSLAdvancedWidget
            setOrderValue={manualSetOrderValue}
            order={formattedOrder as PaxeerNetworkOrder}
            onSubmit={onSubmitAdvancedTPSL}
            onClose={() => {
              setShowTPSLAdvanced(false);
            }}
            symbolLeverage={props.symbolLeverage}
          />
        )}
      </SimpleSheet>
    </OrderEntryProvider>
  );
};
