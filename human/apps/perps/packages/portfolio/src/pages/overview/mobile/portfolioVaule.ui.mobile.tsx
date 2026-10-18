import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { RouterAdapter } from "@paxeer-network/types";
import {
  Flex,
  Text,
  cn,
  ArrowRightShortIcon,
  EyeIcon,
} from "@paxeer-network/ui";
import { PortfolioLeftSidebarPath } from "../../../layout";

type Props = {
  portfolioValue: number | null;
  unrealPnL: number;
  unrealROI: number;
  visible: boolean;
  toggleVisible: () => void;
  canTrade: boolean;
  routerAdapter?: RouterAdapter;
};

export const PortfolioValueMobile: FC<Props> = (props) => {
  const { t } = useTranslation();
  return (
    <Flex
      direction={"column"}
      width={"100%"}
      height={"100%"}
      className="oui-relative oui-items-start oui-overflow-hidden oui-rounded-2xl oui-bg-primary-darken"
      p={4}
    >
      <Flex direction="row" gapX={1} itemAlign={"center"}>
        <Text className="oui-text-sm oui-text-primary-contrast/[0.54]">
          {t("portfolio.overview.handle.title")}
        </Text>
        <EyeIcon
          size={16}
          className={cn(
            props.canTrade ? "oui-text-primary-contrast/[0.54]" : "oui-hidden",
          )}
          onClick={props.toggleVisible}
        />
      </Flex>
      <Flex
        direction="row"
        gapX={1}
        itemAlign={"baseline"}
        className="oui-mt-1"
      >
        <Text.numeral
          visible={props.visible}
          className="oui-text-3xl oui-font-bold oui-text-primary-contrast"
        >
          {props.portfolioValue ?? "--"}
        </Text.numeral>
        <Text className="oui-text-base oui-font-bold oui-text-primary-contrast/80">
          USDC
        </Text>
      </Flex>
      <Flex
        direction="row"
        gapX={1}
        itemAlign={"center"}
        className="oui-text-sm oui-text-primary-contrast"
      >
        <Text.pnl visible={props.visible}>{props.unrealPnL ?? "--"}</Text.pnl>
        <Text.roi
          visible={props.visible}
          rule="percentages"
          prefix={"("}
          suffix={")"}
        >
          {props.unrealROI ?? "--"}
        </Text.roi>
      </Flex>
      <div
        className="oui-absolute oui-end-0 oui-top-0 oui-flex oui-h-full oui-items-center oui-justify-center oui-px-4"
        onClick={() =>
          props.routerAdapter?.onRouteChange({
            href: PortfolioLeftSidebarPath.Assets,
            name: "Assets",
          })
        }
      >
        <ArrowRightShortIcon
          size={18}
          opacity={0.99}
          className="oui-text-primary-contrast"
        />
      </div>
    </Flex>
  );
};
