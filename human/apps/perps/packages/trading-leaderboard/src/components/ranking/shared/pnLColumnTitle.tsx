import { useTranslation } from "@paxeer-network/i18n";
import {
  useScreen,
  useLongPress,
  InfoCircleIcon,
  Tooltip,
  Flex,
  modal,
} from "@paxeer-network/ui";

export const PnLColumnTitle = ({ type }: { type?: "general" | "campaign" }) => {
  const { isMobile } = useScreen();
  const { t } = useTranslation();

  const tooltipContent =
    type === "general"
      ? t("tradingLeaderboard.realizedPnl.tooltip")
      : t("tradingLeaderboard.pnl.tooltip");

  const longPress = useLongPress(() => {
    modal.alert({
      title: t("common.tips"),
      message: tooltipContent,
    });
  });

  const view = (
    <Flex gap={1}>
      <div>
        {type === "general" ? t("common.realizedPnl") : t("common.pnl")}
      </div>
      <InfoCircleIcon opacity={1} className="w-4 h-4 cursor-pointer" />
    </Flex>
  );

  if (isMobile) {
    return <div {...longPress}>{view}</div>;
  }

  return <Tooltip content={tooltipContent}>{view}</Tooltip>;
};
