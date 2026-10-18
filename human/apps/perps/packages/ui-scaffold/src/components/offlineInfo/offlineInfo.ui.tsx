import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Button, cn, Flex, RefreshIcon, Text } from "@paxeer-network/ui";
import { InfoIcon } from "../icons";
import { OfflineInfoState } from "./offlineInfo.script";

export type OfflineInfoProps = OfflineInfoState & {
  style?: React.CSSProperties;
  className?: string;
  onRefresh?: () => void;
  onStatusChange?: (last: boolean, next: boolean) => void;
};

export const OfflineInfo: FC<OfflineInfoProps> = (props) => {
  const { offline } = props;
  const { t } = useTranslation();

  if (!offline) {
    return null;
  }
  return (
    <Flex
      className={cn(
        "oui-offline-info oui-text-warning-darken oui-bg-warning-darken/15 oui-rounded",
        props.className,
      )}
      style={props.style}
      itemAlign="center"
      justify="center"
      gap={1}
      py={1}
      px={2}
    >
      <InfoIcon size={20} className="oui-size-4 oui-shrink-0 lg:oui-size-5" />
      <Text>{t("common.offlineDescription")}</Text>
      <Button
        variant={"contained"}
        color="warning"
        size={"xs"}
        icon={<RefreshIcon />}
        className="oui-bg-warning-darken"
        onClick={() => {
          if (props.onRefresh) {
            props.onRefresh();
          } else {
            window.location.reload();
          }
        }}
      >
        <Text>{t("common.refresh")}</Text>
      </Button>
    </Flex>
  );
};
