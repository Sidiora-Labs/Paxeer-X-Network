import { FC, MouseEvent, ReactNode, useCallback, useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  Column,
  Text,
  Flex,
  ListView,
  Divider,
  useScreen,
  Tooltip,
  modal,
} from "@paxeer-network/ui";
import { AuthGuardDataTable } from "@paxeer-network/ui-connector";
import { Decimal } from "@paxeer-network/utils";
import { RefereeDataType } from "../../../../../hooks/useMultiLevelReferees";
import { ReferralCodeFormType } from "../../../../../types";
import { formatYMDTime } from "../../../../../utils/utils";
import {
  AddressCell,
  BreakdownCell,
  TooltipCell,
  MobileCell,
  MobileCard,
} from "../base/cells";
import { RefereesTableScriptReturns } from "./refereesTable.script";

type RefereesTableUIProps = RefereesTableScriptReturns;

const getRebateRateText = (rate: number) => {
  return (rate * 100).toFixed(0) + "%";
};

const getRefereeRebateRateText = (
  refereeRate: number,
  baseRebateRate: number,
) => {
  return new Decimal(refereeRate).add(baseRebateRate).mul(100).toFixed(0) + "%";
};

const getRefereeType = (
  bindType: string,
  t: ReturnType<typeof useTranslation>["t"],
) => {
  if (bindType === "legacy") {
    return {
      text: t("affiliate.singleLevelLegacy"),
      tooltip: t("affiliate.singleLevelLegacy.tooltip"),
    };
  }

  return {
    text: t("affiliate.multiLevel"),
    tooltip: t("affiliate.multiLevel.tooltip"),
  };
};

const useMobileDescriptionModal = (content: ReactNode, title: ReactNode) => {
  const { isMobile } = useScreen();

  return useCallback(
    (e: MouseEvent) => {
      if (!isMobile) return;
      e.preventDefault();
      e.stopPropagation();

      modal.dialog({
        title,
        closable: true,
        size: "sm",
        content: (
          <div className="oui-text-sm oui-leading-5 oui-text-base-contrast">
            {content}
          </div>
        ),
      });
    },
    [isMobile, content, title],
  );
};

const DescriptionCell: FC<{
  description?: string | null;
}> = ({ description }) => {
  const { t } = useTranslation();
  const hasDescription = !!description && description.length > 0;
  const onClick = useMobileDescriptionModal(
    description,
    t("affiliate.refereeNote"),
  );
  const { isMobile } = useScreen();

  if (!hasDescription) {
    return (
      <Text size="2xs" intensity={54} className="oui-leading-[18px]">
        --
      </Text>
    );
  }

  return (
    <Tooltip content={description} open={isMobile ? false : undefined}>
      <Text
        size="2xs"
        intensity={54}
        className="oui-block oui-max-w-full oui-cursor-pointer oui-truncate oui-underline oui-decoration-dashed oui-underline-offset-4 oui-decoration-base-contrast-36 oui-leading-[18px]"
        onClick={onClick}
      >
        {description}
      </Text>
    </Tooltip>
  );
};

const AddressDescriptionCell: FC<{
  item: RefereeDataType;
}> = ({ item }) => {
  const { t } = useTranslation();
  return (
    <Flex direction="column" itemAlign="start" gap={1} className="oui-min-w-0">
      <AddressCell address={item.address} title={t("common.address")} />
      <DescriptionCell description={item.description} />
    </Flex>
  );
};

