import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { cn, EditIcon, Flex, Text, modal } from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import { MobileTooltip } from "../items";
import { BracketOrderPriceState } from "./bracketOrderPrice.script";

export const BracketOrderPrice: FC<BracketOrderPriceState> = (props) => {
  const { t } = useTranslation();

  const onEdit = () => {
    modal.show("EditBracketOrderSheetId", {
      order: props.order,
    });
  };

  if (!props.sl_trigger_price && !props.tp_trigger_price) return <></>;

  return (
    <Flex gap={1} itemAlign={"center"} justify={"start"}>
      <MobileTooltip
        classNames={{
          content: "oui-bg-base-6 oui-ms-2",
          arrow: "oui-fill-base-6",
        }}
        content={
          <Flex direction={"column"} itemAlign={"start"} gap={1}>
            {typeof props.pnl?.tpPnL !== "undefined" && (
              <Text.numeral
                // @ts-ignore
                prefix={
                  <Text intensity={80}>{`${t("tpsl.tpPnl")}:`} &nbsp;</Text>
                }
                suffix={<Text intensity={20}>{" USDC"}</Text>}
                dp={props.quote_dp}
                color="buy"
                showIdentifier
              >
                {props.pnl?.tpPnL}
              </Text.numeral>
            )}
            {typeof props.pnl?.slPnL !== "undefined" && (
              <Text.numeral
                // @ts-ignore
                prefix={
                  <Text intensity={80}>{`${t("tpsl.slPnl")}:`} &nbsp;</Text>
                }
                suffix={<Text intensity={20}>{" USDC"}</Text>}
                dp={props.quote_dp}
                color="sell"
              >
                {props.pnl?.slPnL}
              </Text.numeral>
            )}
          </Flex>
        }
      >
        <button
          onClick={() => {
            props.setOpen(!props.open);
          }}
        >
          <Flex gap={1} width={"1"}>
            {props.tp_trigger_price && (
              <Price
                type="TP"
                value={props.tp_trigger_price}
                quote_dp={props.quote_dp}
              />
            )}
            {props.sl_trigger_price && (
              <Price
                type="SL"
                value={props.sl_trigger_price}
                quote_dp={props.quote_dp}
              />
            )}
          </Flex>
        </button>
      </MobileTooltip>
      <EditIcon
        onClick={onEdit}
        opacity={1}
        className="oui-cursor-pointer oui-text-base-contrast-54"
        size={16}
      />
    </Flex>
  );
};

const Price = (props: {
  type: "TP" | "SL";
  value?: number | string;
  quote_dp: number;
}) => {
  const { type, value, quote_dp } = props;

  return value ? (
    <Text.numeral
      size="2xs"
      className={cn(
        "oui-border-b oui-border-dashed oui-border-base-contrast-12",
        type === "TP" ? "oui-text-trade-profit" : "oui-text-trade-loss",
      )}
      key={"tp"}
      rule="price"
      dp={quote_dp}
      padding={false}
      rm={Decimal.ROUND_DOWN}
      // @ts-ignore
      prefix={
        <span className={"oui-text-base-contrast-36"}>
          {`${type} `}:&nbsp;&nbsp;
        </span>
      }
    >
      {value}
    </Text.numeral>
  ) : (
    <></>
  );
};
