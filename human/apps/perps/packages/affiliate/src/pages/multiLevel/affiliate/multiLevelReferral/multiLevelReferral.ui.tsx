import { FC } from "react";
import { SVGProps } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Flex, Text, Button, cn } from "@paxeer-network/ui";
import { TradingVolumeProgress } from "../../components/tradingVolumeProgress";
import { MultiLevelReferralReturns } from "./multiLevelReferral.script";

export const MultiLevelReferral: FC<MultiLevelReferralReturns> = (props) => {
  const { isMultiLevelReferralUnlocked } = props;
  const { t } = useTranslation();

  return (
    <Flex
      r={"2xl"}
      width={"100%"}
      height={"100%"}
      gap={4}
      p={5}
      direction={"column"}
      intensity={900}
      className={cn(
        "oui-affiliate-multiLevelReferral",
        "oui-border oui-border-line-6",
      )}
    >
      <Text size="lg" className="oui-w-full oui-text-start">
        {t("affiliate.referral")}
      </Text>
      <Flex
        width="100%"
        height="100%"
        direction="column"
        itemAlign="center"
        justify="center"
        gap={4}
      >
        <MultiLevelReferralIcon className="oui-text-primary-light" />

        <Text size="base" intensity={98}>
          {t("affiliate.newReferralProgram.title")}
        </Text>

        {isMultiLevelReferralUnlocked ? (
          <>
            <Text size="sm" intensity={54} className="oui-text-center">
              {t("affiliate.newReferralProgram.description")}
            </Text>
            <Button
              size="md"
              className="oui-px-16"
              onClick={props.createReferralCode}
            >
              {t("affiliate.referralCode.create")}
            </Button>
          </>
        ) : (
          <TradingVolumeProgress
            buttonProps={{
              className: "oui-px-16",
            }}
          />
        )}
      </Flex>
    </Flex>
  );
};

const MultiLevelReferralIcon = (props: SVGProps<SVGSVGElement>) => (
  <svg
    width={64}
    height={64}
    viewBox="0 0 64 64"
    fill="currentColor"
    xmlns="http://www.w3.org/2000/svg"
    {...props}
  >
    <rect width={64} height={64} rx={32} fill="rgb(var(--oui-color-base-8))" />
    <path
      fillRule="evenodd"
      clipRule="evenodd"
      d="M28.822 23.24a.86.86 0 0 1 .607-.24h5.142c.228 0 .446.086.607.24a.8.8 0 0 1 .25.579v3.28a.8.8 0 0 1-.25.578.88.88 0 0 1-.607.24h-1.719v3.259h7.714c.228 0 .446.086.606.24a.8.8 0 0 1 .251.578v4.094h1.72c.227 0 .445.086.606.24a.8.8 0 0 1 .251.578v3.275a.8.8 0 0 1-.251.58.88.88 0 0 1-.606.239H38a.88.88 0 0 1-.606-.24.8.8 0 0 1-.251-.579v-3.274a.8.8 0 0 1 .25-.58.88.88 0 0 1 .607-.24h1.71v-3.274h-6.858v3.275h1.72c.227 0 .445.086.605.24a.8.8 0 0 1 .252.578v3.275a.8.8 0 0 1-.252.58.88.88 0 0 1-.606.239H29.43a.88.88 0 0 1-.606-.24.8.8 0 0 1-.252-.579v-3.274a.8.8 0 0 1 .252-.58.88.88 0 0 1 .606-.24h1.709v-3.274H24.28v3.275H26c.227 0 .445.086.606.24a.8.8 0 0 1 .251.578v3.275a.8.8 0 0 1-.25.58A.88.88 0 0 1 26 41h-5.143a.88.88 0 0 1-.606-.24.8.8 0 0 1-.251-.579v-3.274a.8.8 0 0 1 .251-.58.88.88 0 0 1 .606-.24h1.71v-4.093a.8.8 0 0 1 .25-.579.88.88 0 0 1 .606-.24h7.715v-3.258h-1.71a.88.88 0 0 1-.605-.24.8.8 0 0 1-.252-.579V23.82q0-.163.065-.313a.8.8 0 0 1 .186-.268"
    />
  </svg>
);
