import { FC, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useMaxQty, utils } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { API, MarginMode, OrderSide } from "@paxeer-network/types";
import { AlgoOrderRootType } from "@paxeer-network/types";
import {
  cn,
  Flex,
  Popover,
  toast,
  Text,
  Slider,
  Button,
  PopoverTrigger,
  PopoverRoot,
  PopoverContent,
} from "@paxeer-network/ui";
import {
  commifyOptional,
  Decimal,
  isPositionalTPSL,
  getTPSLQuantity,
} from "@paxeer-network/utils";
import { EditType } from "../../../../type";
import { grayCell } from "../../../../utils/util";
import { useSymbolContext } from "../../../provider/symbolContext";
import { useOrderListContext } from "../../orderListContext";
import { useTPSLOrderRowContext } from "../../tpslOrderRowContext";
import { ConfirmContent } from "../editOrder/confirmContent";
import { InnerInput } from "../editOrder/innerInput";

export const QuantityCell = (props: {
  order: API.OrderExt | API.AlgoOrder;
  disabled?: boolean;
}) => {
  const { order } = props;
  const { reduce_only } = order;
  const originValue = order.quantity?.toString() ?? "";
  const [value, setValue] = useState(originValue);

  const [editing, setEditing] = useState(false);

  const { t } = useTranslation();

  const [open, setOpen] = useState(false);
  const [error, setError] = useState<string>();

  const { editOrder, editAlgoOrder, checkMinNotional } = useOrderListContext();
  const { onUpdateOrder: onUpdateTPSLOrder, position } =
    useTPSLOrderRowContext();

  const { base_dp, base, base_tick } = useSymbolContext();

  const setQuantity = async (qty: string, maxQty?: number): Promise<void> => {
    setValue(qty);
    const positionQty = Math.abs(position?.position_qty || 0);

    if (position && reduce_only && Number(qty) > positionQty) {
      setError(
        t("orders.quantity.lessThanPosition", {
          quantity: positionQty,
        }),
      );
    } else {
      const quantity = Number(qty);
      if (maxQty && quantity > maxQty) {
        setError(
          t("orders.quantity.lessThan", {
            quantity: commifyOptional(maxQty, {
              fix: base_dp,
            }),
          }),
        );
      } else {
        setError(undefined);
      }
    }
    return Promise.resolve();
  };

  useEffect(() => {
    setQuantity(order.quantity?.toString() ?? "");
  }, [props.order.quantity]);

  const closePopover = () => {
    setOpen(false);
    setEditing(false);
  };
  const cancelPopover = () => {
    setOpen(false);
    setQuantity(order.quantity?.toString() ?? "");
    setEditing(false);
  };
  const [isSubmitting, setIsSubmitting] = useState(false);

  const inputRef = useRef<HTMLInputElement>(null);

  const clickHandler = () => {
    if (!!error) {
      return;
    }

    if (Number(value) === Number(order.quantity)) {
      setEditing(false);
      return;
    }

    const price =
      order.algo_order_id !== undefined ? order.trigger_price : order.price;
    if (price !== null && order.reduce_only !== true) {
      const notionalText = checkMinNotional(order.symbol, price, value);
      if (notionalText) {
        toast.error(notionalText);
        setIsSubmitting(false);
        cancelPopover();
        return;
      }
    }

    setOpen(true);
  };

  const onClick = (event: any) => {
    event?.stopPropagation();
    event?.preventDefault();

    clickHandler();
  };

  const handleKeyDown = (event: any) => {
    if (event.key === "Enter") {
      event?.stopPropagation();
      event?.preventDefault();
      clickHandler();
    }
  };

  const onConfirm = useCallback(() => {
    setIsSubmitting(true);

    let params: any = {
      symbol: order.symbol,
      order_type: order.type,
      side: order.side,
      order_price: order.price,
      order_quantity: value,
      // reduce_only: Boolean(order.reduce_only),
      algo_order_id: order.algo_order_id,
    };

    if (
      typeof params.algo_order_id !== "undefined" &&
      params.order_type === "MARKET"
    ) {
      // stop market order
      const { order_price, ...rest } = params;
      params = rest;
    }

    // if reduce_only is not undefined, request body must have reduce_only value
    if (typeof order.reduce_only !== "undefined") {
      params.reduce_only = order.reduce_only;
    }

    if (order.order_tag) {
      params.order_tag = order.order_tag;
    }

    if (order.client_order_id) {
      params.client_order_id = order.client_order_id;
    }

    if (order?.visible_quantity === 0) {
      params["visible_quantity"] = 0;
    }

    // @ts-ignore
    if (order?.tag !== undefined) {
      // @ts-ignore
      params["order_tag"] = order.tag;
    }

    // include original margin_mode so backend receives it when editing
    if (order.margin_mode !== undefined) {
      params.margin_mode = order.margin_mode;
    }

    let future;

    if ("algo_type" in order && order.algo_type === AlgoOrderRootType.TP_SL) {
      future = onUpdateTPSLOrder(order as API.AlgoOrderExt, params);
    } else {
      if (order.algo_order_id !== undefined) {
        future = editAlgoOrder(order.algo_order_id.toString(), params);
      } else {
        future = editOrder((order as API.OrderExt).order_id.toString(), params);
      }
    }

    future
      .then(
        (result: any) => {
          closePopover();
          setQuantity(value.toString());
        },
        (err: any) => {
          toast.error(err.message);
          setQuantity(order.quantity?.toString() ?? "");
          cancelPopover();
        },
      )
      .finally(() => setIsSubmitting(false));
  }, [value]);

  const componentRef = useRef<HTMLDivElement | null>(null);
  const quantitySliderRef = useRef<HTMLDivElement | null>(null);

  const handleClickOutside = (event: any) => {
    if (
      componentRef.current &&
      quantitySliderRef.current &&
      !componentRef.current.contains(event.target as Node) &&
      !quantitySliderRef.current.contains(event.target as Node) &&
      !open
    ) {
      cancelPopover();
    }
  };

  useEffect(() => {
    document.addEventListener("mousedown", handleClickOutside);

    return () => {
      document.removeEventListener("mousedown", handleClickOutside);
    };
  }, [open, order.quantity]);

  const renderContent = () => {
    if (!editing || props.disabled) {
      return (
        <PreviewCell
          order={order}
          value={value}
          setEditing={setEditing}
          disable={props.disabled}
        />
      );
    }

    return (
      <EditState
        inputRef={inputRef}
        quantitySliderRef={quantitySliderRef}
        base_dp={base_dp}
        base_tick={base_tick}
        quantity={value}
        setQuantity={setQuantity}
        editing={editing}
        setEditing={setEditing}
        handleKeyDown={handleKeyDown}
        onClick={onClick}
        onClose={cancelPopover}
        symbol={order.symbol}
        reduce_only={reduce_only}
        positionQty={position?.position_qty}
        error={error}
        confirmOpen={open}
        side={order.side}
        order={order}
        setError={setError}
      />
    );
  };

  return (
    <Popover
      open={open}
      onOpenChange={setOpen}
      content={
        <ConfirmContent
          type={EditType.quantity}
          base={base}
          value={value}
          cancelPopover={cancelPopover}
          isSubmitting={isSubmitting}
          onConfirm={onConfirm}
        />
      }
      contentProps={{
        onOpenAutoFocus: (e) => {
          // e.preventDefault();
          // e.stopPropagation();
        },
      }}
    >
      <div
        onClick={(e) => {
          e.stopPropagation();
          e.preventDefault();
        }}
        ref={componentRef}
      >
        {renderContent()}
      </div>
    </Popover>
  );
};

