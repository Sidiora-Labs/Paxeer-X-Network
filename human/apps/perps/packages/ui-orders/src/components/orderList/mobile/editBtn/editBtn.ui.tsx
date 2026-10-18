import { FC, PropsWithChildren } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Button, SimpleSheet } from "@paxeer-network/ui";

export const EditBtn: FC<{ onShowEditSheet: () => void }> = (props) => {
  const { t } = useTranslation();

  return (
    <Button
      variant="outlined"
      fullWidth
      color="secondary"
      size="sm"
      className="oui-border-base-contrast-36"
      onClick={() => {
        props.onShowEditSheet();
      }}
    >
      {t("common.edit")}
    </Button>
  );
};

export const EditOrderSheet: FC<
  PropsWithChildren<{
    open: boolean;
    onOpenChange: (open: boolean) => void;
  }>
> = (props) => {
  const { t } = useTranslation();

  return (
    <SimpleSheet
      title={t("orders.editOrder")}
      open={props.open}
      onOpenChange={props.onOpenChange}
      classNames={{
        content: "oui-bg-base-8",
      }}
    >
      {props.children}
    </SimpleSheet>
  );
};
