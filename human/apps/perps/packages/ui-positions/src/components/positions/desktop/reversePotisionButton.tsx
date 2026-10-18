import { ArrowLeftRightIcon, Button, modal } from "@paxeer-network/ui";
import { ReversePositionDialogId } from "../../reversePosition";

export const ReversePositionButton = (props: { position: any }) => {
  const { position } = props;

  return (
    <div
      className=""
      style={{ transform: "rotate(90deg)" }}
      onClick={() => {
        modal.show(ReversePositionDialogId, {
          position,
        });
      }}
    >
      <ArrowLeftRightIcon
        size={15}
        opacity={1}
        className="oui-cursor-pointer oui-text-base-contrast-54 hover:oui-text-base-contrast-80"
      />
    </div>
  );
};
