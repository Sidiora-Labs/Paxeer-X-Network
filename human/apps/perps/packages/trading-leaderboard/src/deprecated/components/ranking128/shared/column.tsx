import { ReactNode, useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Text, Column, Box, useScreen, cn, toast } from "@paxeer-network/ui";
import { getCurrentAddressRowKey } from "./util";

export type RankingColumnFields =
  | "rank"
  | "address"
  | "volume"
  | "pnl"
  | "rewards";

export const useRankingColumns = (
  fields?: RankingColumnFields[],
  address?: string,
  enableSort?: boolean,
  type?: "general" | "campaign",
) => {
  const { t } = useTranslation();
  const { isMobile } = useScreen();

  return useMemo(() => {
    const columns = [
      {
        title: t("tradingLeaderboard.rank"),
        dataIndex: "rank",
        width: 50,
        render: (value: number, record: any) => {
          const isYou = record.key === getCurrentAddressRowKey(address!);

          let rankIcon: ReactNode;
          let badgeImg: ReactNode = null;

          if (!isYou) {
            if (value === 1) {
              rankIcon = <FirstRankIcon />;
              badgeImg =
                "/assets/static/sdk/leaderboard/first_badge.png";
            } else if (value === 2) {
              rankIcon = <SecondRankIcon />;
              badgeImg =
                "/assets/static/sdk/leaderboard/second_badge.png";
            } else if (value === 3) {
              rankIcon = <ThirdRankIcon />;
              badgeImg =
                "/assets/static/sdk/leaderboard/third_badge.png";
            }
          }

          return (
            <>
              {badgeImg && (
                <img
                  src={badgeImg as string}
                  alt={`${value}th badge`}
                  className={cn(
                    "oui-z-0 oui-h-[38px] oui-opacity-30 md:oui-h-[46px]",
                    "oui-absolute oui-start-0 oui-top-0",
                    "oui-mix-blend-luminosity",
                    // force create a separate layer in order to fix mix-blend-luminosity not working on ios
                    "oui-transform-gpu",
                  )}
                />
              )}
              <div className="oui-relative">
                {rankIcon || (
                  <Box width={20} pl={2} className="oui-text-center">
                    {value}
                  </Box>
                )}
              </div>
            </>
          );
        },
      },
      {
        title: t("common.address"),
        dataIndex: "address",
        render: (value: string, record: any) => {
          const isYou = record.key === getCurrentAddressRowKey(address!);
          if (isMobile && isYou) {
            return <Text>You</Text>;
          }

          let linearGradientText;

          if (!isYou) {
            if (record.rank === 1) {
              linearGradientText =
                "linear-gradient(169deg, #FBE67B 2.09%, #FCFBE7 15.8%, #F7D14E 40.73%, #D4A041 58.8%)";
            } else if (record.rank === 2) {
              linearGradientText =
                "linear-gradient(201.05deg, #D9D9D9 38.79%, #F7F6F4 53.85%, #D9D9D9 71.71%, #7F7F7F 91.87%), rgba(255, 255, 255, 0.8)";
            } else if (record.rank === 3) {
              linearGradientText =
                "linear-gradient(149.05deg, #B6947E 15.63%, #F8DAC8 31.37%, #B6947E 44.29%, #F8DCCB 56.6%), rgba(255, 255, 255, 0.8)";
            }
          }

          return (
            <>
              <a
                className="oui-flex oui-items-start oui-gap-1"
                href={`https://paxeer-network-dashboard.paxeer-network.network/address/${value}?broker_id=${record.broker_id}`}
                target="_blank"
                rel="noreferrer"
              >
                <Text.formatted
                  rule="address"
                  key={record.rank}
                  copyable
                  onCopy={(e) => {
                    e.preventDefault();
                    e.stopPropagation();
                    navigator.clipboard.writeText(value);
                    toast.success(t("common.copy.copied"));
                  }}
                  // style={
                  //   linearGradientText
                  //     ? {
                  //         background: linearGradientText,
                  //         WebkitBackgroundClip: "text",
                  //         WebkitTextFillColor: "transparent",
                  //         backgroundClip: "text",
                  //       }
                  //     : {}
                  // }
                  className="oui-cursor-pointer oui-underline oui-decoration-line-16 oui-decoration-dashed oui-underline-offset-4"
                >
                  {value}
                </Text.formatted>
                {isYou && <Text> (You)</Text>}
              </a>
            </>
          );
        },
        width: 90,
      },
      {
        title: t("tradingLeaderboard.tradingVolume"),
        dataIndex: "volume",
        onSort: enableSort,
        align: isMobile ? "right" : "left",
        render: (value: string) => {
          if (!value) {
            return "-";
          }
          return (
            <Text.numeral prefix="$" rule="price" dp={2}>
              {value}
            </Text.numeral>
          );
        },
        width: 105,
      },
      {
        title: t("common.pnl"),
        dataIndex: "pnl",
        onSort: enableSort,
        align: isMobile ? "right" : "left",
        render: (value: string) => {
          if (!value) {
            return "-";
          }
          return (
            <Text.numeral prefix="$" rule="price" dp={2} coloring>
              {value}
            </Text.numeral>
          );
        },
        width: 90,
      },
      {
        title: t("tradingLeaderboard.estimatedRewards"),
        dataIndex: "rewards",
        align: isMobile ? "right" : "left",
        render: (value: { amount: number; currency: string }) => {
          if (!value) {
            return "-";
          }
          return (
            <Text.numeral
              suffix={` ${value?.currency || ""}`}
              rule="price"
              dp={0}
            >
              {value?.amount}
            </Text.numeral>
          );
        },
        width: 90,
      },
    ] as Column[];

    return columns.filter((column) =>
      fields?.includes(column.dataIndex as RankingColumnFields),
    );
  }, [t, isMobile, address, fields, enableSort, type]);
};

