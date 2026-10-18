import { FC, useEffect } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { PaxeerNetworkOrder, OrderType } from "@paxeer-network/types";
import {
  Checkbox,
  cn,
  Divider,
  Flex,
  Switch,
  Tooltip,
} from "@paxeer-network/ui";

export type AdditionalInfoProps = {
  pinned: boolean;
  setPinned: (value: boolean) => void;
  needConfirm: boolean;
  setNeedConfirm: (value: boolean) => void;
  hidden: boolean;
  setHidden: (value: boolean) => void;
  onValueChange?: (key: keyof PaxeerNetworkOrder, value: any) => void;
  orderTypeExtra?: OrderType;
  showExtra?: boolean;
};

export const AdditionalInfo: FC<AdditionalInfoProps> = (props) => {
  const { pinned, orderTypeExtra } = props;
  const { t } = useTranslation();

  const onTypeToggle = (type: OrderType) => (checked: boolean) => {
    if (props.onValueChange) {
      props.onValueChange(
        "order_type_ext" as keyof PaxeerNetworkOrder,
        checked ? type : "",
        // orderTypeExtra === type ? "" : type
      );
    }
  };

  useEffect(() => {
    props.onValueChange?.("visible_quantity", props.hidden ? 0 : 1);
  }, [props.hidden]);

  return (
    <div
      className={cn(
        "oui-orderEntry-additionalInfo",
        "oui-text-base-contrast-54",
      )}
    >
      <Flex
        justify={pinned ? "start" : "between"}
        mb={3}
        width={pinned ? "unset" : "100%"}
        className="oui-gap-x-2 md:oui-gap-x-3"
        wrap="wrap"
        gapY={1}
      >
        <Flex itemAlign={"center"}>
          <Checkbox
            data-testid="oui-testid-orderEntry-postOnly-checkBox"
            id={"toggle_order_post_only"}
            className={"oui-postOnly-checkbox oui-peer"}
            color={"white"}
            variant={"radio"}
            disabled={!props.showExtra}
            checked={orderTypeExtra === OrderType.POST_ONLY}
            onCheckedChange={onTypeToggle(OrderType.POST_ONLY)}
          />
          <Tooltip
            content={t("orderEntry.orderType.postOnly.tooltip")}
            className="oui-max-w-[280px] oui-text-2xs oui-text-base-contrast-80"
          >
            <label
              htmlFor={"toggle_order_post_only"}
              className={cn(
                "oui-postOnly-label",
                "oui-ms-1 oui-text-2xs peer-data-[disabled]:oui-text-base-contrast-20",
                "oui-cursor-pointer oui-whitespace-nowrap oui-break-normal oui-border-b oui-border-dashed oui-border-line-12",
              )}
            >
              {t("orderEntry.orderType.postOnly")}
            </label>
          </Tooltip>
        </Flex>
        <Flex itemAlign={"center"}>
          <Checkbox
            data-testid="oui-testid-orderEntry-ioc-checkBox"
            id={"toggle_order_iov"}
            color={"white"}
            className={cn("oui-ioc-checkbox", "oui-peer")}
            variant={"radio"}
            checked={orderTypeExtra === OrderType.IOC}
            onCheckedChange={onTypeToggle(OrderType.IOC)}
            disabled={!props.showExtra}
          />
          <Tooltip
            content={t("orderEntry.orderType.ioc.tooltip")}
            className="oui-max-w-[280px] oui-text-2xs oui-text-base-contrast-80"
          >
            <label
              htmlFor={"toggle_order_iov"}
              className={cn(
                "oui-ioc-label",
                "oui-ms-1 oui-text-2xs peer-data-[disabled]:oui-text-base-contrast-20",
                "oui-cursor-pointer oui-whitespace-nowrap oui-break-normal oui-border-b oui-border-dashed oui-border-line-12",
              )}
            >
              {t("orderEntry.orderType.ioc")}
            </label>
          </Tooltip>
        </Flex>
        <Flex itemAlign={"center"}>
          <Checkbox
            data-testid="oui-testid-orderEntry-fox-checkBox"
            id={"toggle_order_fok"}
            color={"white"}
            variant={"radio"}
            className={cn("oui-fok-checkbox", "oui-peer")}
            checked={orderTypeExtra === OrderType.FOK}
            onCheckedChange={onTypeToggle(OrderType.FOK)}
            disabled={!props.showExtra}
          />
          <Tooltip
            content={t("orderEntry.orderType.fok.tooltip")}
            className="oui-max-w-[280px] oui-text-2xs oui-text-base-contrast-80"
          >
            <label
              htmlFor={"toggle_order_fok"}
              className={cn(
                "oui-fok-label",
                "oui-ms-1 oui-text-2xs peer-data-[disabled]:oui-text-base-contrast-20",
                "oui-cursor-pointer oui-whitespace-nowrap oui-break-normal oui-border-b oui-border-dashed oui-border-line-12",
              )}
            >
              {t("orderEntry.orderType.fok")}
            </label>
          </Tooltip>
        </Flex>
      </Flex>

      <Flex gapX={6}>
        <Flex>
          <Checkbox
            data-testid="oui-testid-orderEntry-orderConfirm-checkBox"
            id={"toggle_order_confirm"}
            color={"white"}
            className="oui-orderConfirm-checkbox"
            checked={props.needConfirm}
            onCheckedChange={(checked) => {
              props.setNeedConfirm(!!checked);
            }}
          />
          <label
            htmlFor={"toggle_order_confirm"}
            className={"oui-orderConfirm-label oui-ms-1 oui-text-2xs"}
          >
            {t("orderEntry.orderConfirm")}
          </label>
        </Flex>
        <Flex>
          <Checkbox
            data-testid="oui-testid-orderEntry-hidden-checkBox"
            id={"toggle_order_hidden"}
            color={"white"}
            className="oui-orderHidden-checkbox"
            checked={props.hidden}
            onCheckedChange={(checked: boolean) => {
              props.setHidden(checked);
            }}
          />
          <Tooltip
            content={t("orderEntry.hidden.tooltip")}
            className="oui-max-w-[280px] oui-text-2xs oui-text-base-contrast-80"
          >
            <label
              htmlFor={"toggle_order_hidden"}
              className={
                "oui-orderHidden-label oui-ms-1 oui-cursor-pointer oui-border-b oui-border-dashed oui-border-line-12 oui-text-2xs"
              }
            >
              {t("orderEntry.hidden")}
            </label>
          </Tooltip>
        </Flex>
      </Flex>
      {!pinned && (
        <>
          <Divider className={"oui-my-3"} />
          <Flex>
            <Switch
              data-testid="oui-testid-orderEntry-additional-keepVisible-switch"
              id={"toggle_order_keep_visible"}
              className="oui-keepVisible-switch"
              onCheckedChange={(checked) => {
                props.setPinned(checked);
              }}
            />
            <label
              htmlFor={"toggle_order_keep_visible"}
              className={"oui-keepVisible-label oui-ms-1 oui-text-2xs"}
            >
              {t("orderEntry.keepVisible")}
            </label>
          </Flex>
        </>
      )}
    </div>
  );
};
