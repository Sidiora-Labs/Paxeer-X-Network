import { SVGProps } from "react";
import { OrderValidationResult } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { useOrderEntryFormErrorMsg } from "@paxeer-network/react-app";
import {
  API,
  PaxeerNetworkOrder,
  OrderType,
  PositionType,
} from "@paxeer-network/types";
import { Divider, Flex, Text } from "@paxeer-network/ui";

export function AdvancedTPSLResult(props: {
  order: Partial<PaxeerNetworkOrder>;
  symbolInfo: API.SymbolExt;
  errors: OrderValidationResult | null;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const { order: formattedOrder, symbolInfo, onEdit, onDelete, errors } = props;

  const { getErrorMsg } = useOrderEntryFormErrorMsg(errors);
  const { t } = useTranslation();

  const renderTp = () => {
    const error = getErrorMsg("tp_trigger_price");
    if (formattedOrder.tp_trigger_price || formattedOrder.tp_order_price) {
      return (
        <Flex
          direction={"column"}
          itemAlign={"start"}
          className="oui-advancedTPSLResult-tp oui-w-full"
          gap={4}
        >
          <Flex
            direction={"column"}
            itemAlign={"start"}
            justify={"between"}
            gapY={1}
            className="oui-w-full"
          >
            <Flex justify={"between"} className="oui-w-full">
              <Text>{t("tpsl.tpTriggerPrice")}</Text>
              <Text.numeral
                suffix={
                  <Text className="oui-ms-1 oui-text-base-contrast-36">
                    {symbolInfo.quote}
                  </Text>
                }
                className="oui-text-base-contrast"
                dp={symbolInfo.quote_dp}
              >
                {formattedOrder.tp_trigger_price ?? ""}
              </Text.numeral>
            </Flex>
            <Flex justify={"between"} className="oui-w-full">
              <Text>{t("tpsl.tpOrderPrice")}</Text>
              {formattedOrder.tp_order_type === OrderType.LIMIT ? (
                <Text.numeral
                  suffix={
                    <Text className="oui-ms-1 oui-text-base-contrast-36">
                      {symbolInfo.quote}
                    </Text>
                  }
                  className="oui-text-base-contrast"
                  dp={symbolInfo.quote_dp}
                >
                  {formattedOrder.tp_order_price ?? ""}
                </Text.numeral>
              ) : (
                <Text className="oui-text-base-contrast">Market</Text>
              )}
            </Flex>
            <Flex justify={"between"} className="oui-w-full">
              <Text>{t("tpsl.totalEstTpPnl")}</Text>
              <Text.numeral
                suffix={
                  <Text className="oui-ms-1 oui-text-base-contrast-36">
                    {symbolInfo.quote}
                  </Text>
                }
                coloring
                dp={2}
              >
                {Number(formattedOrder.tp_pnl)}
              </Text.numeral>
            </Flex>
          </Flex>
          {error && (
            <Flex
              justify={"start"}
              itemAlign={"start"}
              gap={2}
              className="oui-w-full"
            >
              <div className="oui-relative oui-top-[7px] oui-size-1 oui-rounded-full oui-bg-danger" />
              <Text className="oui-text-danger">{error}</Text>
            </Flex>
          )}
        </Flex>
      );
    }
    return null;
  };

  const renderSl = () => {
    if (formattedOrder.sl_trigger_price || formattedOrder.sl_order_price) {
      const error = getErrorMsg("sl_trigger_price");
      return (
        <Flex
          direction={"column"}
          itemAlign={"start"}
          className="oui-advancedTPSLResult-sl oui-w-full"
          gap={4}
        >
          <Flex
            direction={"column"}
            itemAlign={"start"}
            justify={"between"}
            gapY={1}
            className="oui-w-full"
          >
            <Flex justify={"between"} className="oui-w-full">
              <Text>{t("tpsl.slTriggerPrice")}</Text>
              <Text.numeral
                suffix={
                  <Text className="oui-ms-1 oui-text-base-contrast-36">
                    {symbolInfo.quote}
                  </Text>
                }
                className="oui-text-base-contrast"
                dp={symbolInfo.quote_dp}
              >
                {formattedOrder.sl_trigger_price ?? ""}
              </Text.numeral>
            </Flex>
            <Flex justify={"between"} className="oui-w-full">
              <Text>{t("tpsl.slOrderPrice")}</Text>
              {formattedOrder.sl_order_type === OrderType.LIMIT ? (
                <Text.numeral
                  suffix={
                    <Text className="oui-ms-1 oui-text-base-contrast-36">
                      {symbolInfo.quote}
                    </Text>
                  }
                  className="oui-text-base-contrast"
                  dp={symbolInfo.quote_dp}
                >
                  {formattedOrder.sl_order_price ?? ""}
                </Text.numeral>
              ) : (
                <Text className="oui-text-base-contrast">Market</Text>
              )}
            </Flex>

            <Flex justify={"between"} className="oui-w-full">
              <Text>{t("tpsl.totalEstSlPnl")}</Text>
              <Text.numeral
                coloring
                suffix={
                  <Text className="oui-ms-1 oui-text-base-contrast-36">
                    {symbolInfo.quote}
                  </Text>
                }
                dp={2}
              >
                {Number(formattedOrder.sl_pnl)}
              </Text.numeral>
            </Flex>
            {error && (
              <Flex
                justify={"start"}
                itemAlign={"start"}
                gap={2}
                className="oui-w-full"
              >
                <div className="oui-relative oui-top-[7px] oui-size-1 oui-rounded-full oui-bg-danger" />
                <Text className="oui-text-danger">{error}</Text>
              </Flex>
            )}
          </Flex>
        </Flex>
      );
    }
    return null;
  };

  return (
    <Flex
      direction={"column"}
      itemAlign={"start"}
      className="oui-orderEntry-advancedTpslResult oui-w-full oui-text-2xs"
      gap={4}
    >
      <Flex
        justify={"between"}
        itemAlign={"start"}
        className="oui-advancedTpslResult-header oui-w-full"
      >
        <Text>{t("common.tpsl")}</Text>
        <Flex gap={2} className="oui-advancedTpslResult-actions">
          <DeleteIcon
            size={12}
            className="oui-delete-btn oui-cursor-pointer oui-text-base-contrast-54 hover:oui-text-base-contrast"
            opacity={1}
            onClick={onDelete}
          />
          <EditIcon
            size={12}
            className="oui-edit-btn oui-cursor-pointer oui-text-base-contrast-54 hover:oui-text-base-contrast"
            onClick={onEdit}
          />
        </Flex>
      </Flex>
      <Flex justify={"between"} itemAlign={"start"} className="oui-w-full">
        <Text>{t("tpsl.mode")}</Text>
        <Text className="oui-text-base-contrast">
          {formattedOrder.position_type === PositionType.FULL
            ? t("tpsl.fullPosition")
            : t("tpsl.partialPosition")}
        </Text>
      </Flex>
      {renderTp()}
      {renderSl()}

      <Divider className="oui-mb-2 oui-w-full" />
    </Flex>
  );
}

interface IconProps extends SVGProps<SVGSVGElement> {
  size: number;
}
const DeleteIcon: React.FC<IconProps> = (props) => {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width={props.size}
      height={props.size}
      viewBox="0 0 12 12"
      fill="currentColor"
      {...props}
    >
      <path d="M5.99903 0.976562C5.44653 0.976562 4.99903 1.42406 4.99903 1.97656H2.49902C2.22302 1.97656 1.99902 2.20056 1.99902 2.47656C1.99902 2.75256 2.22302 2.97656 2.49902 2.97656H9.49903C9.77503 2.97656 9.99903 2.75256 9.99903 2.47656C9.99903 2.20056 9.77503 1.97656 9.49903 1.97656H6.99903C6.99903 1.42406 6.55153 0.976562 5.99903 0.976562ZM2.49902 3.97655V8.97654C2.49902 10.0715 3.40152 10.961 4.49903 10.961L7.51453 10.9765C8.61203 10.9765 9.49903 10.074 9.49903 8.97654V3.97655H2.49902ZM4.99903 5.47655C5.27503 5.47655 5.49903 5.70055 5.49903 5.97655V8.97654C5.49903 9.25254 5.27503 9.47654 4.99903 9.47654C4.72303 9.47654 4.49903 9.25254 4.49903 8.97654V5.97655C4.49903 5.70055 4.72303 5.47655 4.99903 5.47655ZM6.99903 5.47655C7.27503 5.47655 7.49903 5.70055 7.49903 5.97655V8.97654C7.49903 9.25254 7.27503 9.47654 6.99903 9.47654C6.72303 9.47654 6.49903 9.25254 6.49903 8.97654V5.97655C6.49903 5.70055 6.72303 5.47655 6.99903 5.47655Z" />
    </svg>
  );
};

