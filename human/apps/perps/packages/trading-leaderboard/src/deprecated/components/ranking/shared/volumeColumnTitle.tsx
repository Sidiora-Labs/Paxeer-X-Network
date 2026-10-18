import { useTranslation } from "@paxeer-network/i18n";
import {
  useScreen,
  useLongPress,
  InfoCircleIcon,
  Tooltip,
  Flex,
  modal,
} from "@paxeer-network/ui";

export const VolumeColumnTitle = () => {
  const { isMobile } = useScreen();
  const { t } = useTranslation();

  const tooltipContent =
    "Total trading volume generated during the campaign period. Updated every 30 seconds.";

  const longPress = useLongPress(() => {
    modal.alert({
      title: t("common.tips"),
      message: tooltipContent,
    });
  });

  const view = (
    <Flex gap={1}>
      <div>{t("tradingLeaderboard.tradingVolume")}</div>
      <InfoCircleIcon opacity={1} className="w-4 h-4 cursor-pointer" />
    </Flex>
  );

  if (isMobile) {
    return <div {...longPress}>{view}</div>;
  }

  return <Tooltip content={tooltipContent}>{view}</Tooltip>;
};
