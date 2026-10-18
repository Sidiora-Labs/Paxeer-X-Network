import { useState } from "react";
import { API } from "@paxeer-network/types";
import {
  AddCircleIcon,
  IconButton,
  SimpleDialog,
  SimpleSheet,
} from "@paxeer-network/ui";
import { useAdjustMarginScript } from "./adjustMargin.script";
import { AdjustMargin } from "./adjustMargin.ui";

type AdjustMarginButtonProps = {
  position: API.PositionTPSLExt;
  mode: "dialog" | "sheet";
};

export const AdjustMarginButton = (props: AdjustMarginButtonProps) => {
  const [open, setOpen] = useState(false);

  // The dialog/sheet stays mounted so Radix exit animations can play, while
  // the content is only mounted while open to reset the script state per open.
  const content = open && (
    <AdjustMarginContent
      position={props.position}
      close={() => setOpen(false)}
    />
  );

  return (
    <>
      <IconButton
        color="secondary"
        onClick={(event) => {
          event.stopPropagation();
          setOpen(true);
        }}
      >
        <AddCircleIcon size={16} fill="currentColor" opacity={1} />
      </IconButton>

      {props.mode === "sheet" ? (
        <SimpleSheet open={open} onOpenChange={setOpen} closable={false}>
          {content}
        </SimpleSheet>
      ) : (
        <SimpleDialog
          open={open}
          onOpenChange={setOpen}
          closable={false}
          size="sm"
        >
          {content}
        </SimpleDialog>
      )}
    </>
  );
};

const AdjustMarginContent = (props: {
  position: API.PositionTPSLExt;
  close: () => void;
}) => {
  const state = useAdjustMarginScript({
    position: props.position,
    symbol: props.position.symbol,
    close: props.close,
  });

  return <AdjustMargin {...state} />;
};
