import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { RouterAdapter } from "@paxeer-network/types";
import {
  Flex,
  Text,
  ArrowDownSquareFillIcon,
  ArrowUpSquareFillIcon,
  CalendarMinusIcon,
  ArrowLeftRightIcon,
} from "@paxeer-network/ui";
import { PortfolioLeftSidebarPath } from "../../../layout";

type Props = {
  disabled: boolean;
  onWithdraw?: () => void;
  onDeposit?: () => void;
  onTransfer?: () => void;
  isMainAccount?: boolean;
  routerAdapter?: RouterAdapter;
  hasSubAccount?: boolean;
};

export const PortfolioHandleMobile: FC<Props> = (props) => {
  const { t } = useTranslation();

  const onGotoHistory = () => {
    props.routerAdapter?.onRouteChange({
      href: PortfolioLeftSidebarPath.History,
      name: t("trading.history"),
    });
  };

  return (
    <Flex
      direction={"row"}
      width={"100%"}
      height={"71px"}
      className="oui-gap-3 oui-bg-transparent"
    >
      {props.isMainAccount && (
        <Flex
          direction="column"
          gapY={2}
          itemAlign={"center"}
          className="oui-flex-1 oui-cursor-pointer"
          onClick={props?.onDeposit}
        >
          <div className="oui-flex oui-size-[48px] oui-items-center oui-justify-center oui-rounded-xl oui-bg-base-9">
            <ArrowDownSquareFillIcon size={28} color="white" opacity={1} />
          </div>
          <Text className="oui-text-base-80 oui-text-2xs">
            {t("common.deposit")}
          </Text>
        </Flex>
      )}
      {props.hasSubAccount && (
        <Flex
          direction="column"
          gapY={2}
          itemAlign={"center"}
          className="oui-flex-1 oui-cursor-pointer"
          onClick={props?.onTransfer}
        >
          <div className="oui-flex oui-size-[48px] oui-items-center oui-justify-center oui-rounded-xl oui-bg-base-9">
            <ArrowLeftRightIcon size={28} color="white" opacity={1} />
          </div>
          <Text className="oui-text-base-80 oui-text-2xs">
            {t("common.transfer")}
          </Text>
        </Flex>
      )}
      {props.isMainAccount && (
        <Flex
          direction="column"
          gapY={2}
          itemAlign={"center"}
          className="oui-flex-1 oui-cursor-pointer"
          onClick={props?.onWithdraw}
        >
          <div className="oui-flex oui-size-[48px] oui-items-center oui-justify-center oui-rounded-xl oui-bg-base-9">
            <ArrowUpSquareFillIcon size={28} color="white" opacity={1} />
          </div>
          <Text className="oui-text-base-80 oui-text-2xs">
            {t("common.withdraw")}
          </Text>
        </Flex>
      )}
      <Flex
        direction="column"
        gapY={2}
        itemAlign={"center"}
        className="oui-flex-1 oui-cursor-pointer"
      >
        <div
          className="oui-flex oui-size-[48px] oui-items-center oui-justify-center oui-rounded-xl oui-bg-base-9"
          onClick={onGotoHistory}
        >
          <CalendarMinusIcon
            size={28}
            color="white"
            opacity={1}
            viewBox="0 0 28 28"
            className="oui-w-[28px] oui-h-[28px]"
          />
        </div>
        <Text className="oui-text-base-80 oui-text-2xs">
          {t("trading.history")}
        </Text>
      </Flex>
    </Flex>
  );
};
