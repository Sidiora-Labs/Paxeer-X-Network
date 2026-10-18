import { FC } from "react";
import { Box, Flex, Text, WarningIcon } from "@paxeer-network/ui";

type WarningBannerProps = {
  message: string;
};

export const WarningBanner: FC<WarningBannerProps> = ({ message }) => {
  return (
    <Flex
      itemAlign="start"
      gap={1}
      className="oui-mt-4 oui-w-[calc(100%+2.5rem)] oui-rounded oui-bg-warning-darken/10 oui-px-5 oui-py-2 oui-text-warning-darken"
    >
      <Box className="oui-mt-0.5">
        <WarningIcon width={16} height={16} />
      </Box>
      <Text
        size="xs"
        intensity={80}
        className="oui-w-full oui-text-warning-darken"
      >
        {message}
      </Text>
    </Flex>
  );
};
