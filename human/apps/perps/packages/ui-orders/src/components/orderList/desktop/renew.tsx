import { FC } from "react";
import { useMutation } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { OrderEntity } from "@paxeer-network/types";
import { Button } from "@paxeer-network/ui";

export const Renew: FC<{ record: any }> = (props) => {
  const { record } = props;
  const [doCreateOrder, { data, error, reset, isMutating }] = useMutation<
    OrderEntity,
    any
  >("/v1/order");

  const { t } = useTranslation();

  const onSubmit = () => {
    const data: OrderEntity = {
      symbol: record.symbol,
      order_type: record.type,
      order_price: record.price,
      order_quantity: record.quantity,
      order_amount: record.amount,
      side: record.side,
    };

    if (Number(record.visible_quantity) < Number(record.quantity)) {
      data.visible_quantity = 0;
    }

    if (typeof record.reduce_only !== "undefined") {
      data.reduce_only = record.reduce_only;
    }

    if (record.order_tag) {
      data.order_tag = record.order_tag;
    }

    if (record.client_order_id) {
      data.client_order_id = record.client_order_id;
    }

    doCreateOrder(data);
  };

  return (
    <Button
      size={"sm"}
      variant={"outlined"}
      color={"secondary"}
      loading={isMutating}
      disabled={isMutating}
      onClick={(event) => {
        if (isMutating) return;
        event.preventDefault();
        event.stopPropagation();
        onSubmit();
      }}
    >
      {t("orders.history.renew")}
    </Button>
  );
};
