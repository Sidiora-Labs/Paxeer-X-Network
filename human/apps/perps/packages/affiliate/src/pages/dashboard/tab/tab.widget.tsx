import { useTabScript } from "./tab.script";
import { Tab } from "./tab.ui";

export const TabWidget = (props: {
  classNames?: {
    loadding?: string;
    home?: string;
    dashboard?: string;
  };
}) => {
  const state = useTabScript();
  return <Tab classNames={props.classNames} {...state} />;
};
