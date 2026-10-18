import { useMemo, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  DropdownMenuContent,
  DropdownMenuPortal,
  DropdownMenuRoot,
  DropdownMenuTrigger,
  Flex,
  Text,
  cn,
  Switch,
  injectable,
} from "@paxeer-network/ui";
import { CaretIcon, DisplaySettingIcon } from "../../icons";
import {
  DesktopDisplayControlMenuItem,
  DesktopDisplayControlMenuListProps,
  IProps,
} from "./common";

/**
 * Interceptor target path for desktop TradingView display-control dropdown list.
 */
export const DesktopDisplayControlMenuListTarget =
  "TradingView.DisplayControl.DesktopMenuList";

/**
 * Default desktop dropdown menu list renderer.
 * Plugins can intercept this target to append/reorder/filter menu rows.
 */
const DesktopDisplayControlMenuList: React.FC<
  DesktopDisplayControlMenuListProps
> = ({ items }) => {
  return (
    <>
      {items.map((item) => (
        <Flex
          key={item.id}
          justify={"between"}
          itemAlign={"center"}
          className="oui-w-full"
        >
          <Text
            className={cn(
              "oui-text-sm oui-text-base-contrast-80",
              !(item.checked ?? true) && "oui-text-base-contrast-36",
            )}
          >
            {item.label}
          </Text>
          <Switch
            className="oui-h-4 oui-w-8"
            checked={item.checked ?? true}
            disabled={item.disabled}
            onCheckedChange={(checked) => item.onCheckedChange?.(checked)}
          />
        </Flex>
      ))}
    </>
  );
};

const InjectableDesktopDisplayControlMenuList =
  injectable<DesktopDisplayControlMenuListProps>(
    DesktopDisplayControlMenuList,
    DesktopDisplayControlMenuListTarget,
  );

export const DesktopDisplayControl: React.FC<IProps> = (props) => {
  const { displayControlState, changeDisplayControlState } = props;
  const [open, setOpen] = useState(false);
  const { t } = useTranslation();

  const displayControlMap = useMemo<DesktopDisplayControlMenuItem[]>(() => {
    return [
      {
        label: t("common.position"),
        id: "position",
        checked: displayControlState.position ?? true,
        onCheckedChange: (checked) => {
          changeDisplayControlState({
            ...displayControlState,
            position: checked,
          });
        },
      },
      {
        label: t("tradingView.displayControl.buySell"),
        id: "buySell",
        checked: displayControlState.buySell ?? true,
        onCheckedChange: (checked) => {
          changeDisplayControlState({
            ...displayControlState,
            buySell: checked,
          });
        },
      },
      {
        label: t("tradingView.displayControl.limitOrders"),
        id: "limitOrders",
        checked: displayControlState.limitOrders ?? true,
        onCheckedChange: (checked) => {
          changeDisplayControlState({
            ...displayControlState,
            limitOrders: checked,
          });
        },
      },
      {
        label: t("tradingView.displayControl.stopOrders"),
        id: "stopOrders",
        checked: displayControlState.stopOrders ?? true,
        onCheckedChange: (checked) => {
          changeDisplayControlState({
            ...displayControlState,
            stopOrders: checked,
          });
        },
      },
      {
        label: t("common.tpsl"),
        id: "tpsl",
        checked: displayControlState.tpsl ?? true,
        onCheckedChange: (checked) => {
          changeDisplayControlState({
            ...displayControlState,
            tpsl: checked,
          });
        },
      },
      {
        label: t("tpsl.positionTpsl"),
        id: "positionTpsl",
        checked: displayControlState.positionTpsl ?? true,
        onCheckedChange: (checked) => {
          changeDisplayControlState({
            ...displayControlState,
            positionTpsl: checked,
          });
        },
      },
      {
        label: t("orderEntry.orderType.trailingStop"),
        id: "trailingStop",
        checked: displayControlState.trailingStop ?? true,
        onCheckedChange: (checked) => {
          changeDisplayControlState({
            ...displayControlState,
            trailingStop: checked,
          });
        },
      },
      {
        label: t("tradingView.displayControl.liquidationPrice"),
        id: "liquidationPrice",
        checked: displayControlState.liquidationPrice ?? true,
        onCheckedChange: (checked) => {
          changeDisplayControlState({
            ...displayControlState,
            liquidationPrice: checked,
          });
        },
      },
    ];
  }, [changeDisplayControlState, displayControlState, t]);

  return (
    <>
      <DropdownMenuRoot open={open} onOpenChange={setOpen}>
        <DropdownMenuTrigger asChild>
          <Flex
            justify="start"
            itemAlign="center"
            className="oui-gap-[2px] oui-cursor-pointer oui-text-base-contrast-36  hover:oui-text-base-contrast-80"
          >
            <DisplaySettingIcon
              className={cn(
                "oui-w-[18px] oui-h-[18px] ",
                open && "oui-text-base-contrast-80",
              )}
            />
            <CaretIcon
              className={cn(
                "oui-w-3 oui-h-3",
                open && "oui-text-base-contrast-80 oui-rotate-180",
              )}
            />
          </Flex>
        </DropdownMenuTrigger>
        <DropdownMenuPortal>
          <DropdownMenuContent
            onCloseAutoFocus={(e) => e.preventDefault()}
            onClick={(e) => e.stopPropagation()}
            align="start"
            className="oui-bg-base-8"
          >
            <Flex
              direction="column"
              gap={4}
              px={5}
              py={5}
              width={240}
              justify="start"
              itemAlign="start"
            >
              <InjectableDesktopDisplayControlMenuList
                items={displayControlMap}
              />
            </Flex>
          </DropdownMenuContent>
        </DropdownMenuPortal>
      </DropdownMenuRoot>
    </>
  );
};
