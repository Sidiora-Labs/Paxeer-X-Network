import { ERROR_MSG_CODES, OrderValidationResult } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { useOrderEntryFormErrorMsg } from "@paxeer-network/react-app";
import {
  cn,
  ExclamationFillIcon,
  modal,
  Tooltip,
  useScreen,
} from "@paxeer-network/ui";

const CloseToLiqPriceIcon = ({
  slPriceError,
  className,
}: {
  slPriceError: OrderValidationResult | null;
  className?: string;
}) => {
  const { t } = useTranslation();
  const { isMobile } = useScreen();
  const { getErrorMsg } = useOrderEntryFormErrorMsg(slPriceError);
  const isSlPriceWarning =
    slPriceError?.sl_trigger_price?.type === ERROR_MSG_CODES.SL_PRICE_WARNING;

  const icon = (
    <ExclamationFillIcon
      onMouseEnter={(e) => e.stopPropagation()}
      onMouseLeave={(e) => e.stopPropagation()}
      onPointerEnter={(e) => e.stopPropagation()}
      onPointerLeave={(e) => e.stopPropagation()}
      size={14}
      className={cn(
        "oui-text-warning-darken hover:oui-cursor-pointer",
        className,
      )}
    />
  );
  if (!isSlPriceWarning || !slPriceError) return null;

  if (isMobile) {
    return (
      <button
        onClick={(e) => {
          modal.alert({
            title: t("common.tips"),
            message: getErrorMsg("sl_trigger_price"),
          });
        }}
        className="oui-px-1"
      >
        {icon}
      </button>
    );
  }

  return (
    <Tooltip
      content={getErrorMsg("sl_trigger_price")}
      className="oui-max-w-[240px] oui-text-base-contrast-80"
    >
      {icon}
    </Tooltip>
  );
};
CloseToLiqPriceIcon.displayName = "CloseToLiqPriceIcon";

export { CloseToLiqPriceIcon };