const PreviewCell: FC<{
  order: API.AlgoOrder | API.OrderExt;
  value: string;
  setEditing: any;
  disable?: boolean;
}> = (props) => {
  const { order, value } = props;

  const executed = (order as API.OrderExt).total_executed_quantity;

  return (
    <Flex
      direction="row"
      justify={"start"}
      gap={1}
      className={cn(
        "oui-relative oui-max-w-[110px]",
        order.side === OrderSide.BUY && "oui-text-trade-profit",
        order.side === OrderSide.SELL && "oui-text-trade-loss",
        grayCell(order) && "oui-text-base-contrast-20",
      )}
      onClick={(e) => {
        e.stopPropagation();
        e.preventDefault();
        props.setEditing(true);
      }}
    >
      {"algo_type" in order &&
      order.algo_type === AlgoOrderRootType.TP_SL ? null : (
        <>
          <span>{executed}</span>
          <span>/</span>
        </>
      )}

      <Flex
        r="base"
        className={cn(
          "oui-h-[28px] oui-min-w-[70px]",
          !props.disable &&
            "oui-border oui-border-line-12 oui-bg-base-7 oui-px-2",
        )}
      >
        <Text size="2xs">
          {isPositionalTPSL(order) ? (getTPSLQuantity(order) ?? "--") : value}
        </Text>
      </Flex>
    </Flex>
  );
};

