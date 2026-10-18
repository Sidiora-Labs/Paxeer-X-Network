import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { AccountStatusEnum } from "@paxeer-network/types";
import {
  Box,
  Divider,
  Flex,
  ListView,
  Text,
  DataTable,
  Column,
  useScreen,
} from "@paxeer-network/ui";
import { AuthGuardEmpty } from "@paxeer-network/ui-connector";
import { commifyOptional } from "@paxeer-network/utils";
import { EsPaxeerNetworkIcon } from "../components/esPaxeerNetworkIcon";
import { PaxeerNetworkIcon } from "../components/paxeerNetworkIcon";
import { RewardsTooltip } from "../curEpoch/rewardsTooltip";
import { ListType, RewardsHistoryReturns } from "./rewardsHistory.script";

export const RewardHistory: FC<RewardsHistoryReturns> = (props) => {
  const { t } = useTranslation();

  return (
    <Flex
      id="oui-tradingRewards-home-rewardHistory"
      py={4}
      px={3}
      direction={"column"}
      gap={2}
      itemAlign={"start"}
      r="2xl"
      className="oui-bg-base-9 oui-w-full oui-font-semibold oui-tabular-nums"
    >
      <Text className="oui-text-lg oui-px-3">
        {t("tradingRewards.rewardHistory")}
      </Text>
      <div className="oui-border-t-2 oui-border-line-4 oui-w-full">
        <List {...props} />
      </div>
    </Flex>
  );
};

const List: FC<RewardsHistoryReturns> = (props) => {
  const { isMobile } = useScreen();

  return isMobile ? (
    <ListView
      dataSource={props.originalData}
      renderItem={(item, index) => {
        return <MobileCell data={item} />;
      }}
      className="oui-mt-3 oui-max-h-[356px]"
    />
  ) : (
    <Box px={3}>
      <DesktopList {...props} />
    </Box>
  );
};

const MobileCell: FC<{
  data: ListType;
}> = (props) => {
  const { data } = props;
  const { t } = useTranslation();

  console.log(data.rewardsTooltip);
  const isOrder =
    `${data?.info?.epoch_token || data.epoch_token}`.toLowerCase() === "order";
  const r_warret = commifyOptional(data.info?.r_wallet, { fix: 2 });

  return (
    <Flex
      key={data.epoch_id}
      direction={"column"}
      px={4}
      pt={0}
      gap={3}
      className="oui-text-base-contrast-80"
    >
      <Flex direction={"row"} width={"100%"}>
        <Flex
          direction={"column"}
          className="oui-gap-[6px] oui-flex-1"
          itemAlign={"start"}
        >
          <Text className="oui-text-base-contrast-36 oui-text-2xs">
            {t("tradingRewards.epoch")}
          </Text>
          <Text className="oui-text-sm">{`Epoch ${data.epoch_id}`}</Text>
        </Flex>
        <Flex
          direction={"column"}
          className="oui-gap-[6px] oui-flex-1"
          itemAlign={"start"}
        >
          <Text className="oui-text-base-contrast-36 oui-text-2xs">
            {t("tradingRewards.epochRewards")}{" "}
          </Text>
          <Flex gap={1}>
            {isOrder ? <PaxeerNetworkIcon /> : <EsPaxeerNetworkIcon />}
            <Text className="oui-text-sm">
              {commifyOptional(data.max_reward_amount, { fix: 2 })}
            </Text>
          </Flex>
        </Flex>
        <Flex
          direction={"column"}
          className="oui-gap-[6px] oui-flex-1"
          itemAlign={"end"}
        >
          <Text className="oui-text-base-contrast-36 oui-text-2xs">
            {t("tradingRewards.rewardsEarned")}{" "}
          </Text>
          <Flex gap={1}>
            {r_warret !== "--" &&
              (isOrder ? <PaxeerNetworkIcon /> : <EsPaxeerNetworkIcon />)}
            {!!data.rewardsTooltip ? (
              <RewardsTooltip
                rewardsTooltip={data.rewardsTooltip}
                children={
                  <Text className="oui-text-sm oui-underline oui-decoration-dashed oui-cursor-pointer oui-underline-offset-4 oui-decoration-line-16">
                    {r_warret}
                  </Text>
                }
                align="center"
                className="oui-bg-base-5"
                arrowClassName="oui-fill-base-5"
              />
            ) : (
              <Text className="oui-text-sm">{r_warret}</Text>
            )}
          </Flex>
        </Flex>
      </Flex>
      <Flex direction={"row"} width={"100%"}>
        <Flex
          direction={"column"}
          className="oui-gap-[6px] oui-flex-1"
          itemAlign={"start"}
        >
          <Text className="oui-text-base-contrast-36 oui-text-2xs">
            {t("tradingRewards.rewardHistory.column.startDate")}
          </Text>
          <Flex direction={"row"}>
            <Text className="oui-text-sm">
              {formatTimestamp(data.start_time).firstPart}&nbsp;
            </Text>
            <Text className="oui-text-2xs oui-text-base-contrast-36">
              {formatTimestamp(data.start_time).secondPart}
            </Text>
          </Flex>
        </Flex>
        <Flex
          direction={"column"}
          className="oui-gap-[6px] oui-flex-1"
          itemAlign={"end"}
        >
          <Text className="oui-text-base-contrast-36 oui-text-2xs">
            {t("tradingRewards.rewardHistory.column.endDate")}
          </Text>
          <Flex direction={"row"}>
            <Text className="oui-text-sm">
              {formatTimestamp(data.end_time).firstPart}&nbsp;
            </Text>
            <Text className="oui-text-2xs oui-text-base-contrast-36">
              {formatTimestamp(data.end_time).secondPart}
            </Text>
          </Flex>
        </Flex>
      </Flex>
      <Divider className="oui-w-full" />
    </Flex>
  );
};

