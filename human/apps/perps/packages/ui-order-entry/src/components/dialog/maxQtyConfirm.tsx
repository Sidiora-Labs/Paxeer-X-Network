import { FC, memo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { SimpleDialog } from "@paxeer-network/ui";

type MaxQtyConfirmProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  maxQty: string;
  onConfirm: () => void;
  base: string;
};

export const MaxQtyConfirm: FC<MaxQtyConfirmProps> = memo((props) => {
  const { t } = useTranslation();
  return (
    <SimpleDialog
      open={props.open}
      title={t("orderEntry.orderConfirm")}
      closable
      onOpenChange={props.onOpenChange}
      size="sm"
      classNames={{
        footer: "oui-maxQtyConfirm-footer",
        body: "oui-maxQtyConfirm-body",
      }}
      actions={{
        primary: {
          label: t("orderEntry.placeOrderNow"),
          className:
            "oui-primary-btn oui-text-sm oui-font-semibold oui-w-[100%] oui-h-8",
          onClick: () => {
            props.onConfirm();
            return Promise.resolve();
          },
        },
        secondary: {
          label: t("common.cancel"),
          className:
            "oui-secondary-btn oui-text-sm oui-font-semibold oui-w-[100%] oui-h-8",
          onClick: () => {
            props.onOpenChange(false);
            return Promise.resolve();
          },
        },
      }}
    >
      <div className="oui-maxQtyConfirm-content oui-text-2xs lg:oui-text-sm">
        {t("orderEntry.maxQty.reminder.content", {
          maxQty: `${props.maxQty} ${props.base}`,
        })}
      </div>
    </SimpleDialog>
  );
});
