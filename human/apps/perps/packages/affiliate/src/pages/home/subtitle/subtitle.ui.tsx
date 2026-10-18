import { FC } from "react";
import { SubtitleReturns } from "./subtitle.script";
import { Flex, Text } from "@paxeer-network/ui";
import { useTranslation } from "@paxeer-network/i18n";

export const Subtitle: FC<SubtitleReturns> = (props) => {
  const { t } = useTranslation();

  return (
    <Flex
      id="oui-affiliate-home-subtitle"
      direction={"column"}
      className="oui-text-sm md:oui-text-base xl:oui-text-lg oui-text-base-contrast-80 oui-text-center"
      gap={3}
    >
      <Text>{t("affiliate.page.subTitle")}</Text>
      <Flex
        direction={"row"}
        gap={1}
        className="oui-text-primary-light oui-fill-primary-light hover:oui-text-primary-darken oui-cursor-pointer oui-text-2xs md:oui-text-xs xl:oui-text-sm"
        onClick={(e) => {
          if (props.onLearnAffiliate) {
            props.onLearnAffiliate?.();
          } else if (props.learnAffiliateUrl) {
            window.open(props.learnAffiliateUrl, "_blank");
          }
        }}
      >
        <Text children={t("affiliate.page.learnMore")} />
        <svg
          width="16"
          height="16"
          viewBox="0 0 16 16"
          fill="currentColor"
          xmlns="http://www.w3.org/2000/svg"
        >
          <path d="M4.008 7.995c0-.368.298-.666.666-.666H9.71L7.733 5.331l.937-.936 3.143 3.122c.13.13.195.304.195.479a.67.67 0 0 1-.195.478L8.67 11.596l-.937-.937 1.978-1.998H4.674a.666.666 0 0 1-.666-.666" />
        </svg>
      </Flex>
    </Flex>
  );
};