const DesktopList: FC<RewardsHistoryReturns> = (props) => {
  const { data } = props;
  const { t } = useTranslation();

  const columns: Column<ListType>[] = [
    {
      title: t("tradingRewards.epoch"),
      dataIndex: "epoch_id",
      className: "oui-w-1/4 oui-px-0",
      render: (value) => {
        return <Text>{`${t("tradingRewards.epoch")} ${value}`}</Text>;
      },
    },
    {
      title: t("tradingRewards.rewardHistory.column.start&EndDate"),
      dataIndex: "time",
      className: "oui-w-1/4 oui-px-0",
      render: (value, record) => {
        return (
          <Flex
            direction={"column"}
            className="oui-gap-[2px]"
            justify={"start"}
            itemAlign={"start"}
          >
            <Flex direction={"row"} gap={1}>
              <Text>{formatTimestamp(record.start_time).firstPart}</Text>
              <Text className="oui-text-base-contrast-54">
                {formatTimestamp(record.start_time).secondPart}
              </Text>
            </Flex>
            <Flex direction={"row"} gap={1}>
              <Text>{formatTimestamp(record.end_time).firstPart}</Text>
              <Text className="oui-text-base-contrast-54">
                {formatTimestamp(record.end_time).secondPart}
              </Text>
            </Flex>
          </Flex>
        );
      },
    },
    {
      title: t("tradingRewards.epochRewards"),
      dataIndex: "max_reward_amount",
      className: "oui-w-1/4 oui-px-0",
      render: (value, record) => {
        const isOrder =
          `${record?.info?.epoch_token || record.epoch_token}`.toLowerCase() ===
          "order";
        return (
          <Flex direction={"row"} gap={1}>
            {isOrder ? <PaxeerNetworkIcon /> : <EsPaxeerNetworkIcon />}
            <Text>{commifyOptional(value)}</Text>
          </Flex>
        );
      },
    },
    {
      title: t("tradingRewards.rewardsEarned"),
      dataIndex: "earned",
      className: "oui-w-1/4 oui-px-0",
      render: (value, record) => {
        const isOrder =
          `${record?.info?.epoch_token || record.epoch_token}`.toLowerCase() ===
          "order";

        let children = (
          <Text
            className={
              record.rewardsTooltip
                ? "oui-underline oui-decoration-dashed oui-cursor-pointer oui-underline-offset-4 oui-decoration-line-16"
                : ""
            }
          >
            {commifyOptional(record.info?.r_wallet, { fix: 2 })}
          </Text>
        );

        if (record.rewardsTooltip) {
          children = (
            <RewardsTooltip
              rewardsTooltip={record.rewardsTooltip}
              children={children}
              align="center"
              className="oui-bg-base-5"
              arrowClassName="oui-fill-base-5"
            />
          );
        }
        return (
          <Flex direction={"row"} gap={1}>
            {isOrder ? <PaxeerNetworkIcon /> : <EsPaxeerNetworkIcon />}
            {children}
          </Flex>
        );
      },
    },
  ];

  return (
    <DataTable
      bordered
      columns={columns}
      loading={props.isLoading}
      dataSource={data}
      emptyView={<AuthGuardEmpty status={AccountStatusEnum.SignedIn} />}
      onRow={(record) => {
        return {
          className: "oui-h-[59px]",
        };
      }}
      pagination={props.pagination}
    />
  );
};

function formatTimestamp(timestamp?: number) {
  if (typeof timestamp === "undefined")
    return {
      firstPart: "-",
      secondPart: "",
    };
  const date = new Date(timestamp);

  const monthNames = [
    "Jan",
    "Feb",
    "Mar",
    "Apr",
    "May",
    "Jun",
    "Jul",
    "Aug",
    "Sep",
    "Oct",
    "Nov",
    "Dec",
  ];
  const month = monthNames[date.getUTCMonth()];
  const day = date.getUTCDate().toString().padStart(2, "0");
  const year = date.getUTCFullYear();
  const hours = date.getUTCHours();
  const minutes = date.getUTCMinutes();
  const amPm = hours >= 12 ? "PM" : "AM";
  const formattedHours = (hours % 12).toString().padStart(2, "0");
  const formattedMinutes = minutes.toString().padStart(2, "0");

  // return `${month} ${day}, ${year}_${formattedHours}:${formattedMinutes} ${amPm}`;
  return {
    firstPart: `${month} ${day}, ${year}`,
    secondPart: `${formattedHours}:${formattedMinutes} ${amPm}`,
  };
}
