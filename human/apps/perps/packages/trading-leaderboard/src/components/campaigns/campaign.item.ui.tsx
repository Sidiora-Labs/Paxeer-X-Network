import { FC } from "react";
import { cn, Text } from "@paxeer-network/ui";
import { CampaignConfig, CampaignTagEnum } from "./type";
import { formatParticipantsCount, getTotalPrizePool } from "./utils";

interface CampaignItemUIProps {
  className?: string;
  active?: boolean;
  tag: CampaignTagEnum;
  onCampaignChange: (campaignId: string) => void;
  campaign: CampaignConfig;
  backgroundSrc?: string;
  classNames?: {
    container?: string;
    content?: string;
    tag?: {
      container?: string;
      text?: string;
    };
    title?: string;
  };
}

export const CampaignItemUI: FC<CampaignItemUIProps> = ({
  active,
  tag,
  onCampaignChange,
  campaign,
  backgroundSrc,
  classNames,
}) => {
  const totalPrizePool = getTotalPrizePool(campaign);
  const formattedPrice = formatParticipantsCount(
    totalPrizePool?.amount || 0,
    0,
  );

  return (
    <div
      className={cn([
        "oui-w-full",
        "oui-relative oui-h-[78px] oui-cursor-pointer oui-overflow-hidden oui-rounded-lg oui-bg-base-contrast-4",
        "oui-group oui-border oui-border-solid oui-backdrop-blur-[200px]",
        active
          ? "oui-border-[rgba(var(--oui-gradient-brand-start))]"
          : "oui-border-transparent",
        "hover:oui-border-base-contrast",
        classNames?.container,
      ])}
      onClick={() => onCampaignChange(campaign.campaign_id.toString())}
    >
      <CampaignTag tag={tag} active={active} classNames={classNames?.tag} />
      <PriceTag price={formattedPrice} active={active} />
      <div
        className={cn([
          "oui-size-full",
          "oui-absolute oui-start-0 oui-top-0",
          "oui-bg-cover oui-bg-center oui-bg-no-repeat oui-bg-blend-luminosity",
          active ? "oui-bg-transparent" : "oui-bg-[lightgray] oui-opacity-40",
          "group-hover:oui-bg-transparent group-hover:oui-opacity-100",
        ])}
        style={{
          backgroundImage: `url(${campaign?.image || backgroundSrc})`,
        }}
      />
      <div
        className={cn([
          "oui-flex oui-size-full oui-flex-col oui-items-center oui-justify-center oui-gap-1",
          "oui-absolute oui-start-0 oui-top-0 oui-z-10",
          classNames?.content,
        ])}
      >
        <Text
          weight="semibold"
          className={cn([
            active ? "oui-text-base-contrast" : "oui-text-base-contrast-54",
            "group-hover:oui-text-base-contrast",
            "oui-trading-leaderboard-title",
            "oui-w-3/5 oui-text-center oui-text-sm",
            classNames?.title,
          ])}
        >
          {campaign.title}
        </Text>
        {campaign.referral_codes && (
          <Text
            className={cn([
              "oui-text-base-contrast-54",
              "oui-text-center oui-text-[10px]",
            ])}
          >
            {Array.isArray(campaign.referral_codes)
              ? campaign.referral_codes.join(",")
              : campaign.referral_codes}
          </Text>
        )}
      </div>
    </div>
  );
};

