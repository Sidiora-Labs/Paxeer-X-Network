import {
  PositionTypeProps,
  useTPSLPositionTypeScript,
} from "./tpslPositionType.script";
import { TPSLPositionTypeUI } from "./tpslPositionType.ui";

export const TPSLPositionTypeWidget = (props: PositionTypeProps) => {
  const state = useTPSLPositionTypeScript(props);
  return <TPSLPositionTypeUI {...state} />;
};
