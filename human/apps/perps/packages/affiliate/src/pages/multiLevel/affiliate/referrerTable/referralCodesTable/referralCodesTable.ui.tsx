import { FC, useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  Column,
  Text,
  ListView,
  Divider,
  useScreen,
} from "@paxeer-network/ui";
import { AuthGuardDataTable } from "@paxeer-network/ui-connector";
import { Decimal } from "@paxeer-network/utils";
import { ReferralCodesRow } from "../../../../../hooks/useMultiLevelReferralCodes";
import {
  MobileCell,
  MobileCard,
  TooltipCell,
  BreakdownCell,
} from "../base/cells";
import { ReferralCodesTableScriptReturns } from "./referralCodesTable.script";

type ReferralCodesTableUIProps = ReferralCodesTableScriptReturns;

const getRefereeRebateRateText = (
  refereeRate: number,
  baseRebateRate: number,
) => {
  return new Decimal(refereeRate).add(baseRebateRate).mul(100).toFixed(0);
};

const getReferralCodeType = (
  referralType: "single" | "multi" | undefined,
  t: ReturnType<typeof useTranslation>["t"],
) => {
  if (referralType === "multi") {
    return {
      text: t("affiliate.multiLevel"),
      tooltip: t("affiliate.multiLevel.tooltip"),
    };
  }

  return {
    text: t("affiliate.singleLevelLegacy"),
    tooltip: t("affiliate.singleLevelLegacy.tooltip"),
  };
};

const MobileReferralCodeItem: FC<{
  item: ReferralCodesRow;
  copyCode: (code: string) => void;
}> = ({ item, copyCode }) => {
  const { t } = useTranslation();
  const referrerRate = new Decimal(item.referrer_rebate_rate ?? 0)
    .mul(100)
    .toFixed(0);
  const refereeRate = getRefereeRebateRateText(
    item.referee_rebate_rate ?? 0,
    item.base_rebate_rate ?? 0,
  );
  const typeInfo = getReferralCodeType(item.referral_type, t);

  return (
    <MobileCard>
      <MobileCell label={t("affiliate.referralCode")}>
        <Text>{item.code || ""}</Text>
      </MobileCell>
      <MobileCell label={t("common.type")}>
        <TooltipCell
          text={typeInfo.text}
          tooltip={typeInfo.tooltip}
          title={t("common.type")}
        />
      </MobileCell>
      <MobileCell
        label={t("affiliate.referralCodes.column.you&DirectReferees")}
        align="end"
      >
        <Text>
          {referrerRate}%/
          <span className="oui-text-base-contrast-54">{refereeRate}%</span>
        </Text>
      </MobileCell>
      <MobileCell label={t("affiliate.networkSize")}>
        <BreakdownCell
          total={item.total_invites ?? 0}
          direct={item.direct_invites ?? 0}
          indirect={item.indirect_invites ?? 0}
          title={t("affiliate.networkSize")}
        />
      </MobileCell>
      <MobileCell label={t("common.volume")}>
        <BreakdownCell
          total={item.total_volume ?? 0}
          direct={item.direct_volume ?? 0}
          indirect={item.indirect_volume ?? 0}
          prefix="$"
          fix={2}
          title={t("common.volume")}
        />
      </MobileCell>
      <MobileCell label={t("affiliate.commission")} align="end">
        <BreakdownCell
          total={item.total_rebate ?? 0}
          direct={item.direct_rebate ?? 0}
          indirect={item.indirect_rebate ?? 0}
          directBonus={item.direct_bonus_rebate}
          prefix="$"
          fix={6}
          title={t("affiliate.commission")}
        />
      </MobileCell>

      <MobileCell label={t("common.action")}>
        <Text
          className="oui-referralCodesTable-copyCode-btn oui-cursor-pointer oui-text-primary-light"
          onClick={(e) => {
            e.stopPropagation();
            copyCode(item.code);
          }}
        >
          {t("common.copy")}
        </Text>
      </MobileCell>
    </MobileCard>
  );
};

