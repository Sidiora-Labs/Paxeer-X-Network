import { useMemo, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { API } from "@paxeer-network/types";
import { OrderType } from "@paxeer-network/types";
import { inputFormatter, Popover, toast } from "@paxeer-network/ui";
import { EditType } from "../../../../type";
import { getOrderStatus } from "../../../../utils/util";
import { useSymbolContext } from "../../../provider/symbolContext";
import { useOrderListContext } from "../../orderListContext";
import { ConfirmContent } from "../editOrder/confirmContent";
import { usePopoverState } from "../hooks/usePopoverState";
import { useValidateField } from "../hooks/useValidateField";
import { EditableCellInput } from "./editableCellInput";
import { PreviewCell } from "./previewCell";

export const PriceCell = (props: {
  order: API.OrderExt;
  disabled?: boolean;
}) => {
  const { order } = props;
  const { t } = useTranslation();

  const originValue = useMemo(() => {
    if (order.type === OrderType.MARKET && !order.price) {
      return "Market";
    }
    return order.price?.toString() ?? "Market";
  }, [order.price, order.type]);

  const [previousOriginValue, setPreviousOriginValue] =
    useState<string>(originValue);
  const [value, setValue] = useState<string>(originValue);

  if (previousOriginValue !== originValue) {
    setPreviousOriginValue(originValue);
    setValue(originValue);
  }

  const isAlgoOrder = order?.algo_order_id !== undefined;

  const [submitting, setSubmitting] = useState(false);

  const { editOrder, editAlgoOrder } = useOrderListContext();

  const { base, quote_dp } = useSymbolContext();

  const {
    open,
    setOpen,
    editing,
    setEditing,
    containerRef,
    closePopover,
    cancelPopover,
    onClick,
  } = usePopoverState({
    originValue,
    value,
    setValue,
  });

  const { error, getErrorMsg } = useValidateField({
    order,
    field: "order_price",
    originValue,
    value,
    fieldValues: {
      order_price: value,
      // add order quantity to validate total
      order_quantity: order.quantity?.toString(),
      // need to pass trigger_price to validate order_price, because order_price is depend on trigger_price when type is stop limit
      trigger_price: order.trigger_price?.toString(),
    },
  });

  const onConfirm = () => {
    setSubmitting(true);

    let order_id = order.order_id;
    // if reduce only is true, edit order must add  symbol, order_type, side, order_price, order_quantity, reduce_only
    let data: any = {
      symbol: order.symbol,
      order_type: order.type,
      side: order.side,
      order_quantity: order.quantity,
      order_price: value,
    };

    // if reduce_only is not undefined, request body must have reduce_only value
    if (order.reduce_only !== undefined) {
      data.reduce_only = order.reduce_only;
    }

    if (order.order_tag) {
      data.order_tag = order.order_tag;
    }

    if (order.client_order_id) {
      data.client_order_id = order.client_order_id;
    }

    // include original margin_mode so backend receives it when editing
    if (order.margin_mode !== undefined) {
      data.margin_mode = order.margin_mode;
    }

    if (isAlgoOrder) {
      order_id = order.algo_order_id as number;
      data = {
        ...data,
        order_id,
        price: value,
        algo_order_id: order_id,
      };
    }

    // edit order don't need to set visible quantity again
    // if (order?.visible_quantity === 0) {
    //   data["visible_quantity"] = 0;
    // }

    // TODO: remove this, because order not have this field
    // @ts-ignore
    if (order.tag !== undefined) {
      // @ts-ignore
      data["order_tag"] = order.tag;
    }

    let future;
    if (order.algo_order_id !== undefined) {
      future = editAlgoOrder(order.algo_order_id.toString(), data);
    } else {
      future = editOrder(order.order_id.toString(), data);
    }

    future
      .then((result: any) => {
        closePopover();
        setValue(value);
      })
      .catch((err: any) => {
        toast.error(err.message);
        cancelPopover();
      })
      .finally(() => setSubmitting(false));
  };

  const isAlgoMarketOrder = order.algo_order_id && order.type == "MARKET";

  if (isAlgoMarketOrder || value === "Market") {
    return <span>{t("common.marketPrice")}</span>;
  }

  const renderContent = () => {
    if (!editing || props.disabled) {
      return (
        <PreviewCell
          value={value}
          status={getOrderStatus(order)}
          setEditing={setEditing}
          disabled={props.disabled}
        />
      );
    }

    return (
      <EditableCellInput
        value={value}
        onChange={setValue}
        onClick={onClick}
        error={error || getErrorMsg("total")}
        formatters={[inputFormatter.dpFormatter(quote_dp)]}
      />
    );
  };

  return (
    <Popover
      open={open}
      onOpenChange={setOpen}
      content={
        <ConfirmContent
          type={EditType.price}
          base={base}
          value={value}
          cancelPopover={cancelPopover}
          isSubmitting={submitting}
          onConfirm={onConfirm}
        />
      }
    >
      <div
        onClick={(e) => {
          e.stopPropagation();
          e.preventDefault();
        }}
        ref={containerRef}
      >
        {renderContent()}
      </div>
    </Popover>
  );
};
