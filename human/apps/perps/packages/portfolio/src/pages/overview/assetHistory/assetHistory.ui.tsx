import { FC, useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { DataFilter } from "@paxeer-network/ui";
import { AuthGuardDataTable } from "@paxeer-network/ui-connector";
import {
  AssetTarget,
  type AssetHistoryScriptReturn,
} from "./assetHistory.script";
import { useAssetHistoryColumns } from "./column";

type AssetHistoryProps = AssetHistoryScriptReturn;

export const AssetHistory: FC<AssetHistoryProps> = (props) => {
  const { dataSource, queryParameter, onFilter, isLoading } = props;
  const { dateRange, target } = queryParameter;
  const columns = useAssetHistoryColumns({
    chainsInfo: props.chainsInfo,
    isDeposit: props.isDeposit,
    isWeb3Wallet: props.isWeb3Wallet,
  });
  const { t } = useTranslation();

  const options = useMemo(() => {
    return [
      { label: t("common.web3Wallet"), value: AssetTarget.Web3Wallet },
      { label: t("common.accountId"), value: AssetTarget.AccountId },
    ];
  }, [t]);

  return (
    <>
      <DataFilter
        items={[
          {
            type: "select",
            name: "target",
            options: options,
            value: target,
          },
          {
            type: "range",
            name: "dateRange",
            value: {
              from: dateRange[0],
              to: dateRange[1],
            },
          },
        ]}
        onFilter={(value) => {
          onFilter(value);
        }}
      />

      <AuthGuardDataTable
        bordered
        loading={isLoading}
        classNames={{
          root: "oui-h-[calc(100%_-_49px)]",
          scroll: "oui-min-h-[400px]",
        }}
        columns={columns}
        dataSource={dataSource}
        pagination={props.pagination}
      />
    </>
  );
};
