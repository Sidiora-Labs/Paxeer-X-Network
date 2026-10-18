import { FC, ReactNode, useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Flex, Text, cn } from "@paxeer-network/ui";
import { commify } from "@paxeer-network/utils";
import { useReferralContext } from "../../../provider";
import { BecomeAffiliateReturns } from "./becomeAffiliate.script";

export const BecomeAffiliate: FC<BecomeAffiliateReturns> = (props) => {
  const { t } = useTranslation();

  const { generateCode } = useReferralContext();

  const applyText = useMemo(() => {
    if (!generateCode) {
      return {
        title: t("affiliate.process.step1.title"),
        desc: t("affiliate.process.step1.description"),
      };
    }
    if (generateCode.requireVolume > 0) {
      return {
        title: t("affiliate.process.step1.volumeGt0.title", {
          requireVolume: generateCode.requireVolume,
        }),
        desc: t("affiliate.process.step1.volumeGt0.description", {
          volume: commify(generateCode.completedVolume ?? 0, 2),
          requireVolume: commify(generateCode.requireVolume),
        }),
      };
    }
    // require volume = 0;
    return {
      title: t("affiliate.process.step1.volumeEq0.title"),
      desc: t("affiliate.process.step1.volumeEq0.description"),
    };
  }, [generateCode, t]);

  if (typeof props.overwrite === "function") {
    return props.overwrite?.(props.state);
  }

  return (
    <Flex
      id="oui-affiliate-home-becomeAffiliate"
      direction={"column"}
      gap={6}
      p={6}
      itemAlign={"center"}
      r={"2xl"}
      className="oui-bg-base-9"
      width={"100%"}
    >
      <Text>{t("affiliate.process.title")}</Text>
      <Flex className="oui-flex oui-flex-col oui-gap-3 xl:oui-w-full xl:oui-flex-row xl:oui-items-stretch">
        <Item
          icon={<ApplyIcon />}
          title={applyText.title}
          content={applyText.desc}
        />
        <div className="oui-shrink xl:oui-flex xl:-oui-rotate-90 xl:oui-flex-row xl:oui-items-center xl:oui-justify-center">
          <ArrowDownIcon />
        </div>
        <Item
          icon={<ShareIcon />}
          title={t("affiliate.process.step2.title")}
          content={t("affiliate.process.step2.description")}
        />
        <div className="oui-shrink xl:oui-flex xl:-oui-rotate-90 xl:oui-flex-row xl:oui-items-center xl:oui-justify-center">
          <ArrowDownIcon />
        </div>
        <Item
          icon={<EarnIcon />}
          title={t("affiliate.process.step3.title")}
          content={t("affiliate.process.step3.description")}
        />
      </Flex>
    </Flex>
  );
};

const Item: FC<{
  icon: ReactNode;
  title: string;
  content: string;
}> = (props) => {
  return (
    <Flex
      className={cn(
        "oui-flex oui-flex-row oui-gap-3",
        "xl:oui-flex-1 xl:oui-flex-col xl:oui-gap-[6px]",
      )}
      width={"100%"}
    >
      <div className="oui-shrink-0">{props.icon}</div>
      <Flex
        className={cn(
          "oui-flex oui-flex-col oui-items-start oui-h-full oui-justify-between",
          // lg
          "xl:oui-items-center xl:oui-justify-start",
        )}
      >
        <Text className="oui-text-sm md:oui-text-base 2xl:oui-text-lg xl:oui-text-center">
          {props.title}
        </Text>
        <Text className="oui-text-2xs oui-text-base-contrast-36  xl:oui-text-center">
          {props.content}
        </Text>
      </Flex>
    </Flex>
  );
};

