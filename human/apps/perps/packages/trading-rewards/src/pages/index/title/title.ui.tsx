import { Divider, Flex, Text } from "@paxeer-network/ui";
import { FC } from "react";
import { TitleConfig } from "./title.script";
import { useTranslation } from "@paxeer-network/i18n";

export const Title: FC<TitleConfig> = (props) => {
  const { title, subtitle, content, docOpenOptions } = props;
  const { t } = useTranslation();

  return (
    <Flex
      id="oui-tradingRewards-home-title"
      p={6}
      direction={"column"}
      itemAlign={"start"}
      gap={4}
      className="oui-bg-base-9 oui-font-semibold"
      r={"2xl"}
      width={"100%"}
    >
      {title || <Text size="lg">{t("common.tradingRewards")}</Text>}
      <Divider intensity={8} className="oui-w-full" />
      <Flex direction={"column"} itemAlign={"start"} gap={1}>
        {subtitle || (
          <Text size="base">
            {t("tradingRewards.subtitle", {
              brokerName: props.brokerName,
            })}
          </Text>
        )}

        {content || <MultiLineText docOpenOptions={docOpenOptions} />}
      </Flex>
    </Flex>
  );
};

const MultiLineText: FC<{
  docOpenOptions?: {
    url?: string;
    target?: string;
    features?: string;
  };
}> = (props) => {
  const { t } = useTranslation();

  // const clickableParts = [
  //   {
  //     content: "Learn more here",
  //     isLink: true,
  //   },
  //   // {
  //   //   content: "about paxeer-network Trading rewards Program.",
  //   //   isLink: false,
  //   // },
  //   // {
  //   //   content: "Trading rewards Docs",
  //   //   isLink: true,
  //   // },
  // ];
  // const renderText = () => {
  //   return clickableParts.map((item, index) => {
  //     return (
  //       <span
  //         key={index}
  //         className={`${
  //           item.isLink
  //             ? "oui-text-primary-light hover:oui-text-primary-darken oui-cursor-pointer"
  //             : ""
  //         }`}
  //         dangerouslySetInnerHTML={{ __html: item.content }}
  //       />
  //     );
  //   });
  // };

  return (
    <div
      className="oui-text-sm oui-text-base-contrast-54 oui-font-normal oui-flex oui-gap-1 oui-items-center hover:oui-text-primary-darken oui-text-primary-light oui-cursor-pointer"
      onClick={() => [
        window.open(
          props.docOpenOptions?.url,
          props.docOpenOptions?.target,
          props.docOpenOptions?.features
        ),
      ]}
    >
      <span className="oui-text-primary-light hover:oui-text-primary-darken oui-cursor-pointer">
        {t("tradingRewards.learnMore")}
      </span>
      <svg
        width="16"
        height="16"
        viewBox="0 0 16 16"
        fill="currentColor"
        xmlns="http://www.w3.org/2000/svg"
      >
        <path d="M4.008 7.995c0-.368.298-.666.666-.666H9.71L7.733 5.331l.937-.936 3.143 3.122c.13.13.195.304.195.479a.67.67 0 0 1-.195.478L8.67 11.596l-.937-.937 1.978-1.998H4.674a.666.666 0 0 1-.666-.666" />
      </svg>
    </div>
  );
};

export default MultiLineText;
