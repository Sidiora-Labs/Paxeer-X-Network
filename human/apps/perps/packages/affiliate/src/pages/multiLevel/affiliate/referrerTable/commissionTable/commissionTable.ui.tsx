import { FC, useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  Flex,
  Column,
  Text,
  DatePicker,
  ListView,
  Divider,
  useScreen,
} from "@paxeer-network/ui";
import { AuthGuardDataTable } from "@paxeer-network/ui-connector";
import { commifyOptional } from "@paxeer-network/utils";
import { formatYMDTime } from "../../../../../utils/utils";
import {
  AddressCell,
  MobileCell,
  MobileCard,
  TooltipCell,
} from "../base/cells";
import {
  CommissionDataType,
  CommissionTableScriptReturns,
} from "./commissionTable.script";

type CommissionTableUIProps = CommissionTableScriptReturns;

const formatCommissionAmt = (v: number) =>
  commifyOptional(v, {
    fix: 6,
    fallback: "0",
    padEnd: true,
    prefix: "$",
  });

const CommissionCell: FC<{
  item: CommissionDataType;
}> = ({ item }) => {
  const { t } = useTranslation();
  const totalCommission = item.commission + item.directBonusRebate;
  const tooltip = (
    <>
      <div>
        • {t("affiliate.base")}: {formatCommissionAmt(item.commission)}
      </div>
      <div>
        • {t("affiliate.directBonus")}:{" "}
        {formatCommissionAmt(item.directBonusRebate)}
      </div>
    </>
  );
  return (
    <TooltipCell
      text={formatCommissionAmt(totalCommission)}
      tooltip={tooltip}
      title={t("affiliate.commission")}
    />
  );
};

const MobileCommissionItem: FC<{
  item: CommissionDataType;
}> = ({ item }) => {
  const { t } = useTranslation();
  return (
    <MobileCard>
      <MobileCell label={t("common.date")}>
        <Text>{formatYMDTime(item.date)}</Text>
      </MobileCell>
      <MobileCell label={t("common.code")}>
        <Text>{item.code}</Text>
      </MobileCell>
      <MobileCell label={t("common.volume")} align="end">
        <Text>
          {commifyOptional(item.volume, {
            fix: 2,
            fallback: "0",
            padEnd: true,
            prefix: "$",
          })}
        </Text>
      </MobileCell>
      <MobileCell label={t("affiliate.commission")}>
        <CommissionCell item={item} />
      </MobileCell>
      <MobileCell
        label={t("common.address")}
        align="end"
        className="oui-col-start-3"
      >
        <AddressCell address={item.address} title={t("common.address")} />
      </MobileCell>
    </MobileCard>
  );
};

export const CommissionTableUI: FC<CommissionTableUIProps> = (props) => {
  const { t } = useTranslation();
  const { isMobile } = useScreen();
  const showPagination = (props.pagination?.count ?? 0) >= 10;

  const columns = useMemo<Column<CommissionDataType>[]>(() => {
    return [
      {
        title: t("common.date"),
        dataIndex: "date",
        render: (value: string) => formatYMDTime(value),
      },
      {
        title: t("common.code"),
        dataIndex: "code",
      },
      {
        title: t("common.volume"),
        dataIndex: "volume",
        render: (value: number) => (
          <Text>
            {commifyOptional(value, {
              fix: 2,
              fallback: "0",
              padEnd: true,
              prefix: "$",
            })}
          </Text>
        ),
        onSort: true,
      },
      {
        title: t("affiliate.commission"),
        dataIndex: "commission",
        render: (_value: number, record: CommissionDataType) => (
          <CommissionCell item={record} />
        ),
        onSort: true,
      },
      {
        title: t("common.address"),
        dataIndex: "address",
        render: (value: string) => (
          <AddressCell address={value} title={t("common.address")} />
        ),
      },
    ];
  }, [t]);

  return (
    <div className="oui-affiliate-commissionTable oui-flex oui-w-full oui-flex-col">
      <Flex
        width="100%"
        height={49}
        className="oui-commissionTable-header oui-border-b oui-border-line-6"
      >
        <div className="oui-px-3">
          <DatePicker.range
            size="xs"
            value={props.dateRange}
            onChange={props.setDateRange}
            max={90}
            disabled={{ after: new Date() }}
          />
        </div>
      </Flex>

      {isMobile ? (
        <div className="oui-affiliate-commissionTable-list oui-flex oui-flex-col oui-px-4">
          <ListView
            dataSource={props.commissionData}
            contentClassName="!oui-space-y-0 oui-pb-3"
            renderItem={(item, index) => (
              <div key={index}>
                <MobileCommissionItem item={item} />
                <Divider intensity={8} />
              </div>
            )}
          />
        </div>
      ) : (
        <div
          className={`oui-affiliate-commissionTable-container oui-px-3 ${showPagination ? "" : "oui-pb-3"}`}
        >
          <AuthGuardDataTable
            bordered
            columns={columns}
            dataSource={props.commissionData}
            loading={props.isLoading}
            pagination={showPagination ? props.pagination : undefined}
            onSort={props.onSort}
            onRow={() => ({ className: "oui-commissionTable-row oui-h-12" })}
            className="oui-commissionTable-table [&_.oui-h-10.oui-w-full]:!oui-mx-0 [&_.oui-table-pagination]:!oui-justify-end [&_th]:!oui-tracking-[0.03em] [&_th]:!oui-px-3 [&_td]:!oui-px-3"
          />
        </div>
      )}
    </div>
  );
};
