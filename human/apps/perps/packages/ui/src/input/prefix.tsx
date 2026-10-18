import React from "react";
import { cnBase } from "tailwind-variants";
import { InputAdditional } from "./inputAdditional";

interface InputPrefixProps {
  prefix?: string | React.ReactNode;
  className?: string;
  id: string;
}

export const InputPrefix: React.FC<InputPrefixProps> = (props) => {
  const { prefix, id } = props;
  if (typeof prefix === "undefined") {
    return null;
  }
  if (typeof prefix === "string") {
    return (
      <InputAdditional name={id} className={props.className}>
        {prefix}
      </InputAdditional>
    );
  }
  return prefix;
};
