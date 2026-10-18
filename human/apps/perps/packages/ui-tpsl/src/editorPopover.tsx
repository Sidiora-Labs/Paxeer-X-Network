import { ReactNode } from "react";
import { AlgoOrderRootType, API, PositionType } from "@paxeer-network/types";
import { Box, Button, modal } from "@paxeer-network/ui";
import { ButtonProps } from "@paxeer-network/ui";
import { TPSLDialogId } from "./positionTPSL";

export const PositionTPSLPopover = (props: {
  position: API.Position;
  order?: API.AlgoOrder;
  label?: string;
  baseDP?: number;
  quoteDP?: number;
  /**
   * Button props
   */
  buttonProps?: ButtonProps;
  isEditing?: boolean;
  children?: ReactNode;
}) => {
  const { position, order, baseDP, quoteDP, buttonProps, isEditing } = props;

  const isPositionTPSL = isEditing
    ? order?.algo_type === AlgoOrderRootType.POSITIONAL_TP_SL
    : undefined;

  const onEdit = () => {
    modal.show(TPSLDialogId, {
      order: order,
      symbol: position.symbol,
      baseDP: baseDP,
      quoteDP: quoteDP,
      positionType: isPositionTPSL ? PositionType.FULL : PositionType.PARTIAL,
      isEditing: isEditing,
      position,
    });
  };

  return (
    <Box onClick={onEdit} className="oui-cursor-pointer">
      {props.children || (
        <Button
          variant="outlined"
          size="sm"
          color="secondary"
          {...buttonProps}
          // onClick={() => {
          //   setOpen(true);
          // }}
        >
          {props.label}
        </Button>
      )}
    </Box>
  );
};
