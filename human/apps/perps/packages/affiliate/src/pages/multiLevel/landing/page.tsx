import { Flex } from "@paxeer-network/ui";
import { Faq } from "./components/faq";
import { Hero } from "./components/hero";
import { HowItWorks } from "./components/howItWorks";

export const LandingPage = () => {
  return (
    <Flex
      id="oui-affiliate-landing-page"
      direction="column"
      itemAlign={"stretch"}
      className="oui-affiliate-landing-page oui-mx-auto oui-w-full oui-p-5 md:oui-w-[1040px] md:oui-py-12"
    >
      <Hero />
      <HowItWorks />
      <Faq />
    </Flex>
  );
};
