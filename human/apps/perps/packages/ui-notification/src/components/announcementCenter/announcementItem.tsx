import { FC, useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { AnnouncementType } from "@paxeer-network/types";
import { Flex, Text, ChevronDownIcon, cn } from "@paxeer-network/ui";
import {
  AnnouncementIcon,
  ArrowRightShortIcon,
  CampaignIcon,
  FundIcon,
  SecurityIcon,
} from "./icons";

export const AnnouncementItem: FC<{
  // title: string;
  expanded: boolean;
  url?: string | null;
  onItemClick: (url: string) => void;
  onExpandToggle?: () => void;
  type: AnnouncementType | undefined | null;
  message: string;
  updatedTime: number;
  className?: string;
  showDivider?: boolean;
}> = (props) => {
  const { t } = useTranslation();
  const Icon = useMemo(() => {
    switch (props.type) {
      case AnnouncementType.Campaign:
        return CampaignIcon;

      case AnnouncementType.Listing:
        return FundIcon;
      case AnnouncementType.Maintenance:
        return SecurityIcon;
      case AnnouncementType.Delisting:
      default:
        return AnnouncementIcon;
    }
  }, [props.type]);

  const title = useMemo(() => {
    switch (props.type) {
      case AnnouncementType.Campaign:
        return t("notification.campaign");
      case AnnouncementType.Delisting:
        return t("notification.delisting");
      case AnnouncementType.Listing:
        return t("notification.listing");
      case AnnouncementType.Maintenance:
        return t("notification.maintenance");
      case AnnouncementType.Vote:
        return t("notification.vote");
      default:
        return t("notification.general");
    }
  }, [props.type, t]);

  const action = useMemo(() => {
    if (
      (props.type === AnnouncementType.Campaign ||
        props.type === AnnouncementType.Vote) &&
      typeof props.url === "string" &&
      props.url !== "" &&
      typeof props.onItemClick === "function"
    ) {
      return (
        <Flex
          gap={1}
          itemAlign="center"
          className="oui-cursor-pointer"
          onClick={(event) => {
            event.stopPropagation();
            props.onItemClick(props.url!);
          }}
        >
          <Text
            size="xs"
            color="buy"
            className="oui-bg-clip-text oui-text-transparent oui-gradient-brand"
          >
            {t("notification.joinNow")}
          </Text>
          <ArrowRightShortIcon size={18} />
        </Flex>
      );
    }
    return null;
  }, [props.type, props.url, t]);

  const updateTime = useMemo(() => {
    if (props.type === AnnouncementType.Maintenance) {
      return (
        <Text size="2xs" intensity={36}>
          {t("notification.recentlyUpdated")}
        </Text>
      );
    }
    return (
      <Text.formatted
        size="2xs"
        intensity={36}
        rule="date"
        formatString="yyyy-MM-dd HH:mm:ss"
      >
        {props.updatedTime}
      </Text.formatted>
    );
  }, [props.updatedTime, props.type, t]);

  return (
    <Flex
      gap={2}
      itemAlign="start"
      className={cn(
        "oui-px-2 oui-py-[6px] oui-text-base-contrast-80",
        !props.showDivider && "oui-rounded-md hover:oui-bg-base-6",
        !props.showDivider && props.expanded && "oui-bg-base-6",
        props.className,
      )}
      onClick={() => {
        props.onExpandToggle?.();
      }}
    >
      <Icon color="white" className="oui-mt-3 oui-shrink-0" />

      <Flex direction="column" itemAlign="start" grow>
        <Text size="xs" intensity={80} weight="bold">
          {title}
        </Text>
        {updateTime}
        {/* Expandable content with animation */}
        <div
          className="oui-grid oui-transition-all oui-duration-300 oui-ease-in-out"
          style={{
            gridTemplateRows: props.expanded ? "1fr" : "0fr",
          }}
        >
          <div className="oui-flex oui-flex-col oui-gap-2 oui-overflow-hidden">
            <Text size="2xs" intensity={80} as="div" className="oui-pt-2">
              {props.message}
            </Text>
            {action}
          </div>
        </div>
      </Flex>
      <div className="oui-pt-3">
        {/* Chevron icon with rotation animation */}
        <ChevronDownIcon
          color="white"
          size={18}
          className={`oui-transition-transform oui-duration-300 oui-ease-in-out ${
            props.expanded ? "oui-rotate-180" : "oui-rotate-0"
          }`}
        />
      </div>
    </Flex>
  );
};
