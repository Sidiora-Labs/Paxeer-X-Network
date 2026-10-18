import { useMemo } from "react";
import { findTPSLFromOrder } from "@paxeer-network/hooks";
import { API } from "@paxeer-network/types";
import { Flex, Text } from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import { useTPSLDetailContext } from "../tpslDetailProvider";
import { FlexCell } from "./common";

export const QtyRender = ({ order }: { order: API.AlgoOrder }) => {
  const { position, base_dp } = useTPSLDetailContext();
  const { tp_trigger_price, sl_trigger_price } = findTPSLFromOrder(order);

  const quantity = useMemo(() => {
    if (order.quantity === 0) {
      return -position.position_qty;
    }

    return position.position_qty > 0 ? -order.quantity : order.quantity;
  }, [order.quantity, position.position_qty]);

  return (
    <Flex
      direction="column"
      justify="start"
      itemAlign="start"
      className="oui-h-full oui-text-2xs"
    >
      <FlexCell>
        <Text.numeral dp={base_dp} rm={Decimal.ROUND_DOWN} padding={false}>
          {quantity}
        </Text.numeral>
      </FlexCell>
      {tp_trigger_price && sl_trigger_price && (
        <FlexCell>
          <div />
        </FlexCell>
      )}
    </Flex>
  );
};
