import { FC } from "react";
import { Leverage } from "./leverage.ui";
import { useLeverageScript } from "./leverage.script";

export type LeverageEditorProps = {
  close?: () => void;
};

export const LeverageEditor: FC<LeverageEditorProps> = (props) => {
  const state = useLeverageScript({ close: props.close });
  return <Leverage {...state} />;
};
