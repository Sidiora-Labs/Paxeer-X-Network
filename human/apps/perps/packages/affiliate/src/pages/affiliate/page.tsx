import { Flex, cn } from "@paxeer-network/ui";
import { SummaryWidget } from "./summary";
import { ReferralLinkWidget } from "./referralLink";
import { TitleStatisticWidget } from "./titleStatistic";
import { ReferralCodesWidget } from "./referralCodes";
import { CommissionAndRefereesWidget } from "./commissionAndReferees";
import { useMediaQuery } from "@paxeer-network/hooks";

export const AffiliatePage = () => {
  return (
    <Flex
      id="oui-affiliate-affiliate-page"
      className={cn(
        "oui-h-lvw",
        // padding
        // "oui-p-4 lg:oui-p-6 xl:oui-p-3",
        "oui-font-semibold"
      )}
      direction={"column"}
      gap={4}
    >
      <Layout />
      <CommissionAndRefereesWidget />
    </Flex>
  );
};

const Layout = () => {
  const is2XL = useMediaQuery("(max-width: 1279px)");

  if (is2XL) {
    return <Layout1024 />;
  }

  return <Layout1280 />;
};

const Layout1024 = () => {
  return (
    <>
      <div className="oui-flex oui-flex-col xl:oui-flex-row oui-gap-4 oui-w-full">
        <SummaryWidget />
        <ReferralLinkWidget />
      </div>
      <TitleStatisticWidget />
      <ReferralCodesWidget />
    </>
  );
};

const Layout1280 = () => {
  return (
    <Flex direction={"row"} itemAlign={"stretch"} gap={4}>
      <Flex direction={"column"} gap={4} className="oui-flex-1 oui-w-1/2">
        <div className="oui-flex-1 oui-h-full oui-w-full">
          <SummaryWidget />
        </div>
        <TitleStatisticWidget />
      </Flex>
      <Flex direction={"column"} gap={4} className="oui-w-1/2">
        <ReferralLinkWidget />
        {/* <div className="oui-h-[360px]"> */}
        <div className="oui-flex-1">
          <ReferralCodesWidget />
        </div>
        {/* </div> */}
      </Flex>
    </Flex>
  );
};