const FirstRankIcon = () => {
  return (
    <svg
      width="25"
      height="25"
      viewBox="0 0 25 25"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
    >
      <path
        d="M3.88281 2.5L7.78711 10.3105C6.38111 11.5855 5.5 13.427 5.5 15.5C5.5 19.4 8.6 22.5 12.5 22.5C16.4 22.5 19.5 19.4 19.5 15.5C19.5 13.427 18.6189 11.5855 17.2129 10.3105L21.1172 2.5H15.5L12.5 8.5L9.5 2.5H3.88281ZM12.5 10.5C15.3 10.5 17.5 12.7 17.5 15.5C17.5 18.3 15.3 20.5 12.5 20.5C9.7 20.5 7.5 18.3 7.5 15.5C7.5 12.7 9.7 10.5 12.5 10.5ZM12.5 12.5C12.4 12.8 11.9 13.6992 11 13.6992V14.6992H12.0996V18.5H13.4004H13.5V12.5H12.5Z"
        fill="url(#paint0_linear_21940_39199)"
      />
      <defs>
        <linearGradient
          id="paint0_linear_21940_39199"
          x1="6.18073"
          y1="6"
          x2="20.1338"
          y2="18.1659"
          gradientUnits="userSpaceOnUse"
        >
          <stop stopColor="#8C421D" />
          <stop offset="0.325272" stopColor="#FBE67B" />
          <stop offset="0.535488" stopColor="#FCFBE7" />
          <stop offset="0.769917" stopColor="#F7D14E" />
          <stop offset="1" stopColor="#D4A041" />
        </linearGradient>
      </defs>
    </svg>
  );
};

