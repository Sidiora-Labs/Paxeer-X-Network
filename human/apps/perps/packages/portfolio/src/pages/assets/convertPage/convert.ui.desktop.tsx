import { useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  DataFilter,
  Flex,
  DataTable,
  modal,
  SimpleDialog,
  Divider,
} from "@paxeer-network/ui";
import { useModal } from "@paxeer-network/ui";
import { AuthGuardDataTable } from "@paxeer-network/ui-connector";
import type { ConvertRecord } from "../type";
import { useConvertColumns, useConvertDetailColumns } from "./convert.column";
import { useConvertScript } from "./convert.script";

type ConvertDesktopUIProps = {
  convertState: ReturnType<typeof useConvertScript>;
};

// Convert Details Modal Component
const ConvertDetailsModal = modal.create<{
  record: ConvertRecord;
  indexPrices: Record<string, number>;
  chainsInfo: any[];
}>((props) => {
  const { t } = useTranslation();
  const { visible, onOpenChange } = useModal();
  // console.log(props);

  const detailColumns = useConvertDetailColumns({
    indexPrices: props.indexPrices,
    chainsInfo: props.chainsInfo,
  });

  return (
    <SimpleDialog
      open={visible}
      onOpenChange={onOpenChange}
      title={t("portfolio.overview.convert.dialog.title.details")}
      size="lg"
      closable
      classNames={{
        content: "oui-max-w-4xl",
      }}
    >
      <DataTable
        columns={detailColumns}
        dataSource={props.record.details}
        bordered
        className="oui-w-full"
        classNames={{
          header: "oui-h-10",
          root: "oui-bg-base-8 oui-max-h-[60vh] oui-overflow-y-scroll oui-custom-scrollbar",
        }}
        onRow={() => ({
          className: "oui-h-[40px]",
        })}
        generatedRowKey={(record) =>
          record.transaction_id || Math.random().toString()
        }
      />
    </SimpleDialog>
  );
});

export const ConvertDesktopUI: React.FC<ConvertDesktopUIProps> = ({
  convertState,
}) => {
  const { t } = useTranslation();

  const handleDetailsClick = (convertId: number) => {
    // Find the convert record by ID
    const record = convertState.dataSource.find(
      (item) => item.convert_id === convertId,
    );
    if (record) {
      modal.show(ConvertDetailsModal, {
        record,
        indexPrices: convertState.indexPrices,
        chainsInfo: convertState.chainsInfo as any,
      });
    }
  };

  const columns = useConvertColumns({
    onDetailsClick: handleDetailsClick,
  });

  const {
    convertedAssetFilter,
    statusFilter,
    dateRange,
    onFilter,
    convertedAssetOptions,
  } = convertState;

  const statusOptions = useMemo(
    () => [
      { label: t("common.status.all"), value: "all" },
      { label: t("orders.status.completed"), value: "completed" },
      { label: t("orders.status.pending"), value: "pending" },
    ],
    [t],
  );

  const dataFilter = useMemo(() => {
    return (
      <DataFilter
        className=""
        onFilter={onFilter}
        items={[
          // {
          //   type: "select",
          //   name: "account",
          //   value: selectedAccount,
          //   options: memoizedOptions,
          // },
          {
            type: "select",
            name: "converted_asset",
            value: convertedAssetFilter,
            options: convertedAssetOptions,
          },
          {
            type: "select",
            name: "status",
            value: statusFilter,
            options: statusOptions,
          },
          {
            type: "range",
            name: "time",
            value: {
              from: dateRange?.[0],
              to: dateRange?.[1],
            },
          },
        ]}
      />
    );
  }, [
    convertedAssetFilter,
    statusFilter,
    dateRange,
    onFilter,
    convertedAssetOptions,
    statusOptions,
  ]);

  return (
    <>
      {dataFilter}

      <AuthGuardDataTable
        bordered
        columns={columns}
        dataSource={convertState.dataSource}
        loading={convertState.isLoading}
        pagination={convertState.pagination}
        manualPagination
        className="oui-mt-4 oui-w-full"
        classNames={{
          root: "oui-h-[calc(100%_-_49px)]",
        }}
        onRow={() => ({
          className: "oui-h-[48px] oui-cursor-pointer",
        })}
        generatedRowKey={(record) => {
          return record.convert_id.toString();
        }}
      />
    </>
  );
};