const ApplyIcon = () => {
  return (
    <svg
      width="48"
      height="48"
      viewBox="0 0 48 48"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      className="xl:oui-w-[64px] xl:oui-h-[64px] 2xl:oui-w-[80px] 2xl:oui-h-[80px]"
    >
      <path
        fillRule="evenodd"
        clipRule="evenodd"
        d="M24.941 5.4h-6.682a.6.6 0 0 0-.593.509l-.462 3a.6.6 0 0 0 .593.691h7.606a.6.6 0 0 0 .593-.691l-.462-3a.6.6 0 0 0-.593-.509m-6.682-1.8a2.4 2.4 0 0 0-2.372 2.035l-.462 3a2.4 2.4 0 0 0 2.372 2.765h7.606a2.4 2.4 0 0 0 2.372-2.765l-.462-3A2.4 2.4 0 0 0 24.941 3.6z"
        fill="rgb(var(--oui-color-base-foreground))"
        fillOpacity=".36"
      />
      <path
        d="M32.721 28.2c.924-1.6 3.233-1.6 4.157 0l7.275 12.6c.924 1.6-.231 3.6-2.079 3.6H27.525c-1.848 0-3.002-2-2.078-3.6z"
        fill="rgb(var(--oui-color-primary-darken))"
      />
      <path
        d="M33.492 38.12H31.56c-.252 0-.426-.276-.336-.535l1.92-5.53a.4.4 0 0 1 .132-.185.34.34 0 0 1 .204-.07h3.24c.255 0 .43.284.333.544l-1.118 3.011h2.104c.31 0 .475.4.27.656l-5.158 6.453c-.251.313-.716.041-.619-.362z"
        fill="rgb(var(--oui-color-base-foreground))"
        fillOpacity=".98"
      />
      <path
        fillRule="evenodd"
        clipRule="evenodd"
        d="M14.4 16.8v2.4h2.4v-2.4zM13.2 15a.6.6 0 0 0-.6.6v4.8a.6.6 0 0 0 .6.6H18a.6.6 0 0 0 .6-.6v-4.8a.6.6 0 0 0-.6-.6z"
        fill="rgb(var(--oui-color-base-foreground))"
        fillOpacity=".36"
      />
      <path
        d="M20.4 16.2a.6.6 0 0 1 .6-.6h2.4a.6.6 0 0 1 .6.6v.6a.6.6 0 0 1-.6.6H21a.6.6 0 0 1-.6-.6zm0 3a.6.6 0 0 1 .6-.6h9a.6.6 0 0 1 .6.6v.6a.6.6 0 0 1-.6.6h-9a.6.6 0 0 1-.6-.6zm-7.8 5.4a.6.6 0 0 1 .6-.6H30a.6.6 0 0 1 .6.6v.6a.6.6 0 0 1-.6.6H13.2a.6.6 0 0 1-.6-.6zm0 3.6a.6.6 0 0 1 .6-.6H30a.6.6 0 0 1 .6.6v.6a.6.6 0 0 1-.6.6H13.2a.6.6 0 0 1-.6-.6zm0 3.6a.6.6 0 0 1 .6-.6h9a.6.6 0 0 1 .6.6v.6a.6.6 0 0 1-.6.6h-9a.6.6 0 0 1-.6-.6z"
        fill="rgb(var(--oui-color-base-foreground))"
        fillOpacity=".36"
      />
      <path
        fillRule="evenodd"
        clipRule="evenodd"
        d="M15.739 6.6H10.2A2.4 2.4 0 0 0 7.8 9v30a2.4 2.4 0 0 0 2.4 2.4h13.84l1.014-1.8H10.2a.6.6 0 0 1-.6-.6V9a.6.6 0 0 1 .6-.6h5.262zm12 1.8H33a.6.6 0 0 1 .6.6v16.47a2.8 2.8 0 0 1 1.8-.205V9A2.4 2.4 0 0 0 33 6.6h-5.538z"
        fill="rgb(var(--oui-color-base-foreground))"
        fillOpacity=".36"
      />
    </svg>
  );
};

const ShareIcon = () => {
  return (
    <svg
      width="48"
      height="48"
      viewBox="0 0 48 48"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      className="xl:oui-w-[64px] xl:oui-h-[64px] 2xl:oui-w-[80px] 2xl:oui-h-[80px]"
    >
      <path
        d="M24 33a9 9 0 1 1-18 0 9 9 0 0 1 18 0"
        fill="rgb(var(--oui-color-primary-darken))"
      />
      <path
        fillRule="evenodd"
        clipRule="evenodd"
        d="M15 32.4a1.8 1.8 0 1 0 0-3.6 1.8 1.8 0 0 0 0 3.6m0 1.8a3.6 3.6 0 1 0 0-7.2 3.6 3.6 0 0 0 0 7.2m-6.578 4.942A4.8 4.8 0 0 1 13.2 34.8h3.6a4.8 4.8 0 0 1 4.778 4.342 9 9 0 0 1-1.778 1.473V39.6a3 3 0 0 0-3-3h-3.6a3 3 0 0 0-3 3v1.015a9 9 0 0 1-1.778-1.473"
        fill="rgb(var(--oui-color-base-foreground))"
        fillOpacity=".98"
      />
      <path
        fillRule="evenodd"
        clipRule="evenodd"
        d="M16.2 12a4.2 4.2 0 1 1-8.4 0 4.2 4.2 0 0 1 8.4 0m1.8 0c0 1.37-.459 2.632-1.231 3.642l2.308 2.308a7.76 7.76 0 0 1 4.023-1.7v-.764A3.602 3.602 0 0 1 24 8.4a3.6 3.6 0 0 1 .9 7.087v.764c1.48.17 2.833.754 3.942 1.633l2.326-2.326a6 6 0 1 1 1.273 1.273l-2.326 2.326a7.76 7.76 0 0 1 1.634 3.943h.764a3.602 3.602 0 0 1 7.087.9 3.6 3.6 0 0 1-7.087.9h-.764a7.76 7.76 0 0 1-1.699 4.023l2.308 2.308a6 6 0 1 1-1.25 1.295l-2.347-2.347a7.8 7.8 0 0 1-3.05 1.433 11 11 0 0 0-.377-1.76 6 6 0 1 0-7.185-7.185q-.855-.261-1.76-.379a7.8 7.8 0 0 1 1.432-3.049l-2.347-2.347A6 6 0 1 1 18 12m7.8 0a1.8 1.8 0 1 1-3.6 0 1.8 1.8 0 0 1 3.6 0M36 25.8a1.8 1.8 0 1 0 0-3.6 1.8 1.8 0 0 0 0 3.6m0-9.6a4.2 4.2 0 1 0 0-8.4 4.2 4.2 0 0 0 0 8.4M40.2 36a4.2 4.2 0 1 1-8.4 0 4.2 4.2 0 0 1 8.4 0"
        fill="rgb(var(--oui-color-base-foreground))"
        fillOpacity=".36"
      />
    </svg>
  );
};

