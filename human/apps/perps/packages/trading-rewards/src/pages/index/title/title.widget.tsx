import React from "react";
import { useTitleScript } from "./title.script";
import { Title } from "./title.ui";

export const TitleWidget: React.FC = () => {
  const state = useTitleScript();
  return <Title {...state} />;
};
