import { ReactNode } from "react";
import { Flex, Text, WarningIcon } from "@paxeer-network/ui";

type WarningBoxProps = {
  children: ReactNode;
};

export const WarningBox = (props: WarningBoxProps) => (
  <Flex
    className="oui-bg-warning/10"
    justify="start"
    itemAlign="start"
    gap={1}
    r="lg"
    p={3}
  >
    <WarningIcon className="oui-shrink-0 oui-text-warning" />
    {typeof props.children === "string" ? (
      <Text size="2xs" intensity={54} className="oui-text-warning">
        {props.children}
      </Text>
    ) : (
      props.children
    )}
  </Flex>
);
