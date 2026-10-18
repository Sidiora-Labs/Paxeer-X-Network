import { API } from "@paxeer-network/types";
import { Flex, Text } from "@paxeer-network/ui";
import {
  Decimal,
  getTPSLLeg,
  getTPSLQuantity,
  getTPSLEstimatePrice,
  matchesTPSLPosition,
} from "@paxeer-network/utils";
import { useTPSLDetailContext } from "../tpslDetailProvider";
import { FlexCell } from "./common";

export const EstPnlRender = ({ order }: { order: API.AlgoOrder }) => {
  const { position } = useTPSLDetailContext();
  const matched = matchesTPSLPosition(order, position);
  return (
    <Flex
      gap={2}
      direction="column"
      justify="between"
      itemAlign="start"
      className="oui-text-2xs"
    >
      {(["tp", "sl"] as const).map((leg) => {
        const child = getTPSLLeg(order, leg);
        if (!child?.trigger_price) return null;
        const qty = matched
          ? getTPSLQuantity(child, position.position_qty)
          : undefined;
        const price = getTPSLEstimatePrice(child);
        const pnl =
          qty != null && price != null
            ? new Decimal(price)
                .minus(position.average_open_price)
                .mul(qty)
                .mul(child.side === "BUY" ? -1 : 1)
                .toNumber()
            : undefined;
        return (
          <FlexCell key={leg}>
            <Text.numeral
              dp={2}
              rm={Decimal.ROUND_DOWN}
              coloring
              padding={false}
            >
              {pnl ?? "--"}
            </Text.numeral>
          </FlexCell>
        );
      })}
    </Flex>
  );
};
