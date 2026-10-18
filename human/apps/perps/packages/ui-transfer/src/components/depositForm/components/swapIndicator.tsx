import React from "react";
import { Flex, Text, TokenIcon } from "@paxeer-network/ui";
import { ArrowRightIcon } from "../../swap/icons";

export const SwapIndicator: React.FC<{
  sourceToken?: string;
  targetToken?: string;
  className?: string;
}> = (props) => {
  const { sourceToken, targetToken, className } = props;

  if (sourceToken === targetToken) {
    return null;
  }

  return (
    <Flex
      width="100%"
      itemAlign="center"
      justify="center"
      className={className}
    >
      <Flex itemAlign="center" justify="center" gap={1}>
        <Flex itemAlign="center" justify="center" gap={1}>
          <TokenIcon className="oui-size-[18px]" name={sourceToken} />
          <Text weight="semibold" intensity={80} size="sm">
            {sourceToken}
          </Text>
        </Flex>
        <ArrowRightIcon className="oui-size-4" />
        <Flex itemAlign="center" justify="center" gap={1}>
          <TokenIcon className="oui-size-[18px]" name={targetToken} />
          <Text weight="semibold" intensity={80} size="sm">
            {targetToken}
          </Text>
        </Flex>
      </Flex>
    </Flex>
  );
};
