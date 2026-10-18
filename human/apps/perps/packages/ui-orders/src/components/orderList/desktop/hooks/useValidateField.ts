import { useEffect, useMemo } from "react";
import { OrderValidationResult, useOrderEntity } from "@paxeer-network/hooks";
import { useOrderEntryFormErrorMsg } from "@paxeer-network/react-app";
import { API, PaxeerNetworkOrder, OrderSide } from "@paxeer-network/types";
import { convertApiOrderTypeToOrderEntryType } from "../../../../utils/util";

export function useValidateField(props: {
  order: API.OrderExt | API.AlgoOrderExt;
  originValue?: string;
  value: string;
  field: keyof OrderValidationResult;
  fieldValues?: Partial<PaxeerNetworkOrder>;
}) {
  const { order, field, originValue, value, fieldValues } = props;
  const changeFields = fieldValues || { [field]: value };

  const orderType = useMemo(
    () => convertApiOrderTypeToOrderEntryType(order as API.AlgoOrderExt),
    [order],
  );

  const { errors, validate, clearErrors } = useOrderEntity({
    symbol: order.symbol,
    order_type: orderType,
    side: order.side as OrderSide,
    ...(changeFields as any),
  });
  const { getErrorMsg } = useOrderEntryFormErrorMsg(errors);

  const error = useMemo(
    () => (field ? getErrorMsg(field) : undefined),
    [getErrorMsg, field],
  );

  useEffect(() => {
    // if value is not changed, then don't validate
    if (value === originValue) {
      clearErrors();
      return;
    }
    validate()
      .then((order) => {
        console.log("order", order);
      })
      .catch((errors) => {
        console.error(`${field} validate error:`, errors);
      });
  }, [value, originValue]);

  return { error, errors, getErrorMsg };
}
