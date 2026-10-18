import { useMemo } from "react";
import { utils } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { API } from "@paxeer-network/types";
import {
  Flex,
  Tooltip,
  Text,
  cn,
  EditIcon,
  useModal,
  modal,
} from "@paxeer-network/ui";
import { calcBracketRoiAndPnL } from "../../../utils/util";
import { useSymbolContext } from "../../provider/symbolContext";

export const BracketOrderPrice = (props: { order: API.AlgoOrderExt }) => {
  const { order } = props;
  const { quote_dp, base_dp } = useSymbolContext();
  const { t } = useTranslation();

  const { sl_trigger_price, tp_trigger_price } = useMemo(() => {
    if (!("algo_type" in order) || !Array.isArray(order.child_orders)) {
      return {};
    }
    return utils.findTPSLFromOrder(props.order.child_orders[0]);
  }, [props.order]);

  const { pnl } = calcBracketRoiAndPnL(order);

  if (!tp_trigger_price && !sl_trigger_price) {
    return "--";
  }

  return (
    <Tooltip
      // @ts-ignore
      content={
        <Flex direction={"column"} itemAlign={"start"} gap={1}>
          {typeof pnl.tpPnL !== "undefined" && (
            <Text.numeral
              // @ts-ignore
              prefix={
                <Text intensity={80}>{`${t("tpsl.tpPnl")}:`} &nbsp;</Text>
              }
              suffix={<Text intensity={20}>{" USDC"}</Text>}
              dp={quote_dp}
              color="buy"
              showIdentifier
            >
              {pnl.tpPnL}
            </Text.numeral>
          )}
          {typeof pnl.slPnL !== "undefined" && (
            <Text.numeral
              // @ts-ignore
              prefix={
                <Text intensity={80}>{`${t("tpsl.slPnl")}:`} &nbsp;</Text>
              }
              suffix={<Text intensity={20}>{" USDC"}</Text>}
              dp={quote_dp}
              color="sell"
            >
              {pnl.slPnL}
            </Text.numeral>
          )}
        </Flex>
      }
      className="oui-bg-base-6"
    >
      <Flex itemAlign={"center"} justify={"start"} gap={2}>
        <Flex direction={"column"} justify={"start"} itemAlign={"start"}>
          <Price type="TP" value={tp_trigger_price} quote_dp={quote_dp} />
          <Price type="SL" value={sl_trigger_price} quote_dp={quote_dp} />
        </Flex>
        <EditBracketOrder order={order} />
      </Flex>
    </Tooltip>
  );
};

const EditBracketOrder = (props: { order: API.AlgoOrderExt }) => {
  const { order } = props;

  const onEdit = () => {
    modal.show("EditBracketOrderDialogId", {
      order,
    });
  };

  return (
    <EditIcon
      size={16}
      className="oui-text-base-contrast oui-cursor-pointer"
      onClick={() => {
        onEdit();
      }}
    />
  );
};

const Price = (props: {
  type: "TP" | "SL";
  value?: number | string;
  quote_dp: number;
}) => {
  const { type, value, quote_dp } = props;
  const { t } = useTranslation();

  return value ? (
    <Text.numeral
      className={cn(
        "oui-gap-0 oui-decoration-white/20 oui-border-b oui-border-dashed oui-border-base-contrast-12",
        type === "TP" ? "oui-text-trade-profit" : "oui-text-trade-loss",
      )}
      key={"tp"}
      rule="price"
      dp={quote_dp}
      // @ts-ignore
      prefix={
        <span className={"oui-text-base-contrast-54"}>
          {type === "TP" ? `${t("tpsl.tp")} -` : `${t("tpsl.sl")} -`}
          &nbsp;
        </span>
      }
    >
      {value}
    </Text.numeral>
  ) : (
    <></>
  );
};
