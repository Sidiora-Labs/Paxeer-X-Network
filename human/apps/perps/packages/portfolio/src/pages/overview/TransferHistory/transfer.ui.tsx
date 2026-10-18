import React, { useMemo } from "react";
import { produce } from "immer";
import { useAccount } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import type { API } from "@paxeer-network/types";
import { DataFilter, formatAddress } from "@paxeer-network/ui";
import { AuthGuardDataTable } from "@paxeer-network/ui-connector";
import type { SelectOption } from "@paxeer-network/ui/src/select/withOptions";
import { useColumns } from "./column";
import type { useTransferHistoryHookReturn } from "./useDataSource.script";

type HistoryProps = useTransferHistoryHookReturn;

export enum AccountType {
  ALL = "All accounts",
  MAIN = "Main accounts",
}

export const TransferHistoryUI: React.FC<
  Readonly<HistoryProps & ReturnType<typeof useAccount>>
> = (props) => {
  const {
    dataSource,
    queryParameter,
    state,
    isMainAccount,
    isLoading,
    selectedAccount,
    onFilter,
  } = props;
  const { side, dateRange } = queryParameter;

  const { t } = useTranslation();

  const ALL_ACCOUNTS: SelectOption = {
    label: t("common.allAccount"),
    value: AccountType.ALL,
  };

  const MAIN_ACCOUNT: SelectOption = {
    label: t("common.mainAccount"),
    value: AccountType.MAIN,
  };

  const subAccounts = state.subAccounts ?? [];

  const memoizedOptions = useMemo(() => {
    if (Array.isArray(subAccounts) && subAccounts.length) {
      return [
        ALL_ACCOUNTS,
        MAIN_ACCOUNT,
        ...subAccounts.map<SelectOption>((value) => ({
          value: value.id,
          label: value?.description || formatAddress(value?.id),
        })),
      ];
    }
    return [ALL_ACCOUNTS, MAIN_ACCOUNT];
  }, [subAccounts]);

  const columns = useColumns();

  return (
    <>
      <DataFilter
        onFilter={onFilter}
        items={produce(
          [
            {
              type: "select",
              name: "side",
              value: side,
              options: [
                { value: "OUT", label: t("common.outflow") },
                { value: "IN", label: t("common.inflow") },
              ],
            },
            {
              type: "range",
              name: "dateRange",
              value: {
                from: dateRange[0],
                to: dateRange[1],
              },
            },
          ],
          (draft) => {
            if (isMainAccount) {
              draft.unshift({
                type: "select",
                name: "account",
                value: selectedAccount as any,
                options: memoizedOptions,
              });
            }
          },
        )}
      />
      <AuthGuardDataTable<API.TransferHistoryRow>
        bordered
        columns={columns}
        dataSource={dataSource}
        loading={isLoading}
        // isValidating={isValidating}
        className="oui-font-semibold"
        classNames={{ root: "oui-h-[calc(100%_-_49px)]" }}
        pagination={props.pagination}
        generatedRowKey={(row) => row.id}
      />
    </>
  );
};