const MobileRefereeItem: FC<{
  item: RefereeDataType;
  onEditReferee: RefereesTableScriptReturns["onEditReferee"];
  onEditDescription: RefereesTableScriptReturns["onEditDescription"];
  showActionColumn: boolean;
  baseRebateRate: number;
}> = ({
  item,
  onEditReferee,
  onEditDescription,
  showActionColumn,
  baseRebateRate,
}) => {
  const { t } = useTranslation();
  const typeInfo = getRefereeType(item.bind_type, t);
  return (
    <MobileCard>
      <MobileCell label={t("common.address")}>
        <AddressDescriptionCell item={item} />
      </MobileCell>
      <MobileCell label={t("common.type")}>
        <TooltipCell
          text={typeInfo.text}
          tooltip={typeInfo.tooltip}
          title={t("common.type")}
        />
      </MobileCell>
      <MobileCell label={t("affiliate.boundAt")} align="end">
        <Text size="sm">{formatYMDTime(item.code_binding_time)}</Text>
      </MobileCell>
      <MobileCell label={t("affiliate.referralCodes")}>
        <Text size="sm">{item.bind_code}</Text>
      </MobileCell>

      <MobileCell
        label={t("affiliate.referralCodes.column.you&DirectReferees")}
      >
        <Text size="sm">
          {getRebateRateText(item.referral_rebate_rate) + "/"}
          <span className="oui-text-base-contrast-54">
            {getRefereeRebateRateText(
              item.referee_rebate_rate,
              baseRebateRate,
            )}{" "}
          </span>
          {!item.is_default_rate && (
            <span className="oui-text-primary-light">
              {`(${t("affiliate.customized")})`}
            </span>
          )}
        </Text>
      </MobileCell>
      <MobileCell label={t("affiliate.networkSize")} align="end">
        <BreakdownCell
          total={item.network_size}
          direct={item.direct_invites}
          indirect={item.indirect_invites}
          title={t("affiliate.networkSize")}
        />
      </MobileCell>
      <MobileCell label={t("common.volume")}>
        <BreakdownCell
          total={item.volume}
          direct={item.direct_volume}
          indirect={item.indirect_volume}
          prefix="$"
          fix={2}
          title={t("common.volume")}
        />
      </MobileCell>

      <MobileCell label={t("affiliate.commission")}>
        <BreakdownCell
          total={item.commission}
          direct={item.direct_rebate}
          indirect={item.indirect_rebate}
          directBonus={item.direct_bonus_rebate}
          prefix="$"
          fix={6}
          title={t("affiliate.commission")}
        />
      </MobileCell>
      <MobileCell
        label={t("common.action")}
        align="end"
        className="oui-col-start-3"
      >
        <Flex
          gap={2}
          itemAlign="center"
          className="oui-flex-wrap oui-justify-end oui-gap-y-1"
        >
          {showActionColumn && item.bind_type !== "legacy" && (
            <Text
              className="oui-refereesTable-edit-btn oui-shrink-0 oui-cursor-pointer oui-text-primary-light"
              onClick={() => onEditReferee(ReferralCodeFormType.Edit, item)}
            >
              {t("common.edit")}
            </Text>
          )}
          <Text
            className="oui-refereesTable-note-btn oui-shrink-0 oui-cursor-pointer oui-text-primary-light"
            onClick={() => onEditDescription(item)}
          >
            {t("affiliate.note")}
          </Text>
          {showActionColumn &&
            item.bind_type !== "legacy" &&
            !item.is_default_rate && (
              <Text
                className="oui-refereesTable-reset-btn oui-shrink-0 oui-cursor-pointer oui-text-primary-light"
                onClick={() => onEditReferee(ReferralCodeFormType.Reset, item)}
              >
                {t("common.reset")}
              </Text>
            )}
        </Flex>
      </MobileCell>
    </MobileCard>
  );
};

