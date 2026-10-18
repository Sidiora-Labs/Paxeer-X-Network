import { Flex, Box, cn } from "@paxeer-network/ui";
import { SummaryWidget } from "./summary";
import { TitleStatisticWidget } from "./titleStatistic";
import { RebatesWidget } from "./rebates";
export const TraderPage = () => {
  return (
    <Flex
        id="oui-affiliate-trader-page"
        className={cn(
          "oui-h-lvw oui-w-full",
          // padding
          // "oui-p-4 lg:oui-p-6 xl:oui-p-3",
          "oui-font-semibold"
        )}
        direction={"column"}
        gap={4}
      >
        <Flex
          width={"100%"}
          gap={4}
          className="oui-flex oui-flex-col xl:oui-flex-row xl:oui-items-stretch"
        >
          <SummaryWidget />
          <TitleStatisticWidget />
        </Flex>
        <RebatesWidget />
      </Flex>
  );
};
