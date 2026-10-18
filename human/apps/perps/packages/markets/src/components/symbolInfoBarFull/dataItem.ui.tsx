import React from "react";
import { cn, Flex, Tooltip, Text } from "@paxeer-network/ui";

interface DataItemProps {
  label: string;
  value: React.ReactNode;
  hint?: React.ReactNode;
}

export const DataItem: React.FC<DataItemProps> = (props) => {
  const { label, value, hint } = props;
  return (
    <Flex direction="column" itemAlign="start">
      <Tooltip
        open={hint ? undefined : false}
        content={hint}
        className="oui-max-w-[330px] oui-bg-base-6 "
        arrow={{ className: "oui-fill-base-6" }}
        delayDuration={300}
      >
        <Text
          size="2xs"
          intensity={36}
          className={cn(
            "oui-data-label",
            "oui-whitespace-nowrap oui-break-normal",
            hint &&
              "oui-cursor-pointer oui-border-b oui-border-dashed oui-border-line-12",
          )}
        >
          {label}
        </Text>
      </Tooltip>
      <Text
        size="2xs"
        intensity={98}
        className={cn(
          "oui-data-value",
          "oui-whitespace-nowrap oui-break-normal oui-leading-[20px]",
        )}
      >
        {value}
      </Text>
    </Flex>
  );
};
