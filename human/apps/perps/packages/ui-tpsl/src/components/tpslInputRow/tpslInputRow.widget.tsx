import { Props, useTPSLInputRowScript } from "./tpslInputRow.script";
import { TPSLInputRowUI } from "./tpslInputRow.ui";

export const TPSLInputRowWidget = (props: Props) => {
  const state = useTPSLInputRowScript(props);
  return <TPSLInputRowUI {...state} />;
};
