import { useLeverageBySymbol } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { MarginMode, PositionType } from "@paxeer-network/types";
import {
  cn,
  EditIcon,
  Text,
  useScreen,
  ChevronRightIcon,
} from "@paxeer-network/ui";
import { modal } from "@paxeer-network/ui";
import {
  PositionTPSLPopover,
  TPSLDetailDialogId,
  TPSLDialogId,
  TPSLSheetId,
  TPSLDetailSheetId,
} from "@paxeer-network/ui-tpsl";
import { Decimal } from "@paxeer-network/utils";
import { usePositionsRowContext } from "../positionsRowContext";

// ------------ TP/SL Price input end------------
export const TPSLButton = () => {
  const { position, baseDp, quoteDp, tpslOrder } = usePositionsRowContext();
  const { t } = useTranslation();
  return (
    <PositionTPSLPopover
      position={position}
      order={tpslOrder}
      label={t("common.tpsl")}
      baseDP={baseDp}
      quoteDP={quoteDp}
      isEditing={false}
    />
  );
};

export const TPSLEditIcon = () => {
  const { position, baseDp, quoteDp, tpslOrder } = usePositionsRowContext();
  const { isMobile } = useScreen();

  const onEdit = () => {
    const dialogId = isMobile ? TPSLDetailSheetId : TPSLDetailDialogId;
    modal.show(dialogId, {
      order: tpslOrder,
      position: position,
      baseDP: baseDp,
      quoteDP: quoteDp,
    });
  };

  return (
    // <PositionTPSLPopover
    //   position={position}
    //   order={tpslOrder}
    //   baseDP={baseDp}
    //   quoteDP={quoteDp}
    //   isEditing
    // >
    <EditIcon
      onClick={onEdit}
      opacity={1}
      className="oui-cursor-pointer oui-text-base-contrast-54"
      size={16}
    />
    // </PositionTPSLPopover>
  );
};

export const AddIcon = (props: { positionType: PositionType }) => {
  const { position, baseDp, quoteDp, tpslOrder } = usePositionsRowContext();
  const { t } = useTranslation();
  const { isMobile } = useScreen();
  const onAdd = () => {
    const dialogId = isMobile ? TPSLSheetId : TPSLDialogId;
    const modalParams = {
      position,
      symbol: position.symbol,
      baseDP: baseDp,
      quoteDP: quoteDp,
      isEditing: false,
      positionType: props.positionType,
    };
    modal.show(dialogId, modalParams);
  };
  return (
    <Text
      className={cn(
        "oui-cursor-pointer oui-text-base-contrast",
        isMobile && "oui-text-base-contrast-80",
      )}
      onClick={onAdd}
    >
      {t("tpsl.add")}
    </Text>
  );
};

type LeverageBadgeProps = {
  symbol: string;
  leverage: number;
  modalId: string;
  marginMode?: MarginMode | 0 | 1;
};

export const LeverageBadge = (props: LeverageBadgeProps) => {
  const { symbol, leverage, marginMode } = props;
  const { t } = useTranslation();

  const resolvedMarginMode = typeof marginMode === "number" ? (marginMode === 0 ? MarginMode.CROSS : MarginMode.ISOLATED) : marginMode;

  const showModal = () => {
    modal.show(props.modalId, {
      symbol,
      curLeverage: leverage,
      marginMode: resolvedMarginMode,
    });
  };

  return (
    <div className="oui-flex oui-items-center oui-gap-1">
      <div
        className={cn(
          "oui-flex oui-h-[18px] oui-items-center",
          "oui-rounded oui-bg-line-6 oui-px-2",
          "oui-text-2xs oui-font-semibold oui-text-base-contrast-36",
        )}
      >
        <Text>
          {resolvedMarginMode === undefined
            ? "--"
            : resolvedMarginMode === MarginMode.ISOLATED
              ? t("marginMode.isolated")
              : t("marginMode.cross")}
        </Text>
      </div>
      <div
        className={cn(
          "oui-flex oui-h-[18px] oui-items-center oui-gap-1",
          "oui-rounded oui-bg-line-6 oui-px-2",
          "oui-text-2xs oui-font-semibold oui-text-base-contrast-36",
          "oui-cursor-pointer",
        )}
        onClick={showModal}
      >
        {leverage === undefined ? (
          <LeverageDisplay symbol={symbol} marginMode={marginMode} />
        ) : (
          <Text.numeral dp={0} rm={Decimal.ROUND_DOWN} size="2xs" unit="X">
            {leverage}
          </Text.numeral>
        )}
        <ChevronRightIcon
          size={12}
          className="oui-text-base-contrast-36"
          opacity={1}
        />
      </div>
    </div>
  );
};

/** TODO: remove this */
export const LeverageDisplay = ({
  symbol,
  marginMode,
}: {
  symbol: string;
  marginMode?: MarginMode | 0 | 1;
}) => {
  const leverage = useLeverageBySymbol(symbol, typeof marginMode === "number" ? (marginMode === 0 ? MarginMode.CROSS : MarginMode.ISOLATED) : marginMode);

  return (
    <>
      {leverage === undefined ? (
        <Text size="2xs">--</Text>
      ) : (
        <Text.numeral dp={0} rm={Decimal.ROUND_DOWN} size="2xs" unit="X">
          {leverage}
        </Text.numeral>
      )}
    </>
  );
};