export const RefereesTableUI: FC<RefereesTableUIProps> = (props) => {
  const { t } = useTranslation();
  const { isMobile } = useScreen();
  const showPagination = (props.refereesPagination?.count ?? 0) >= 10;

  const refereeColumns = useMemo<Column<RefereeDataType>[]>(() => {
    return [
      {
        title: t("common.address"),
        dataIndex: "address",
        render: (_: string, record: RefereeDataType) => (
          <AddressDescriptionCell item={record} />
        ),
      },
      {
        title: t("common.type"),
        dataIndex: "bind_type",
        render: (_: unknown, record: RefereeDataType) => {
          const typeInfo = getRefereeType(record.bind_type, t);
          return (
            <TooltipCell
              text={typeInfo.text}
              tooltip={typeInfo.tooltip}
              title={t("common.type")}
            />
          );
        },
      },
      {
        title: t("affiliate.boundAt"),
        dataIndex: "code_binding_time",
        render: (value: number) => <Text>{formatYMDTime(value)}</Text>,
      },
      {
        title: t("affiliate.referralCodes"),
        dataIndex: "bind_code",
        render: (value: string) => <Text>{value}</Text>,
      },
      {
        title: t("affiliate.referralCodes.column.you&DirectReferees"),
        dataIndex: "referee_rebate_rate",
        width: 175,
        render: (_: unknown, record: RefereeDataType) => (
          <Text>
            {getRebateRateText(record.referral_rebate_rate) + "/"}
            <span className="oui-text-base-contrast-54">
              {getRefereeRebateRateText(
                record.referee_rebate_rate,
                props.baseRebateRate,
              )}{" "}
            </span>
            {!record.is_default_rate && (
              <span className="oui-text-primary-light">
                {`(${t("affiliate.customized")})`}
              </span>
            )}
          </Text>
        ),
        onSort: true,
      },
      {
        title: t("affiliate.networkSize"),
        dataIndex: "network_size",
        render: (_: number, record: RefereeDataType) => (
          <BreakdownCell
            total={record.network_size}
            direct={record.direct_invites}
            indirect={record.indirect_invites}
            title={t("affiliate.networkSize")}
          />
        ),
        onSort: true,
      },
      {
        title: t("common.volume"),
        dataIndex: "volume",
        render: (_: number, record: RefereeDataType) => (
          <BreakdownCell
            total={record.volume}
            direct={record.direct_volume}
            indirect={record.indirect_volume}
            prefix="$"
            fix={2}
            title={t("common.volume")}
          />
        ),
        onSort: true,
      },
      {
        title: t("affiliate.commission"),
        dataIndex: "commission",
        render: (_: number, record: RefereeDataType) => (
          <BreakdownCell
            total={record.commission}
            direct={record.direct_rebate}
            indirect={record.indirect_rebate}
            directBonus={record.direct_bonus_rebate}
            prefix="$"
            fix={6}
            title={t("affiliate.commission")}
          />
        ),
        onSort: true,
      },
      {
        title: t("common.action"),
        dataIndex: "action",
        render: (_: unknown, record: RefereeDataType) => (
          <Flex
            gap={2}
            itemAlign="center"
            className="oui-flex-wrap oui-gap-y-1"
          >
            {props.showActionColumn && record.bind_type !== "legacy" && (
              <Text
                className="oui-refereesTable-edit-btn oui-shrink-0 oui-cursor-pointer oui-text-primary-light"
                onClick={() =>
                  props.onEditReferee(ReferralCodeFormType.Edit, record)
                }
              >
                {t("common.edit")}
              </Text>
            )}
            <Text
              className="oui-refereesTable-note-btn oui-shrink-0 oui-cursor-pointer oui-text-primary-light"
              onClick={() => props.onEditDescription(record)}
            >
              {t("affiliate.note")}
            </Text>
            {props.showActionColumn &&
              record.bind_type !== "legacy" &&
              !record.is_default_rate && (
                <Text
                  className="oui-refereesTable-reset-btn oui-shrink-0 oui-cursor-pointer oui-text-primary-light"
                  onClick={() =>
                    props.onEditReferee(ReferralCodeFormType.Reset, record)
                  }
                >
                  {t("common.reset")}
                </Text>
              )}
          </Flex>
        ),
      } as Column<RefereeDataType>,
    ];
  }, [
    t,
    props.onEditReferee,
    props.onEditDescription,
    props.showActionColumn,
    props.baseRebateRate,
  ]);

  return (
    <>
      {isMobile ? (
        <div className="oui-affiliate-refereesTable oui-flex oui-flex-col oui-px-4">
          <ListView
            dataSource={props.refereesData}
            contentClassName="!oui-space-y-0 oui-pb-3"
            renderItem={(item, index) => (
              <div key={index}>
                <MobileRefereeItem
                  item={item}
                  onEditReferee={props.onEditReferee}
                  onEditDescription={props.onEditDescription}
                  showActionColumn={props.showActionColumn}
                  baseRebateRate={props.baseRebateRate}
                />
                <Divider intensity={8} />
              </div>
            )}
          />
        </div>
      ) : (
        <div
          className={`oui-affiliate-refereesTable oui-px-3 ${
            showPagination ? "" : "oui-pb-3"
          }`}
        >
          <AuthGuardDataTable
            bordered
            columns={refereeColumns}
            dataSource={props.refereesData}
            loading={props.isRefereesLoading}
            pagination={showPagination ? props.refereesPagination : undefined}
            onSort={props.onRefereesSort}
            onRow={() => ({ className: "oui-refereesTable-row oui-h-12" })}
            className="oui-refereesTable-table [&_.oui-h-10.oui-w-full]:!oui-mx-0 [&_.oui-table-pagination]:!oui-justify-end [&_th]:!oui-tracking-[0.03em] [&_th]:!oui-px-3 [&_td]:!oui-px-3"
          />
        </div>
      )}
    </>
  );
};
