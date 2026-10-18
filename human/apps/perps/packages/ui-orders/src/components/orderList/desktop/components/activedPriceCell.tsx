import { useCallback, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { API, OrderType } from "@paxeer-network/types";
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

export const ActivedPriceCell = (props: {
  order: API.AlgoOrderExt;
  disabled?: boolean;
}) => {
  const { order } = props;
  const { t } = useTranslation();
  // the value before editing, initial value
  const originValue = order.activated_price?.toString() ?? "";

  const [previousOriginValue, setPreviousOriginValue] =
    useState<string>(originValue);
  const [value, setValue] = useState<string>(originValue);

  if (previousOriginValue !== originValue) {
    setPreviousOriginValue(originValue);
    setValue(originValue);
  }

  const [submitting, setSubmitting] = useState(false);
  const disabled = props.disabled || order.is_activated || order.is_triggered;

  const { editAlgoOrder } = useOrderListContext();
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

  const { error } = useValidateField({
    order,
    field: "activated_price",
    originValue,
    value,
  });

  const onConfirm = useCallback(() => {
    setSubmitting(true);

    const data: any = {
      algo_order_id: order.algo_order_id,
      activated_price: value,
    };
    // include original margin_mode so backend receives it when editing
    if (order.margin_mode !== undefined) {
      data.margin_mode = order.margin_mode;
    }

    editAlgoOrder(order.algo_order_id.toString(), data)
      .then((result: any) => {
        closePopover();
        setValue(value);
      })
      .catch((err: any) => {
        toast.error(err.message);
        cancelPopover();
      })
      .finally(() => setSubmitting(false));
  }, [order.algo_order_id, value, cancelPopover]);

  if (!order.activated_price && order.type === OrderType.MARKET) {
    return <span>{t("common.marketPrice")}</span>;
  }

  const renderContent = () => {
    if (!editing || disabled) {
      return (
        <PreviewCell
          value={value}
          status={getOrderStatus(order)}
          setEditing={setEditing}
          disabled={disabled}
        />
      );
    }

    return (
      <EditableCellInput
        value={value}
        onChange={setValue}
        onClick={onClick}
        // when editing, value should not be empty
        error={value ? error : t("orderEntry.triggerPrice.error.required")}
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
          type={EditType.triggerPrice}
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
