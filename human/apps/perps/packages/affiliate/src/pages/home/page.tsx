import { Flex, Box, cn } from "@paxeer-network/ui";
import { BecomeAffiliateWidget } from "./becomeAffiliate";
import { TopWidget } from "./top";
import { CardWidget } from "./card";

export const HomePage = () => {
  return (
    <Flex
      id="oui-affiliate-home-page"
      className={cn(
        "oui-h-lvw ",
        // padding
        // "oui-p-4 lg:oui-p-6 xl:oui-p-3",
        "oui-font-semibold"
      )}
      direction={"column"}
      gap={4}
    >
      <TopWidget />
      <CardWidget />
      <BecomeAffiliateWidget />
    </Flex>
  );
};