export const ReferralCodesTableUI: FC<ReferralCodesTableUIProps> = (props) => {
  const { t } = useTranslation();
  const { isMobile } = useScreen();

  const columns = useMemo<Column<ReferralCodesRow>[]>(() => {
    return [
      {
        title: t("affiliate.referralCode"),
        dataIndex: "code",
        render: (value: string) => <Text>{value || ""}</Text>,
      },
      {
        title: t("common.type"),
        dataIndex: "referral_type",
        render: (_: unknown, record: ReferralCodesRow) => {
          const typeInfo = getReferralCodeType(record.referral_type, t);
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
        title: (
          <Text>
            {t("affiliate.referralCodes.column.defaultSplit")}
            <br />({t("affiliate.referralCodes.column.you&DirectReferees")})
          </Text>
        ),
        dataIndex: "rate",
        width: 180,
        className: "!oui-whitespace-normal",
        render: (_: unknown, record: ReferralCodesRow) => {
          const referrerRate = new Decimal(record.referrer_rebate_rate ?? 0)
            .mul(100)
            .toFixed(0);
          const refereeRate = getRefereeRebateRateText(
            record.referee_rebate_rate ?? 0,
            record.base_rebate_rate ?? 0,
          );
          return (
            <Text>
              {referrerRate}%
              <span className="oui-text-base-contrast-54">/{refereeRate}%</span>
            </Text>
          );
        },
      },
      {
        title: t("affiliate.networkSize"),
        dataIndex: "total_invites",
        render: (_: unknown, record: ReferralCodesRow) => {
          return (
            <BreakdownCell
              total={record.total_invites ?? 0}
              direct={record.direct_invites ?? 0}
              indirect={record.indirect_invites ?? 0}
              title={t("affiliate.networkSize")}
            />
          );
        },
        onSort: true,
      },
      {
        title: t("common.volume"),
        dataIndex: "total_volume",
        render: (_: unknown, record: ReferralCodesRow) => {
          return (
            <BreakdownCell
              total={record.total_volume ?? 0}
              direct={record.direct_volume ?? 0}
              indirect={record.indirect_volume ?? 0}
              prefix="$"
              fix={2}
              title={t("common.volume")}
            />
          );
        },
        onSort: true,
      },
      {
        title: t("affiliate.commission"),
        dataIndex: "total_rebate",
        render: (_: unknown, record: ReferralCodesRow) => {
          return (
            <BreakdownCell
              total={record.total_rebate ?? 0}
              direct={record.direct_rebate ?? 0}
              indirect={record.indirect_rebate ?? 0}
              directBonus={record.direct_bonus_rebate}
              prefix="$"
              fix={6}
              title={t("affiliate.commission")}
            />
          );
        },
        onSort: true,
      },
      {
        title: t("common.action"),
        dataIndex: "action",
        render: (_: unknown, record: ReferralCodesRow) => (
          <Text
            className="oui-cursor-pointer oui-text-primary-light"
            onClick={(e) => {
              e.stopPropagation();
              props.copyCode(record.code);
            }}
          >
            {t("common.copy")}
          </Text>
        ),
      },
    ];
  }, [t, props.copyCode]);

  return (
    <>
      {isMobile ? (
        <div className="oui-affiliate-referralCodesTable oui-flex oui-flex-col oui-px-4">
          <ListView
            dataSource={props.sortedCodes}
            contentClassName="!oui-space-y-0 oui-pb-3"
            renderItem={(item, index) => (
              <div key={index}>
                <MobileReferralCodeItem item={item} copyCode={props.copyCode} />
                <Divider intensity={8} />
              </div>
            )}
          />
        </div>
      ) : (
        <div className="oui-affiliate-referralCodesTable oui-px-3">
          <AuthGuardDataTable
            bordered
            columns={columns}
            dataSource={props.sortedCodes}
            loading={props.isLoading}
            onSort={props.onSort}
            onRow={() => ({ className: "oui-referralCodesTable-row oui-h-12" })}
            className="oui-referralCodesTable-table oui-pb-3 [&_.oui-h-10.oui-w-full]:!oui-mx-0 [&_.oui-table-pagination]:!oui-justify-end [&_th]:!oui-tracking-[0.03em] [&_th]:!oui-px-3 [&_td]:!oui-px-3"
          />
        </div>
      )}
    </>
  );
};