const EditState: FC<{
  inputRef: any;
  quantitySliderRef: any;
  base_dp: number;
  base_tick: number;
  quantity: string;
  setQuantity: (quantity: string, maxQty: number) => Promise<void>;
  editing: boolean;
  confirmOpen: boolean;
  setEditing: (value: boolean) => void;
  handleKeyDown: (e: any) => void;
  onClick: (e: any) => void;
  onClose: () => void;
  error?: string;
  symbol: string;
  reduce_only: boolean;
  positionQty?: number;
  side?: string;
  order: any;
  setError: (err: string) => void;
}> = (props) => {
  const {
    inputRef,
    quantitySliderRef,
    base_dp,
    base_tick,
    quantity,
    setQuantity,
    setEditing,
    handleKeyDown,
    onClick,
    onClose,
    error,
    symbol,
    reduce_only,
    positionQty,
    confirmOpen,
    order,
  } = props;

  const maxBuyQty = useMaxQty(symbol, order.side, {
    reduceOnly: order.reduce_only,
    marginMode: order.margin_mode ?? MarginMode.CROSS,
  });

  const maxQty = useMemo(() => {
    if (reduce_only) {
      return Math.abs(positionQty ?? 0);
    }
    return order.quantity + Math.abs(maxBuyQty);
  }, [order.quantity, maxBuyQty, reduce_only, positionQty]);

  const [sliderValue, setSliderValue] = useState<number | undefined>(undefined);

  useEffect(() => {
    if (sliderValue === undefined) {
      const sliderValue = new Decimal(quantity)
        .div(maxQty)
        .abs()
        .mul(100)
        .toNumber();
      setSliderValue(sliderValue);
    }
  }, [sliderValue, maxQty, quantity]);

  const formatQuantity = async (_qty: string | number): Promise<void> => {
    if (base_tick > 0) {
      _qty = utils.formatNumber(_qty, base_tick) ?? _qty;
    }
    return setQuantity(`${_qty}`, maxQty);
  };

  return (
    <PopoverRoot open={!confirmOpen}>
      <PopoverTrigger>
        <InnerInput
          inputRef={inputRef}
          dp={base_dp}
          value={quantity}
          setValue={(e: string) => {
            const quantity = Math.abs(Math.min(Number(e), maxQty)).toString();
            setQuantity(e, maxQty);
            if (e.endsWith(".")) return;
            if (Number(quantity) === 0) {
              setSliderValue(0);
              return;
            }
            const sliderValue = new Decimal(e)
              .div(maxQty)
              .mul(100)
              .toDecimalPlaces(2, Decimal.ROUND_DOWN)
              .toNumber();
            setSliderValue(Math.min(100, sliderValue));
          }}
          setEditing={setEditing}
          handleKeyDown={handleKeyDown}
          onClick={onClick}
          onClose={onClose}
          onBlur={(e) => {
            formatQuantity(e.target.value);
          }}
          hintInfo={error}
        />
      </PopoverTrigger>
      <PopoverContent
        className="oui-w-[360px] oui-rounded-xl"
        align="start"
        side="bottom"
        onOpenAutoFocus={(event) => {
          // event.stopPropagation();
          event.preventDefault();
        }}
      >
        <Flex
          p={1}
          gap={2}
          width={"100%"}
          itemAlign={"start"}
          ref={quantitySliderRef}
        >
          <Text.numeral
            size="xs"
            intensity={98}
            className="oui-min-w-[30px] "
            dp={2}
            padding={false}
            unit="%"
            rm={Decimal.ROUND_DOWN}
          >
            {`${sliderValue}`}
          </Text.numeral>
          <Flex
            direction={"column"}
            width={"100%"}
            gap={2}
            className="oui-mt-[6px]"
          >
            <Slider
              markCount={4}
              value={[sliderValue ?? 0]}
              onValueChange={(e) => {
                const values = Array.from(e.values());
                setSliderValue(values[0]);
                const quantity = new Decimal(values[0])
                  .div(100)
                  .mul(maxQty)
                  .abs()
                  .toFixed(base_dp, Decimal.ROUND_DOWN);
                setQuantity(quantity, maxQty);
              }}
              onValueCommit={(values) => {
                const quantity = new Decimal(values[0])
                  .div(100)
                  .mul(maxQty)
                  .abs()
                  .toFixed(base_dp, Decimal.ROUND_DOWN);
                formatQuantity(quantity).finally(() => {
                  inputRef.current.focus();
                });
              }}
            />
            <Buttons
              onClick={(value) => {
                setSliderValue(value * 100);
                let quantity = new Decimal(value)
                  .mul(maxQty)
                  .abs()
                  .toFixed(base_dp, Decimal.ROUND_DOWN);
                quantity = utils.formatNumber(quantity, base_tick) ?? quantity;

                setQuantity(quantity, maxQty);
                setTimeout(() => {
                  inputRef.current.focus();
                  inputRef.current.setSelectionRange(
                    quantity.length,
                    quantity.length,
                  );
                }, 100);
              }}
            />
          </Flex>
        </Flex>
      </PopoverContent>
    </PopoverRoot>
  );
};

const Buttons = (props: { onClick: (value: number) => void }) => {
  const { t } = useTranslation();
  const list = [
    {
      label: "0%",
      value: 0,
    },
    {
      label: "25%",
      value: 0.25,
    },
    {
      label: "50%",
      value: 0.5,
    },
    {
      label: "75%",
      value: 0.75,
    },
    {
      label: t("common.max"),
      value: 1,
    },
  ];

  return (
    <Flex gap={2} width={"100%"}>
      {list.map((item, index) => {
        return (
          <Button
            key={index}
            variant="outlined"
            color="secondary"
            size="xs"
            onClick={(e) => {
              e.stopPropagation();
              e.preventDefault();
              props.onClick(item.value);
            }}
            className="oui-w-1/5"
          >
            {item.label}
          </Button>
        );
      })}
    </Flex>
  );
};
