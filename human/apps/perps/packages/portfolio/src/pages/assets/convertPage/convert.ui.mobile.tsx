import { Fragment, isValidElement, useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  useModal,
  modal,
  Text,
  Divider,
  TokenIcon,
  Flex,
  DataFilter,
  SimpleSheet,
  toast,
  ScrollIndicator,
} from "@paxeer-network/ui";
import { getEffectiveConvertStatus } from "@paxeer-network/ui-transfer";
import { ConvertRecord, ConvertTransaction } from "../type";
import { ConvertedAssetColumn, ConvertStatusBadge } from "./convert.column";
import { useConvertScript } from "./convert.script";

type ConvertMobileUIProps = {
  convertState: ReturnType<typeof useConvertScript>;
};

type ConvertMobileItemProps = {
  item?: any;
  chainsInfo: any[];
};

// Amounts in convert history are denominated in the row's received_asset
// (USDC for forward, the target collateral for reverse conversions).
const amountWithAsset = (amount: number | undefined, asset?: string) =>
  asset ? `${amount ?? "-"} ${asset}` : `${amount ?? "-"}`;

type ConvertMobileFieldProps = {
  label: string;
  value?: React.ReactNode;
  className?: string;
  copyable?: boolean;
  rule?: "address" | "txId";
  onClick?: () => void;
};

