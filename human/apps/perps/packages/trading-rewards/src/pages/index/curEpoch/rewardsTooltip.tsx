import React, { FC, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Flex, Text, Tooltip } from "@paxeer-network/ui";
import { commifyOptional } from "@paxeer-network/utils";

export type RewardsTooltipProps = {
  brokerName: string | undefined;
  curRewards: number;
  otherRewards: number;
};

export const RewardsTooltip: FC<{
  rewardsTooltip?: RewardsTooltipProps;
  children?: React.ReactElement;
  className?: string;
  arrowClassName?: string;
  align?: "start" | "center" | "end";
}> = (props) => {
  const [open, setOpen] = useState(false);
  const { t } = useTranslation();

  const content = (): any => {
    return (
      <Flex
        direction={"column"}
        className="oui-leading-[1.5] oui-text-2xs oui-text-base-contrast-80 oui-min-w-[204px]"
        gap={1}
      >
        <Flex gap={1} width={"100%"}>
          <Text className="oui-flex-1">{props.rewardsTooltip?.brokerName}</Text>
          <Text>
            {commifyOptional(props.rewardsTooltip?.curRewards, { fix: 2 })}
          </Text>
        </Flex>
        <Flex gap={1} width={"100%"}>
          <Text className="oui-flex-1">
            {t("tradingRewards.otherPaxeerNetworkDex")}
          </Text>
          <Text>
            {commifyOptional(props.rewardsTooltip?.otherRewards, { fix: 2 })}
          </Text>
        </Flex>
      </Flex>
    );
  };

  return (
    <Tooltip
      content={content()}
      align={props.align}
      className={props.className}
      open={open}
      onOpenChange={setOpen}
      arrow={{
        className: props.arrowClassName,
      }}
      delayDuration={100}
    >
      {!!props.children ? (
        React.cloneElement(props.children!, {
          onClick: (e: any) => {
            e.preventDefault();
            setOpen(!open);
          },
        })
      ) : (
        <svg
          width="21"
          height="20"
          viewBox="0 0 21 20"
          fill="none"
          xmlns="http://www.w3.org/2000/svg"
          className="oui-cursor-pointer"
          onClick={(e) => {
            e.preventDefault();
            setOpen(!open);
          }}
        >
          <path
            d="M10.5 1.678a8.333 8.333 0 1 0-.001 16.667 8.333 8.333 0 0 0 0-16.667m0 4.167a.833.833 0 1 1-.001 1.667.833.833 0 0 1 0-1.667m0 2.5c.46 0 .832.373.832.833v4.167a.833.833 0 0 1-1.666 0V9.178c0-.46.373-.833.833-.833"
            fill="rgb(var(--oui-color-base-foreground))"
            fillOpacity=".36"
          />
        </svg>
      )}
    </Tooltip>
  );
};
