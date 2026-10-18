import { FC } from "react";
import { Trans } from "@paxeer-network/i18n";
import { Flex, Text } from "@paxeer-network/ui";

export const ViewFAQs: FC = () => {
  return (
    <Flex justify="center" gapX={1} mt={3}>
      <Trans
        i18nKey="transfer.swapDeposit.viewFAQs"
        components={[
          <Text
            size="xs"
            color="primaryLight"
            onClick={() => {
              window.open("https://learn.woo.org/woofi/faqs/woofi-pro");
            }}
            className="oui-cursor-pointer"
            key="0"
          />,
        ]}
      />
    </Flex>
  );
};