const EditIcon: React.FC<IconProps> = (props) => {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      viewBox="0 0 12 12"
      width={props.size}
      height={props.size}
      fill="currentColor"
      {...props}
    >
      <path d="M8.49779 0.976562C8.36529 0.976562 8.23229 1.02357 8.13829 1.11707C7.86029 1.39507 6.85979 2.39558 6.63779 2.61808L2.13529 7.12059L1.63479 7.62059C1.56529 7.69059 1.52929 7.78958 1.50979 7.88658L1.00979 10.3881C0.939788 10.7381 1.23779 11.0361 1.58779 10.9666C1.90079 10.9036 3.77679 10.5286 4.08929 10.4661C4.18629 10.4466 4.28529 10.4106 4.35529 10.3411L4.85529 9.84059L9.35779 5.33808C9.58029 5.11608 10.5808 4.11506 10.8588 3.83756C10.9523 3.74356 10.9993 3.61056 10.9993 3.47806C10.9993 2.65956 10.7908 2.07456 10.3583 1.63306C9.92179 1.18756 9.33879 0.976562 8.49779 0.976562ZM8.69479 1.98606C9.14629 2.01256 9.43879 2.11608 9.63929 2.32108C9.84429 2.53008 9.97379 2.82008 10.0018 3.26258C9.72779 3.53608 9.32679 3.93106 8.99829 4.25956C8.60179 3.86306 8.11279 3.37407 7.71629 2.97757C8.04529 2.64907 8.42129 2.25956 8.69479 1.98606ZM6.99729 3.69657L8.27929 4.97858L4.49579 8.76207L3.21379 7.48009L6.99729 3.69657ZM2.49479 8.19908L3.77679 9.48107L3.72979 9.52809C3.39979 9.59409 2.73329 9.73359 2.11929 9.85659L2.44779 8.24608L2.49479 8.19908Z" />
    </svg>
  );
};