const CampaignTag: FC<{
  tag: CampaignTagEnum;
  active?: boolean;
  classNames?: {
    container?: string;
    text?: string;
  };
}> = ({ tag, active, classNames }) => {
  const tagText = tag.slice(0, 1).toUpperCase() + tag.slice(1);
  return (
    <div
      className={cn([
        "oui-w-fit oui-rounded-br-lg",
        "oui-absolute oui-start-0 oui-top-0 oui-z-10 oui-flex oui-items-center",
        active && tag !== CampaignTagEnum.COMING
          ? "oui-bg-[rgba(var(--oui-gradient-brand-start))]"
          : "oui-bg-base-4",
        active && tag !== CampaignTagEnum.ENDED && "group-hover:oui-bg-base-4",
        tag === CampaignTagEnum.ENDED &&
          "oui-bg-base-6 group-hover:oui-bg-base-6",
        classNames?.container,
      ])}
    >
      {tag === CampaignTagEnum.ENDED ? (
        <Text
          size="2xs"
          weight="semibold"
          className={cn(["oui-text-base-contrast-54", classNames?.text])}
        >
          {tagText}
        </Text>
      ) : (
        <Text.gradient
          size="2xs"
          weight="semibold"
          color="brand"
          className={cn([
            active &&
              tag !== CampaignTagEnum.COMING &&
              "oui-text-base-contrast-80 group-hover:oui-text-transparent",
            classNames?.text,
          ])}
        >
          {tagText}
        </Text.gradient>
      )}
    </div>
  );
};

const PriceTag = ({ price, active }: { price: string; active?: boolean }) => {
  return (
    <div
      className={cn([
        "oui-flex oui-h-[18px] oui-items-center oui-gap-0.5 oui-rounded-lg oui-bg-warning/70 oui-px-1 oui-py-0.5 oui-backdrop-blur-[2px]",
        "oui-absolute oui-end-1 oui-top-1 oui-z-10 oui-flex oui-items-center",
        active ? "oui-opacity-100" : "oui-opacity-60",
      ])}
    >
      <LeaderboardTotalPrice />
      <Text
        size="2xs"
        className={cn(
          "oui-trading-leaderboard-title oui-font-medium oui-text-base-contrast",
        )}
      >
        {price}
      </Text>
    </div>
  );
};

const LeaderboardTotalPrice = () => {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width="10"
      height="10"
      viewBox="0 0 10 10"
      fill="currentColor"
      className="oui-fill-warning"
    >
      <path d="M3.2 9.5C2.44167 9.5 1.80208 9.23958 1.28125 8.71875C0.760417 8.19792 0.5 7.55833 0.5 6.8C0.5 6.48333 0.554167 6.175 0.6625 5.875C0.770833 5.575 0.925 5.30417 1.125 5.0625L2.9 2.925L1.6875 0.5H8.3125L7.1 2.925L8.875 5.0625C9.075 5.30417 9.22917 5.575 9.3375 5.875C9.44583 6.175 9.5 6.48333 9.5 6.8C9.5 7.55833 9.2375 8.19792 8.7125 8.71875C8.1875 9.23958 7.55 9.5 6.8 9.5H3.2ZM5 7C4.725 7 4.48958 6.90208 4.29375 6.70625C4.09792 6.51042 4 6.275 4 6C4 5.725 4.09792 5.48958 4.29375 5.29375C4.48958 5.09792 4.725 5 5 5C5.275 5 5.51042 5.09792 5.70625 5.29375C5.90208 5.48958 6 5.725 6 6C6 6.275 5.90208 6.51042 5.70625 6.70625C5.51042 6.90208 5.275 7 5 7ZM3.8125 2.5H6.1875L6.6875 1.5H3.3125L3.8125 2.5ZM3.2 8.5H6.8C7.275 8.5 7.67708 8.33542 8.00625 8.00625C8.33542 7.67708 8.5 7.275 8.5 6.8C8.5 6.6 8.46458 6.40625 8.39375 6.21875C8.32292 6.03125 8.225 5.8625 8.1 5.7125L6.2625 3.5H3.75L1.9 5.7C1.775 5.85 1.67708 6.02083 1.60625 6.2125C1.53542 6.40417 1.5 6.6 1.5 6.8C1.5 7.275 1.66458 7.67708 1.99375 8.00625C2.32292 8.33542 2.725 8.5 3.2 8.5Z" />
    </svg>
  );
};
