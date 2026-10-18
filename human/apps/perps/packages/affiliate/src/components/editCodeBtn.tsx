import { FC, useState } from "react";
import { EditIcon } from "../icons/editIcon";

export const EditCode: FC<{
  size?: number;
  onClick?: () => void;
}> = (props) => {
  return (
    <button>
      <EditIcon
        className=" oui-mt-px oui-cursor-pointer oui-fill-base-contrast-36 hover:oui-fill-base-contrast-80"
        fillOpacity={1}
        fill="currentColor"
        onClick={props.onClick}
      />
    </button>
  );
};