export const ConvertMobileUI: React.FC<ConvertMobileUIProps> = ({
  convertState,
}) => {
  const { t } = useTranslation();

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
        onFilter={onFilter}
        items={[
          // {
          //   size: "sm",
          //   type: "picker",
          //   name: "account",
          //   value: selectedAccount,
          //   options: memoizedOptions,
          // },
          {
            size: "md",
            type: "picker",
            name: "converted_asset",
            value: convertedAssetFilter,
            options: convertedAssetOptions,
            className: "oui-whitespace-nowrap",
          },
          {
            size: "md",
            type: "picker",
            name: "status",
            value: statusFilter,
            options: statusOptions,
            className: "oui-whitespace-nowrap",
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
    <div className="oui-flex oui-flex-col oui-gap-1 oui-px-3">
      <ScrollIndicator className="oui-pe-5">
        <Flex direction="row">{dataFilter}</Flex>
      </ScrollIndicator>
      {convertState.dataSource.map((item) => (
        <ConvertMobileItem
          key={item.convert_id}
          item={item}
          chainsInfo={convertState.chainsInfo as any}
        />
      ))}
    </div>
  );
};

const ConvertMobileField: React.FC<ConvertMobileFieldProps> = ({
  label,
  value,
  rule,
  copyable = false,
  className = "",
  onClick,
}) => {
  const { t } = useTranslation();
  const onCopy = (e: any) => {
    e.preventDefault();
    e.stopPropagation();
    toast.success(t("common.copy.copied"));
  };
  return (
    <div
      className={`oui-text-2xs oui-font-semibold oui-text-base-contrast-80 [&_p]:oui-text-base-contrast-36 ${className}`}
      onClick={onClick}
    >
      <p>{label}</p>
      {isValidElement(value) ? (
        value
      ) : (
        <Text.formatted rule={rule} copyable={copyable} onCopy={onCopy}>
          {value || "-"}
        </Text.formatted>
      )}
    </div>
  );
};

const ConvertMobileItem: React.FC<ConvertMobileItemProps> = (props) => {
  const { item } = props;
  const { t } = useTranslation();

  if (!item) {
    return null;
  }

  const totalHaircut = item.details.reduce(
    (sum: number, detail: ConvertTransaction) => sum + detail.haircut,
    0,
  );

  return (
    <div className="oui-flex oui-flex-col oui-gap-2 oui-rounded-xl oui-bg-base-9 oui-p-2">
      <div className="oui-flex oui-justify-between">
        <div className="oui-flex oui-items-center oui-text-xs oui-font-semibold oui-text-base-contrast-80">
          <ConvertedAssetColumn
            convertedAssets={item.converted_asset}
            details={item.details}
          />
          <div
            className="oui-ms-2 oui-text-primary"
            onClick={() => {
              modal.show(ConverHistoryItemDetailsDialog, {
                item,
                chainsInfo: props.chainsInfo,
              });
            }}
          >
            {t("portfolio.overview.column.convert.details")}
          </div>
        </div>
        <Text.formatted
          className="oui-text-2xs oui-text-base-contrast-36"
          rule="date"
        >
          {item.created_time}
        </Text.formatted>
      </div>

      {/* 5 fields in grid container: 3 per row, wrap to next line */}
      <div className="oui-grid oui-grid-cols-3 oui-gap-2">
        <ConvertMobileField
          label={t("portfolio.overview.column.convert.receivedAmount")}
          value={amountWithAsset(item.received_qty, item.received_asset)}
        />
        <ConvertMobileField
          label={t("common.fee")}
          value={amountWithAsset(totalHaircut, item.received_asset)}
        />
        <ConvertMobileField
          label={t("portfolio.overview.column.convert.convertId")}
          value={item.convert_id}
          copyable={true}
        />
        <ConvertMobileField
          label={t("common.type")}
          value={item.type?.charAt(0).toUpperCase() + item.type?.slice(1)}
        />
        <div className="oui-flex oui-flex-col oui-gap-1 oui-text-2xs oui-text-base-contrast-36">
          <p>{t("common.status")}</p>
          <ConvertStatusBadge status={getEffectiveConvertStatus(item)} />
        </div>
      </div>
    </div>
  );
};

type ConvertHistoryItemDetailsDialogProps = {
  item: ConvertRecord;
  chainsInfo: any[];
};

const ConverHistoryItemDetailsDialog =
  modal.create<ConvertHistoryItemDetailsDialogProps>((props) => {
    const { item, chainsInfo } = props;
    const { t } = useTranslation();
    const { visible, hide, resolve, reject, onOpenChange } = useModal();

    return (
      <SimpleSheet
        title={t("portfolio.overview.convert.dialog.title.details")}
        open={visible}
        onOpenChange={onOpenChange}
      >
        <div className="oui-flex oui-h-[300px] oui-flex-col oui-gap-3 oui-overflow-y-auto oui-rounded-lg oui-bg-base-7 oui-p-2">
          {item.details.map((detail, index) => (
            <Fragment key={detail.transaction_id ?? index}>
              <div className="oui-flex oui-flex-col oui-gap-2">
                <div className="oui-flex oui-items-center oui-gap-1">
                  <TokenIcon name={detail.converted_asset} size="xs" />
                  <div className="oui-text-xs oui-text-base-contrast-80">
                    {detail.converted_asset}
                  </div>
                </div>
                <div className="oui-grid oui-grid-cols-3 oui-gap-2">
                  <ConvertMobileField
                    label={t("common.qty")}
                    value={detail.converted_qty}
                  />
                  <ConvertMobileField
                    label={t(
                      "portfolio.overview.column.convert.receivedAmount",
                    )}
                    value={amountWithAsset(
                      detail.received_qty,
                      detail.received_asset,
                    )}
                  />
                  <ConvertMobileField
                    label={t("common.fee")}
                    value={amountWithAsset(
                      detail.haircut,
                      detail.received_asset,
                    )}
                  />
                  <ConvertMobileField
                    label={t("common.txId")}
                    copyable={!!detail.tx_id}
                    rule="txId"
                    value={detail.tx_id}
                    onClick={() => {
                      if (detail.tx_id) {
                        const chainInfo = chainsInfo.find(
                          (item) => item.chain_id == detail.chain_id,
                        );
                        if (chainInfo?.explorer_base_url) {
                          window.open(
                            `${chainInfo.explorer_base_url}/tx/${detail.tx_id}`,
                            "_blank",
                          );
                        }
                      }
                    }}
                  />
                  <ConvertMobileField
                    label={t("transfer.network")}
                    value={
                      chainsInfo.find(
                        (item) => item.chain_id == detail.chain_id,
                      )?.name || "-"
                    }
                  />
                  <ConvertMobileField
                    label={t("common.result")}
                    value={<ConvertStatusBadge status={detail.result} />}
                  />
                </div>
              </div>
              {index < item.details.length - 1 && <Divider />}
            </Fragment>
          ))}
        </div>
      </SimpleSheet>
    );
  });