const EarnIcon = () => {
  return (
    <svg
      width="48"
      height="48"
      viewBox="0 0 48 48"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      className="xl:oui-w-[64px] xl:oui-h-[64px] 2xl:oui-w-[80px] 2xl:oui-h-[80px]"
    >
      <path
        fillRule="evenodd"
        clipRule="evenodd"
        d="M24 24a2.4 2.4 0 1 0 0-4.8 2.4 2.4 0 0 0 0 4.8m0 1.8a4.2 4.2 0 1 0 0-8.4 4.2 4.2 0 0 0 0 8.4"
        fill="rgb(var(--oui-color-base-foreground))"
        fillOpacity=".36"
      />
      <path
        d="M45 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0"
        fill="rgb(var(--oui-color-primary-darken))"
      />
      <path
        fillRule="evenodd"
        clipRule="evenodd"
        d="M35.1 6.6a.9.9 0 1 1 1.8 0v.437A3 3 0 0 1 39 9.9h-1.8a1.2 1.2 0 1 0-1.2 1.2 3 3 0 0 1 .9 5.863v.437a.9.9 0 0 1-1.8 0v-.437A3 3 0 0 1 33 14.1h1.8a1.2 1.2 0 1 0 1.2-1.2 3 3 0 0 1-.9-5.863z"
        fill="rgb(var(--oui-color-base-foreground))"
        fillOpacity=".98"
      />
      <path
        fillRule="evenodd"
        clipRule="evenodd"
        d="M40.2 23.7a.9.9 0 0 1 1.8 0v.6q0 .045-.004.086c-.197 9.351-7.525 16.948-16.765 17.573l1.205 1.205a.9.9 0 1 1-1.272 1.272l-2.7-2.7a.9.9 0 0 1 0-1.272l2.7-2.7a.9.9 0 1 1 1.272 1.272l-1.11 1.11c8.325-.674 14.87-7.642 14.874-16.14zM21.564 4.836a.9.9 0 1 1 1.272-1.272l2.7 2.7a.9.9 0 0 1 0 1.272l-2.7 2.7a.9.9 0 1 1-1.272-1.272l1.11-1.11C14.347 8.528 7.8 15.5 7.8 24v.3a.9.9 0 1 1-1.8 0v-.6l.004-.086C6.201 14.263 13.53 6.666 22.77 6.042z"
        fill="rgb(var(--oui-color-base-foreground))"
        fillOpacity=".36"
      />
      <path
        fillRule="evenodd"
        clipRule="evenodd"
        d="M24 11.4q.615 0 1.213.058a11 11 0 0 0 .061 1.816Q24.647 13.2 24 13.2c-5.965 0-10.8 4.835-10.8 10.8 0 2.898 1.142 5.53 3 7.47a4.8 4.8 0 0 1 4.8-4.79h6a4.8 4.8 0 0 1 4.8 4.789c1.858-1.94 3-4.572 3-7.47q0-.647-.074-1.274a11 11 0 0 0 1.816.06q.057.6.058 1.214c0 6.959-5.641 12.6-12.6 12.6S11.4 30.959 11.4 24 17.041 11.4 24 11.4m6 21.581v-1.5a3 3 0 0 0-3-3h-6a3 3 0 0 0-3 3v1.5a10.75 10.75 0 0 0 6 1.819c2.22 0 4.284-.67 6-1.819"
        fill="rgb(var(--oui-color-base-foreground))"
        fillOpacity=".36"
      />
    </svg>
  );
};

const ArrowDownIcon = () => {
  return (
    <svg
      width="17"
      height="16"
      viewBox="0 0 17 16"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
    >
      <path
        d="M8.5048 4.00806C8.8728 4.00806 9.1708 4.30626 9.1708 4.67406C9.1708 4.99259 9.1708 8.71739 9.1708 9.71072L11.1688 7.73339L12.1055 8.67005L8.98346 11.8127C8.85346 11.9434 8.6788 12.0081 8.50413 12.0081C8.33013 12.0081 8.15612 11.9427 8.02612 11.8127L4.90413 8.67005L5.8408 7.73339L7.83881 9.71072C7.83881 8.71739 7.83881 4.99259 7.83881 4.67406C7.83881 4.30626 8.1368 4.00806 8.5048 4.00806Z"
        fill="rgb(var(--oui-color-primary))"
      />
    </svg>
  );
};
