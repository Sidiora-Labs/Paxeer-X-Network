import { PropsWithChildren } from "react";
import { AffiliateLayout } from "./layout.ui";
import { useLayoutBuilder } from "./layout.script";

export const AffiliateLayoutWidget = (props: PropsWithChildren) => {
  const state = useLayoutBuilder();
  return <AffiliateLayout {...state} children={props.children} />;
};
