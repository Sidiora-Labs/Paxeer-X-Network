import { FC, ReactNode } from "react";
import { Flex } from "@paxeer-network/ui";
import { ArrowDownIcon } from "../../icons";

type ExchangeDividerProps = {
  icon?: ReactNode;
};

export const ExchangeDivider: FC<ExchangeDividerProps> = ({ icon }) => {
  return (
    <Flex height={40} gapX={3}>
      <Flex height={1} className="oui-flex-1 oui-bg-base-contrast-12"></Flex>
      {icon || <ArrowDownIcon className="oui-text-primary-light" />}
      <Flex height={1} className="oui-flex-1 oui-bg-base-contrast-12"></Flex>
    </Flex>
  );
};
