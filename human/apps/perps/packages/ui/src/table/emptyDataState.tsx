import { type FC } from "react";
import { injectable } from "@paxeer-network/plugin-core";
import { Box } from "../box";
import { Flex } from "../flex";
import EmptyStateIcon from "../icon/emptyData";
import { useLocale } from "../locale";
import { Text } from "../typography";

/** Props for Table.EmptyDataIdentifier injectable; used by plugins for typed interceptor */
export interface EmptyDataStateProps {
  title?: string;
  className?: string;
}

export const EmptyDataState: FC<EmptyDataStateProps> = (props) => {
  const [locale] = useLocale("empty");

  return (
    <Flex
      itemAlign="center"
      direction="column"
      gapY={4}
      className={props.className}
    >
      <Box>
        <EmptyStateIcon />
      </Box>
      <Text as="div" intensity={36} size="2xs">
        {props.title ?? locale.description}
      </Text>
    </Flex>
  );
};

/** Injectable default for Table.EmptyDataIdentifier slot - plugins can intercept via PaxeerNetworkPluginProvider */
export const InjectableEmptyDataState = injectable(
  EmptyDataState,
  "Table.EmptyDataIdentifier",
);
