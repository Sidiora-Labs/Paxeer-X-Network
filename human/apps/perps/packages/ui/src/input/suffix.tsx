import React from "react";
import { InputAdditional } from "./inputAdditional";

interface InputPrefixProps {
  suffix?: string | React.ReactNode;
  className?: string;
  id: string;
}

export const InputSuffix: React.FC<InputPrefixProps> = (props) => {
  const { suffix, id } = props;
  if (typeof suffix === "undefined") {
    return null;
  }
  if (typeof suffix === "string") {
    return (
      <InputAdditional name={id} className={props.className}>
        {suffix}
      </InputAdditional>
    );
  }
  return suffix;
};