const SecondRankIcon = () => {
  return (
    <svg
      width="25"
      height="25"
      viewBox="0 0 25 25"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
    >
      <path
        d="M3.88281 2.5L7.78711 10.3105C6.38111 11.5855 5.5 13.427 5.5 15.5C5.5 19.4 8.6 22.5 12.5 22.5C16.4 22.5 19.5 19.4 19.5 15.5C19.5 13.427 18.6189 11.5855 17.2129 10.3105L21.1172 2.5H15.5L12.5 8.5L9.5 2.5H3.88281ZM12.5 10.5C15.3 10.5 17.5 12.7 17.5 15.5C17.5 18.3 15.3 20.5 12.5 20.5C9.7 20.5 7.5 18.3 7.5 15.5C7.5 12.7 9.7 10.5 12.5 10.5ZM12.5469 12.5C10.7729 12.5 10.481 13.901 10.5 14.5H11.6738C11.6738 14.357 11.809 13.5 12.5 13.5C13.163 13.5 13.291 14.0232 13.291 14.2852C13.291 15.0512 12.245 15.7623 10.5 17.6973V18.5L14.4883 18.4766L14.4863 17.5332H12.2285C13.8425 15.8792 14.5 15.1309 14.5 14.1719C14.5 13.4869 14.1149 12.5 12.5469 12.5Z"
        fill="url(#paint0_linear_21940_39214)"
      />
      <defs>
        <linearGradient
          id="paint0_linear_21940_39214"
          x1="6.18073"
          y1="6"
          x2="20.1338"
          y2="18.1659"
          gradientUnits="userSpaceOnUse"
        >
          <stop stopColor="#7F7F7F" />
          <stop offset="0.325272" stopColor="#D9D9D9" />
          <stop offset="0.535488" stopColor="#F7F6F4" />
          <stop offset="0.769917" stopColor="#D9D9D9" />
          <stop offset="1" stopColor="#7F7F7F" />
        </linearGradient>
      </defs>
    </svg>
  );
};

const ThirdRankIcon = () => {
  return (
    <svg
      width="29"
      height="25"
      viewBox="0 0 29 25"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
    >
      <path
        d="M5.88281 2.5L9.78711 10.3105C8.38111 11.5855 7.5 13.427 7.5 15.5C7.5 19.4 10.6 22.5 14.5 22.5C18.4 22.5 21.5 19.4 21.5 15.5C21.5 13.427 20.6189 11.5855 19.2129 10.3105L23.1172 2.5H17.5L14.5 8.5L11.5 2.5H5.88281ZM14.5 10.5C17.3 10.5 19.5 12.7 19.5 15.5C19.5 18.3 17.3 20.5 14.5 20.5C11.7 20.5 9.5 18.3 9.5 15.5C9.5 12.7 11.7 10.5 14.5 10.5ZM14.4688 12.5C13.6927 12.5 12.5898 12.9348 12.5898 14.0918H13.7266C13.7266 13.9118 13.8461 13.4336 14.4941 13.4336C14.6251 13.4336 15.2715 13.4767 15.2715 14.1797C15.2715 14.8967 14.7109 14.9844 14.4219 14.9844H13.8145V15.8906H14.4219C14.5659 15.8906 15.3613 15.8537 15.3613 16.7637C15.3613 16.8837 15.3111 17.5625 14.4961 17.5625C13.8081 17.5625 13.6233 17.0284 13.6562 16.8164H12.5195C12.4615 17.4334 12.9757 18.4961 14.4688 18.4961C15.3018 18.4961 16.5 18.0942 16.5 16.7812C16.5 15.8643 15.8621 15.536 15.5391 15.418C15.6781 15.354 16.4082 14.9771 16.4082 14.1641C16.4082 13.7131 16.2127 12.5 14.4688 12.5Z"
        fill="url(#paint0_linear_21940_39224)"
      />
      <defs>
        <linearGradient
          id="paint0_linear_21940_39224"
          x1="8.61159"
          y1="5.33333"
          x2="22.7368"
          y2="20.4383"
          gradientUnits="userSpaceOnUse"
        >
          <stop stopColor="#B6947E" />
          <stop offset="0.2" stopColor="#8F6959" />
          <stop offset="0.475" stopColor="#F8DAC8" />
          <stop offset="0.67" stopColor="#AC836E" />
          <stop offset="0.83" stopColor="#B6947E" />
          <stop offset="1" stopColor="#F8DCCB" />
        </linearGradient>
      </defs>
    </svg>
  );
};
