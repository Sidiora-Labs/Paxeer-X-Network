import { memo } from "react";
import { useTranslation, Trans } from "@paxeer-network/i18n";
import { Button, CloseIcon, ThrottledButton } from "@paxeer-network/ui";
import { commify } from "@paxeer-network/utils";
import { EditType } from "../../../../type";

type ConfirmContentProps = {
  type: EditType;
  base: string;
  value: string;
  cancelPopover: () => void;
  isSubmitting: boolean;
  onConfirm: (e: any) => void;
};

export const ConfirmContent = memo((props: ConfirmContentProps) => {
  const { type, base, value, cancelPopover, isSubmitting, onConfirm } = props;
  const { t } = useTranslation();

  const renderLabel = () => {
    const common = {
      values: { base, value: commify(value) },
      components: [<span key="0" className="oui-text-warning-darken" />],
    };

    switch (type) {
      case EditType.quantity:
        return (
          // @ts-ignore
          <Trans i18nKey="order.edit.confirm.quantity" {...common} />
        );
      case EditType.price:
        return (
          // @ts-ignore
          <Trans i18nKey="order.edit.confirm.price" {...common} />
        );
      case EditType.triggerPrice:
        return (
          // @ts-ignore
          <Trans i18nKey="order.edit.confirm.triggerPrice" {...common} />
        );
      case EditType.callbackValue:
        return (
          // @ts-ignore
          <Trans i18nKey="order.edit.confirm.callbackValue" {...common} />
        );
      case EditType.callbackRate:
        return (
          // @ts-ignore
          <Trans i18nKey="order.edit.confirm.callbackRate" {...common} />
        );
    }
  };

  return (
    <div className="oui-relative oui-pt-5">
      <div className="desktop:oui-text-sm oui-text-2xs oui-text-base-contrast-54">
        {renderLabel()}
      </div>
      <div className="oui-mt-5 oui-grid oui-grid-cols-2 oui-gap-2">
        <Button
          color="secondary"
          size={"md"}
          onClick={cancelPopover}
          disabled={isSubmitting}
        >
          {t("common.cancel")}
        </Button>
        <ThrottledButton size={"md"} loading={isSubmitting} onClick={onConfirm}>
          {t("common.confirm")}
        </ThrottledButton>
      </div>
      <button
        className="oui-absolute oui-end-0 oui-top-0 oui-text-base-contrast-54"
        onClick={cancelPopover}
      >
        <CloseIcon size={16} color="white" opacity={1} />
      </button>
    </div>
  );
});

ConfirmContent.displayName = "ConfirmContent";
