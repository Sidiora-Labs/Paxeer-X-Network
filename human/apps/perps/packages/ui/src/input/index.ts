import { QuantityInput } from "./extends/quantity";
import { Input as InputBase, inputVariants } from "./input";
import { InputWithTooltip } from "./input.tooltip";

export { InputAdditional } from "./inputAdditional";

export type { InputProps } from "./input";
export type { InputWithTooltipProps } from "./input.tooltip";
export * as inputFormatter from "./formatter";

export type {
  InputFormatter,
  InputFormatterOptions,
} from "./formatter/inputFormatter";

export { TextField } from "./textField";

export { type TextFieldProps } from "./textField";

export { InputHelpText } from "./inputHelpText";
export type { InputHelpTextProps } from "./inputHelpText";

type InputType = typeof InputBase & {
  token: typeof QuantityInput;
  tooltip: typeof InputWithTooltip;
};

const Input = InputBase as InputType;
Input.token = QuantityInput;
Input.tooltip = InputWithTooltip;

export { Input, inputVariants };
