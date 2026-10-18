import { FC } from "react";
import { ShareIcon } from "@paxeer-network/ui";
import { ShareButtonScriptReturn } from "./shareButton.script";

export const ShareButton: FC<ShareButtonScriptReturn> = (props) => {
  if (!props.sharePnLConfig) {
    return null;
  }

  return (
    <button
      type="button"
      onClick={(e: any) => {
        e.stopPropagation();
        props.showModal();
      }}
    >
      <ShareIcon color="white" opacity={0.54} size={props.iconSize ?? 16} />
    </button>
  );
};
