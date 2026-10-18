import {
  useShareButtonScript,
  ShareButtonScriptOptions,
} from "./shareButton.script";
import { ShareButton } from "./shareButton.ui";

export type ShareButtonWidgetProps = ShareButtonScriptOptions;

export const ShareButtonWidget = (props: ShareButtonWidgetProps) => {
  const state = useShareButtonScript(props);
  return <ShareButton {...state} />;
};
