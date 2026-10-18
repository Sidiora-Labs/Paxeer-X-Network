import { PNLInput } from "./pnlInput.ui";
import { BuilderProps, PnLMode, usePNLInputBuilder } from "./useBuilder.script";

export const PnlInputWidget = (
  props: BuilderProps & {
    testIds?: {
      input?: string;
      dropDown?: string;
    };
    quote: string;
  }
) => {
  const { testIds, quote, ...rest } = props;
  const state = usePNLInputBuilder(rest);
  return (
    <PNLInput {...state} testIds={testIds} quote={quote} type={props.type} />
  );
};
