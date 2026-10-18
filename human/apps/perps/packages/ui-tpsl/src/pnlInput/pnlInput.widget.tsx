import { PNLInput } from "./pnlInput.ui";
import { BuilderProps, usePNLInputBuilder } from "./useBuilder.script";

export const PnlInputWidget = (
  props: BuilderProps & {
    testId?: string;
    quote: string;
  }
) => {
  const { testId, quote, ...rest } = props;
  const state = usePNLInputBuilder(rest);
  return <PNLInput {...state} testId={testId} quote={quote} />;
};
